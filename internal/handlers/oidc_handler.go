package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/oidc"
)

// OIDCHandler handles the OIDC authorization code flow.
type OIDCHandler struct {
	db             *gorm.DB
	cfg            *config.Config
	sessionManager *scs.SessionManager
	provider       *oidc.Provider
}

// NewOIDCHandler creates an OIDCHandler. provider must not be nil.
func NewOIDCHandler(db *gorm.DB, cfg *config.Config, sm *scs.SessionManager, p *oidc.Provider) *OIDCHandler {
	return &OIDCHandler{db: db, cfg: cfg, sessionManager: sm, provider: p}
}

// InitiateLogin redirects the user to the IdP authorization endpoint.
// GET /auth/oidc/login
func (h *OIDCHandler) InitiateLogin(w http.ResponseWriter, r *http.Request) {
	state, err := generateState()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if err := h.sessionManager.RenewToken(r.Context()); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	h.sessionManager.Put(r.Context(), "oidc_state", state)

	http.Redirect(w, r, h.provider.OAuth2Config.AuthCodeURL(state), http.StatusFound)
}

// Callback handles the IdP redirect after authentication.
// GET /auth/oidc/callback
func (h *OIDCHandler) Callback(w http.ResponseWriter, r *http.Request) {
	// Validate state to prevent CSRF
	sessionState := h.sessionManager.GetString(r.Context(), "oidc_state")
	if sessionState == "" || r.URL.Query().Get("state") != sessionState {
		flash.Error(w, "Login failed: invalid state parameter.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.sessionManager.Remove(r.Context(), "oidc_state")

	// Exchange authorization code for tokens
	token, err := h.provider.OAuth2Config.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		logger.Error("oidc token exchange failed", "error", err)
		flash.Error(w, "Authentication failed. Please try again.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Extract and verify the ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		flash.Error(w, "Authentication failed: no identity token in response.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	idToken, err := h.provider.Verifier.Verify(r.Context(), rawIDToken)
	if err != nil {
		logger.Error("oidc id_token verification failed", "error", err)
		flash.Error(w, "Authentication failed: token verification error.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Parse claims
	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		flash.Error(w, "Authentication failed: could not read identity claims.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	claims := h.provider.ExtractClaims(rawClaims)

	if claims.Subject == "" || claims.Email == "" {
		flash.Error(w, "Authentication failed: missing required claims.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Find or provision the user
	user, err := h.findOrProvisionUser(claims)
	if err != nil {
		logger.Error("oidc user provisioning failed", "error", err, "subject", claims.Subject)
		flash.Error(w, "Failed to provision user account. Please contact an administrator.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Establish session
	if err := h.sessionManager.RenewToken(r.Context()); err != nil {
		flash.Error(w, "Failed to create session. Please try again.")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.sessionManager.Put(r.Context(), "user_id", int(user.ID))

	http.Redirect(w, r, "/files", http.StatusSeeOther)
}

// findOrProvisionUser resolves the local User for a given set of OIDC claims:
//
//  1. If a user already has this OIDC subject stored → return them (fast path).
//  2. If a user with identity_provider="oidc" and matching email exists but no
//     subject yet → link them (first login after admin switched their IDP).
//  3. Otherwise → auto-provision a new user with identity_provider="oidc".
//
// Admin status is synced from the OIDC claim on every login when OIDCAdminClaim
// is configured.
func (h *OIDCHandler) findOrProvisionUser(claims oidc.Claims) (*models.User, error) {
	var user models.User

	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Fast path: subject already linked.
		if err := tx.Where("oidc_subject = ?", claims.Subject).First(&user).Error; err == nil {
			return h.syncAdminAndSave(tx, &user, claims)
		}

		// First OIDC login after admin switched identity_provider to "oidc":
		// find by email, but only for accounts explicitly marked as OIDC.
		if err := tx.Where("email = ? AND identity_provider = 'oidc' AND oidc_subject = ''", claims.Email).
			First(&user).Error; err == nil {
			user.OIDCSubject = claims.Subject
			return h.syncAdminAndSave(tx, &user, claims)
		}

		// Auto-provision a brand-new user.
		var count int64
		if err := tx.Model(&models.User{}).Count(&count).Error; err != nil {
			return err
		}

		username := claims.Username
		if username == "" {
			// Fall back to the local part of the email address.
			if idx := len(claims.Email); idx > 0 {
				username = claims.Email
				for i, ch := range claims.Email {
					if ch == '@' {
						username = claims.Email[:i]
						break
					}
				}
			}
		}

		// Ensure username is unique within the transaction.
		username = h.uniqueUsername(tx, username)

		user = models.User{
			Username:         username,
			Email:            claims.Email,
			IdentityProvider: "oidc",
			OIDCSubject:      claims.Subject,
			StorageQuota:     h.cfg.DefaultUserQuota,
			IsAdmin:          count == 0 || claims.IsAdmin, // first user is always admin
		}
		return tx.Create(&user).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// syncAdminAndSave optionally updates IsAdmin from the OIDC claim and saves.
func (h *OIDCHandler) syncAdminAndSave(tx *gorm.DB, user *models.User, claims oidc.Claims) error {
	if h.cfg.OIDCAdminClaim != "" {
		user.IsAdmin = claims.IsAdmin
	}
	return tx.Save(user).Error
}

// uniqueUsername returns base if unused, otherwise base_2, base_3, …
func (h *OIDCHandler) uniqueUsername(tx *gorm.DB, base string) string {
	candidate := base
	for i := 2; ; i++ {
		var count int64
		tx.Model(&models.User{}).Where("username = ?", candidate).Count(&count)
		if count == 0 {
			return candidate
		}
		candidate = base + "_" + string(rune('0'+i))
		if i > 9 {
			// Use sprintf-style for i >= 10
			candidate = base + "_" + itoa(i)
		}
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
)

// ShareHandler handles share link creation, revocation, and public access.
type ShareHandler struct {
	db      *gorm.DB
	storage storage.StorageBackend
}

func NewShareHandler(db *gorm.DB, storageService storage.StorageBackend) *ShareHandler {
	return &ShareHandler{db: db, storage: storageService}
}

// generateToken returns a 32-byte cryptographically random URL-safe base64 token.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// consumeUse atomically increments the use counter, respecting MaxUses.
// Returns false if the limit has already been reached.
func (h *ShareHandler) consumeUse(link *models.ShareLink) bool {
	if link.MaxUses != nil {
		result := h.db.Model(link).
			Where("uses < ?", *link.MaxUses).
			Update("uses", gorm.Expr("uses + 1"))
		return result.Error == nil && result.RowsAffected > 0
	}
	h.db.Model(link).Update("uses", gorm.Expr("uses + 1")) //nolint:errcheck
	return true
}

// streamFile writes the file contents to the response with appropriate headers.
func (h *ShareHandler) streamFile(w http.ResponseWriter, r *http.Request, file models.File) {
	reader, err := h.storage.Open(r.Context(), file.StoragePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer reader.Close() //nolint:errcheck

	safeFilename := strings.ReplaceAll(file.Filename, `"`, `\"`)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		safeFilename, url.PathEscape(file.Filename)))
	w.Header().Set("Content-Type", file.MimeType)
	if file.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(file.FileSize, 10))
	}

	if _, err := io.Copy(w, reader); err != nil {
		logger.Error("error streaming shared file", "path", file.StoragePath, "error", err)
	}
}

// showPasswordForm renders the password entry page for a protected share link.
func (h *ShareHandler) showPasswordForm(w http.ResponseWriter, token, filename, errMsg string) {
	if err := render(w, "share_password.html", map[string]any{
		"Title":    "Protected Link",
		"Token":    token,
		"Filename": filename,
		"Error":    errMsg,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// lookupValidLink fetches and validates a share link by token.
// Writes a 404 response and returns nil if the token is invalid, expired, or the file is gone.
func (h *ShareHandler) lookupValidLink(w http.ResponseWriter, token string) *models.ShareLink {
	var link models.ShareLink
	if err := h.db.Preload("File").Where("token = ?", token).First(&link).Error; err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	if link.File.ID == 0 || link.File.SoftDeletedAt != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	return &link
}

// CreateShareLink handles POST /files/{id}/share.
// Creates a new share link for a file owned by the authenticated user.
func (h *ShareHandler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid file ID", http.StatusBadRequest)
		return
	}

	// Verify the file exists and belongs to the user
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NULL", fileID, user.ID).First(&file).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	var expiresAt *time.Time
	if v := strings.TrimSpace(r.FormValue("expires_at")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "Invalid expiry date (use YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
		// Expire at end of the chosen day in UTC
		endOfDay := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
		expiresAt = &endOfDay
	}

	var maxUses *int
	if v := strings.TrimSpace(r.FormValue("max_uses")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "max_uses must be a positive integer", http.StatusBadRequest)
			return
		}
		maxUses = &n
	}

	var passwordHash *string
	if v := r.FormValue("password"); v != "" {
		h, err := auth.HashPassword(v, 10)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		passwordHash = &h
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	link := models.ShareLink{
		Token:        token,
		FileID:       uint(fileID),
		UserID:       user.ID,
		ExpiresAt:    expiresAt,
		MaxUses:      maxUses,
		PasswordHash: passwordHash,
	}
	if err := h.db.Create(&link).Error; err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	flash.Success(w, "Share link created.")
	http.Redirect(w, r, "/files/"+strconv.FormatUint(fileID, 10), http.StatusSeeOther)
}

// RevokeShareLink handles POST /share/{token}/revoke.
// Only the owner of the linked file may revoke the link.
func (h *ShareHandler) RevokeShareLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := chi.URLParam(r, "token")

	var link models.ShareLink
	if err := h.db.Where("token = ? AND user_id = ?", token, user.ID).First(&link).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Delete(&link).Error; err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/files/"+strconv.FormatUint(uint64(link.FileID), 10), http.StatusSeeOther)
}

// AccessShareLink handles GET /s/{token}.
// Public endpoint — no authentication required.
// Returns 404 for expired, exhausted, revoked, or non-existent tokens (no enumeration).
// If the link is password-protected, renders a password entry form instead of serving the file.
func (h *ShareHandler) AccessShareLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link := h.lookupValidLink(w, token)
	if link == nil {
		return
	}

	// Password-protected: show form, do not consume a use yet.
	if link.PasswordHash != nil {
		h.showPasswordForm(w, token, link.File.Filename, "")
		return
	}

	if !h.consumeUse(link) {
		http.NotFound(w, r)
		return
	}

	h.streamFile(w, r, link.File)
}

// VerifySharePassword handles POST /s/{token}.
// Validates the submitted password and, if correct, streams the file.
func (h *ShareHandler) VerifySharePassword(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link := h.lookupValidLink(w, token)
	if link == nil {
		return
	}

	// No password required — redirect to GET which will serve directly.
	if link.PasswordHash == nil {
		http.Redirect(w, r, "/s/"+token, http.StatusSeeOther)
		return
	}

	password := r.FormValue("password")
	if !auth.VerifyPassword(*link.PasswordHash, password) {
		h.showPasswordForm(w, token, link.File.Filename, "Incorrect password. Please try again.")
		return
	}

	if !h.consumeUse(link) {
		http.NotFound(w, r)
		return
	}

	h.streamFile(w, r, link.File)
}

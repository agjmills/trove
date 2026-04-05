package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
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

	token, err := generateToken()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	link := models.ShareLink{
		Token:     token,
		FileID:    uint(fileID),
		UserID:    user.ID,
		ExpiresAt: expiresAt,
		MaxUses:   maxUses,
	}
	if err := h.db.Create(&link).Error; err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

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
func (h *ShareHandler) AccessShareLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	var link models.ShareLink
	err := h.db.Preload("File").Where("token = ?", token).First(&link).Error
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Check expiry
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		http.NotFound(w, r)
		return
	}

	// Ensure the file hasn't been trashed or hard-deleted before consuming a use
	if link.File.ID == 0 || link.File.SoftDeletedAt != nil {
		http.NotFound(w, r)
		return
	}

	// Check max uses and increment atomically
	if link.MaxUses != nil {
		updated := h.db.Model(&link).
			Where("uses < ?", *link.MaxUses).
			Update("uses", gorm.Expr("uses + 1"))
		if updated.Error != nil || updated.RowsAffected == 0 {
			http.NotFound(w, r)
			return
		}
	} else {
		h.db.Model(&link).Update("uses", gorm.Expr("uses + 1")) //nolint:errcheck
	}

	reader, err := h.storage.Open(r.Context(), link.File.StoragePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}
	defer reader.Close() //nolint:errcheck

	safeFilename := strings.ReplaceAll(link.File.Filename, `"`, `\"`)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		safeFilename, url.PathEscape(link.File.Filename)))
	w.Header().Set("Content-Type", link.File.MimeType)
	if link.File.FileSize > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(link.File.FileSize, 10))
	}

	if _, err := io.Copy(w, reader); err != nil {
		log.Printf("Warning: error streaming shared file %s: %v", link.File.StoragePath, err)
	}
}

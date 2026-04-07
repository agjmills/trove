package handlers

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
)

// FolderShareHandler handles folder share link creation, revocation, and public access.
type FolderShareHandler struct {
	db             *gorm.DB
	storage        storage.StorageBackend
	sessionManager *scs.SessionManager
}

func NewFolderShareHandler(db *gorm.DB, storageService storage.StorageBackend, sessionManager *scs.SessionManager) *FolderShareHandler {
	return &FolderShareHandler{db: db, storage: storageService, sessionManager: sessionManager}
}

// folderShareSessionKey returns the session key used to track that a folder share token has been unlocked.
func folderShareSessionKey(token string) string {
	return "fsl_" + token
}

// lookupValidFolderLink fetches and validates a folder share link by token.
// Writes a 404 response and returns nil if the token is invalid or expired.
func (h *FolderShareHandler) lookupValidFolderLink(w http.ResponseWriter, token string) *models.FolderShareLink {
	var link models.FolderShareLink
	if err := h.db.Where("token = ?", token).First(&link).Error; err != nil {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	if link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt) {
		http.Error(w, "404 page not found", http.StatusNotFound)
		return nil
	}
	return &link
}

// consumeFolderUse atomically increments the use counter, respecting MaxUses.
func (h *FolderShareHandler) consumeFolderUse(link *models.FolderShareLink) bool {
	if link.MaxUses != nil {
		result := h.db.Model(link).
			Where("uses < ?", *link.MaxUses).
			Update("uses", gorm.Expr("uses + 1"))
		return result.Error == nil && result.RowsAffected > 0
	}
	h.db.Model(link).Update("uses", gorm.Expr("uses + 1")) //nolint:errcheck
	return true
}

// isUnlocked reports whether the current session has unlocked this folder share token.
func (h *FolderShareHandler) isUnlocked(r *http.Request, token string) bool {
	return h.sessionManager.GetBool(r.Context(), folderShareSessionKey(token))
}

// folderFiles returns all non-deleted, completed files owned by the share user
// that live directly in or under the shared folder path.
func (h *FolderShareHandler) folderFiles(link *models.FolderShareLink) ([]models.File, error) {
	var files []models.File
	prefix := link.FolderPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	err := h.db.
		Where("user_id = ? AND trashed_at IS NULL AND upload_status = 'completed' AND (logical_path = ? OR logical_path LIKE ?)",
			link.UserID, link.FolderPath, prefix+"%").
		Find(&files).Error
	sortFilesByPathAndFilenameNaturally(files)
	return files, err
}

// showFolderPasswordForm renders the password entry form for a protected folder share.
func (h *FolderShareHandler) showFolderPasswordForm(w http.ResponseWriter, token, folderName, errMsg string) {
	if err := render(w, "folder_share_password.html", map[string]any{
		"Title":      "Protected Link",
		"Token":      token,
		"FolderName": folderName,
		"Error":      errMsg,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// FileEntry pairs a File with its display path relative to the shared folder root.
type FileEntry struct {
	models.File
	RelativePath string // empty if file is directly in the shared folder
}

// renderFolderListing renders the public folder view with all accessible files.
func (h *FolderShareHandler) renderFolderListing(w http.ResponseWriter, _ *http.Request, link *models.FolderShareLink) {
	files, err := h.folderFiles(link)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	folderName := path.Base(link.FolderPath)
	if link.FolderPath == "/" {
		folderName = "Root"
	}

	prefix := link.FolderPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	entries := make([]FileEntry, 0, len(files))
	for _, f := range files {
		rel := ""
		if f.LogicalPath != link.FolderPath {
			rel = strings.TrimPrefix(f.LogicalPath, prefix)
		}
		entries = append(entries, FileEntry{File: f, RelativePath: rel})
	}

	if err := render(w, "folder_share_view.html", map[string]any{
		"Title":      folderName + " — Shared Folder",
		"Token":      link.Token,
		"FolderName": folderName,
		"Files":      entries,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ShowFolderShareManagement handles GET /folders/view — share management for a folder (auth required).
func (h *FolderShareHandler) ShowFolderShareManagement(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	folderPath := sanitizeFolderPath(r.URL.Query().Get("path"))

	// Root is always valid; other folders must exist
	if folderPath != "/" {
		var folderCount, fileCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ?", user.ID, folderPath).Count(&folderCount)
		h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ? AND trashed_at IS NULL", user.ID, folderPath).Count(&fileCount)
		if folderCount == 0 && fileCount == 0 {
			http.NotFound(w, r)
			return
		}
	}

	var shareLinks []models.FolderShareLink
	h.db.Where("folder_path = ? AND user_id = ?", folderPath, user.ID).
		Order("created_at DESC").
		Find(&shareLinks)

	folderName := path.Base(folderPath)
	if folderPath == "/" {
		folderName = "Root"
	}

	flashMsg := flash.Get(w, r)
	if err := render(w, "folder_view.html", map[string]any{
		"Title":      folderName + " — Sharing",
		"User":       user,
		"FolderPath": folderPath,
		"FolderName": folderName,
		"ShareLinks": shareLinks,
		"Flash":      flashMsg,
	}); err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// CreateFolderShareLink handles POST /folders/share.
func (h *FolderShareHandler) CreateFolderShareLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	folderPath := sanitizeFolderPath(r.FormValue("folder_path"))

	if folderPath != "/" {
		var folderCount, fileCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ?", user.ID, folderPath).Count(&folderCount)
		h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ? AND trashed_at IS NULL", user.ID, folderPath).Count(&fileCount)
		if folderCount == 0 && fileCount == 0 {
			http.NotFound(w, r)
			return
		}
	}

	var expiresAt *time.Time
	if v := strings.TrimSpace(r.FormValue("expires_at")); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			http.Error(w, "Invalid expiry date (use YYYY-MM-DD)", http.StatusBadRequest)
			return
		}
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
		ph, err := auth.HashPassword(v, 10)
		if err != nil {
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		passwordHash = &ph
	}

	token, err := generateToken()
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	link := models.FolderShareLink{
		Token:        token,
		FolderPath:   folderPath,
		UserID:       user.ID,
		ExpiresAt:    expiresAt,
		MaxUses:      maxUses,
		PasswordHash: passwordHash,
	}
	if err := h.db.Create(&link).Error; err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	flash.Success(w, "Folder share link created.")
	http.Redirect(w, r, "/folders/view?path="+url.QueryEscape(folderPath), http.StatusSeeOther)
}

// RevokeFolderShareLink handles POST /f/{token}/revoke — only the owner may revoke.
func (h *FolderShareHandler) RevokeFolderShareLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	token := chi.URLParam(r, "token")

	var link models.FolderShareLink
	if err := h.db.Where("token = ? AND user_id = ?", token, user.ID).First(&link).Error; err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.db.Delete(&link).Error; err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	flash.Success(w, "Share link revoked.")
	http.Redirect(w, r, "/folders/view?path="+url.QueryEscape(link.FolderPath), http.StatusSeeOther)
}

// AccessFolderShareLink handles GET /f/{token} — public endpoint.
func (h *FolderShareHandler) AccessFolderShareLink(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link := h.lookupValidFolderLink(w, token)
	if link == nil {
		return
	}

	if link.PasswordHash != nil {
		if !h.isUnlocked(r, token) {
			h.showFolderPasswordForm(w, token, path.Base(link.FolderPath), "")
			return
		}
		// Already unlocked via session — render without consuming another use.
		h.renderFolderListing(w, r, link)
		return
	}

	if !h.consumeFolderUse(link) {
		http.NotFound(w, r)
		return
	}

	h.renderFolderListing(w, r, link)
}

// VerifyFolderSharePassword handles POST /f/{token} — validates password, unlocks via session.
func (h *FolderShareHandler) VerifyFolderSharePassword(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link := h.lookupValidFolderLink(w, token)
	if link == nil {
		return
	}

	if link.PasswordHash == nil {
		http.Redirect(w, r, "/f/"+token, http.StatusSeeOther)
		return
	}

	folderName := path.Base(link.FolderPath)
	password := r.FormValue("password")
	if !auth.VerifyPassword(*link.PasswordHash, password) {
		h.showFolderPasswordForm(w, token, folderName, "Incorrect password. Please try again.")
		return
	}

	if !h.consumeFolderUse(link) {
		http.NotFound(w, r)
		return
	}

	h.sessionManager.Put(r.Context(), folderShareSessionKey(token), true)
	http.Redirect(w, r, "/f/"+token, http.StatusSeeOther)
}

// DownloadSharedFolderFile handles GET /f/{token}/files/{id} — public file download.
func (h *FolderShareHandler) DownloadSharedFolderFile(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	link := h.lookupValidFolderLink(w, token)
	if link == nil {
		return
	}

	if link.PasswordHash != nil && !h.isUnlocked(r, token) {
		http.Redirect(w, r, "/f/"+token, http.StatusSeeOther)
		return
	}

	fileID, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid file ID", http.StatusBadRequest)
		return
	}

	// Verify the file belongs to the share owner and lives within the shared folder.
	var file models.File
	prefix := link.FolderPath
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if err := h.db.Where(
		"id = ? AND user_id = ? AND trashed_at IS NULL AND (logical_path = ? OR logical_path LIKE ?)",
		fileID, link.UserID, link.FolderPath, prefix+"%",
	).First(&file).Error; err != nil {
		http.NotFound(w, r)
		return
	}

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
		logger.Error("error streaming shared folder file", "path", file.StoragePath, "error", err)
	}
}

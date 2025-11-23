package handlers

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
	"gorm.io/gorm"
)

type FileHandler struct {
	db      *gorm.DB
	cfg     *config.Config
	storage *storage.Service
}

func NewFileHandler(db *gorm.DB, cfg *config.Config, storage *storage.Service) *FileHandler {
	return &FileHandler{
		db:      db,
		cfg:     cfg,
		storage: storage,
	}
}

// sanitizeFolderPath cleans and validates a folder path
func sanitizeFolderPath(path string) string {
	if path == "" {
		return "/"
	}

	// Clean the path (removes .., ., trailing slashes)
	path = filepath.Clean("/" + path)

	// Ensure it starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Don't allow going above root
	if path == "." || path == ".." {
		return "/"
	}

	return path
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse multipart form (max 500MB by default)
	if err := r.ParseMultipartForm(int64(h.cfg.MaxUploadSize)); err != nil {
		http.Error(w, "File too large", http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Check storage quota
	if user.StorageUsed+header.Size > user.StorageQuota {
		http.Error(w, "Storage quota exceeded", http.StatusInsufficientStorage)
		return
	}

	// Save file to disk
	filename, hash, size, err := h.storage.SaveFile(file, header.Filename)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
		return
	}

	// Get and sanitize folder path
	folderPath := sanitizeFolderPath(r.FormValue("folder"))

	// Create database record
	fileRecord := models.File{
		UserID:           user.ID,
		Filename:         filename,
		OriginalFilename: header.Filename,
		FilePath:         h.storage.GetFilePath(filename),
		FileSize:         size,
		MimeType:         header.Header.Get("Content-Type"),
		Hash:             hash,
		FolderPath:       folderPath,
	}

	if err := h.db.Create(&fileRecord).Error; err != nil {
		// Clean up file if database insert fails
		h.storage.DeleteFile(filename)
		http.Error(w, "Failed to save file metadata", http.StatusInternalServerError)
		return
	}

	// Update user storage
	if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
		UpdateColumn("storage_used", gorm.Expr("storage_used + ?", size)).Error; err != nil {
		// Don't fail the upload, but log it
		fmt.Printf("Warning: failed to update user storage: %v\n", err)
	}

	// Redirect back to the folder we uploaded to
	redirectURL := "/dashboard"
	if folderPath != "/" {
		redirectURL = "/dashboard?folder=" + folderPath
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (h *FileHandler) CreateFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	currentFolder := sanitizeFolderPath(r.FormValue("current_folder"))
	folderName := strings.TrimSpace(r.FormValue("folder_name"))

	if folderName == "" {
		http.Error(w, "Folder name is required", http.StatusBadRequest)
		return
	}

	// Validate folder name (no slashes, no ..)
	if strings.Contains(folderName, "/") || strings.Contains(folderName, "..") {
		http.Error(w, "Invalid folder name", http.StatusBadRequest)
		return
	}

	// Build new folder path
	newFolderPath := currentFolder
	if currentFolder == "/" {
		newFolderPath = "/" + folderName
	} else {
		newFolderPath = currentFolder + "/" + folderName
	}

	// Check if folder already exists
	var existingFolder models.Folder
	result := h.db.Where("user_id = ? AND folder_path = ?", user.ID, newFolderPath).First(&existingFolder)

	if result.Error == nil {
		// Folder already exists
		http.Redirect(w, r, "/dashboard?folder="+newFolderPath, http.StatusSeeOther)
		return
	}

	// Create the folder record
	folder := models.Folder{
		UserID:     user.ID,
		FolderPath: newFolderPath,
	}

	if err := h.db.Create(&folder).Error; err != nil {
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	// Redirect to the new folder
	http.Redirect(w, r, "/dashboard?folder="+newFolderPath, http.StatusSeeOther)
}

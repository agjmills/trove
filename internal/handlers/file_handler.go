package handlers

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
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

	// Get and sanitize folder path
	folderPath := sanitizeFolderPath(r.FormValue("folder"))

	// Check if file with same name exists in this folder and auto-rename if needed
	originalFilename := header.Filename
	finalFilename := h.getUniqueFilename(user.ID, folderPath, originalFilename)

	// Calculate hash first to check for deduplication
	hash, err := h.storage.CalculateHash(file)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to calculate file hash: %v", err), http.StatusInternalServerError)
		return
	}

	// Reset file reader to beginning after hashing
	if seeker, ok := file.(io.Seeker); ok {
		seeker.Seek(0, 0)
	}

	var filename string
	var actualSize int64
	var isDuplicate bool

	// Check if this file already exists (deduplication)
	var existingFile models.File
	result := h.db.Where("user_id = ? AND hash = ?", user.ID, hash).First(&existingFile)

	if result.Error == nil {
		// Duplicate found! Reuse the existing physical file
		filename = existingFile.Filename
		actualSize = existingFile.FileSize
		isDuplicate = true
	} else {
		// New file, save to disk
		filename, _, actualSize, err = h.storage.SaveFile(file, header.Filename)
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to save file: %v", err), http.StatusInternalServerError)
			return
		}
		isDuplicate = false
	}

	// Create database record
	fileRecord := models.File{
		UserID:           user.ID,
		Filename:         filename,
		OriginalFilename: finalFilename,
		FilePath:         h.storage.GetFilePath(filename),
		FileSize:         actualSize,
		MimeType:         header.Header.Get("Content-Type"),
		Hash:             hash,
		FolderPath:       folderPath,
	}

	if err := h.db.Create(&fileRecord).Error; err != nil {
		// Clean up file if database insert fails (only if not a duplicate)
		if !isDuplicate {
			h.storage.DeleteFile(filename)
		}
		http.Error(w, "Failed to save file metadata", http.StatusInternalServerError)
		return
	}

	// Update user storage (only if not a duplicate)
	if !isDuplicate {
		if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
			UpdateColumn("storage_used", gorm.Expr("storage_used + ?", actualSize)).Error; err != nil {
			// Don't fail the upload, but log it
			fmt.Printf("Warning: failed to update user storage: %v\n", err)
		}
	}

	// Show success message with renamed filename if applicable
	if finalFilename != originalFilename {
		flash.Success(w, fmt.Sprintf("File uploaded as '%s'", finalFilename))
	} else {
		flash.Success(w, "File uploaded successfully.")
	}

	// Redirect back to the folder we uploaded to
	redirectURL := "/dashboard"
	if folderPath != "/" {
		redirectURL = "/dashboard?folder=" + folderPath
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// getUniqueFilename checks if a file with the same name exists and returns a unique name
func (h *FileHandler) getUniqueFilename(userID uint, folderPath, originalFilename string) string {
	// Check if file exists
	var count int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND folder_path = ? AND original_filename = ?", userID, folderPath, originalFilename).
		Count(&count)

	if count == 0 {
		return originalFilename
	}

	// File exists, find a unique name
	ext := filepath.Ext(originalFilename)
	nameWithoutExt := strings.TrimSuffix(originalFilename, ext)

	for i := 1; ; i++ {
		newName := fmt.Sprintf("%s (%d)%s", nameWithoutExt, i, ext)
		h.db.Model(&models.File{}).
			Where("user_id = ? AND folder_path = ? AND original_filename = ?", userID, folderPath, newName).
			Count(&count)

		if count == 0 {
			return newName
		}
	}
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
		flash.Error(w, "A folder with that name already exists.")
		http.Redirect(w, r, "/dashboard?folder="+currentFolder, http.StatusSeeOther)
		return
	}

	// Create the folder record
	folder := models.Folder{
		UserID:     user.ID,
		FolderPath: newFolderPath,
	}

	if err := h.db.Create(&folder).Error; err != nil {
		flash.Error(w, "Failed to create folder. Please try again.")
		http.Redirect(w, r, "/dashboard?folder="+currentFolder, http.StatusSeeOther)
		return
	}

	flash.Success(w, "Folder created successfully.")
	// Redirect to the new folder
	http.Redirect(w, r, "/dashboard?folder="+newFolderPath, http.StatusSeeOther)
}

func (h *FileHandler) Download(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get file ID from URL
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "File ID is required", http.StatusBadRequest)
		return
	}

	// Fetch file from database
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Check if file exists on disk
	if !h.storage.FileExists(file.Filename) {
		http.Error(w, "File not found on disk", http.StatusNotFound)
		return
	}

	// Open file
	filePath := h.storage.GetFilePath(file.Filename)
	f, err := h.storage.OpenFile(file.Filename)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Set headers
	w.Header().Set("Content-Type", file.MimeType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.OriginalFilename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", file.FileSize))

	// Stream file to response
	http.ServeFile(w, r, filePath)
}

func (h *FileHandler) Delete(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get file ID from URL
	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "File ID is required", http.StatusBadRequest)
		return
	}

	// Fetch file from database
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Store the folder path and size before deletion
	folderPath := file.FolderPath
	fileSize := file.FileSize
	physicalFilename := file.Filename

	// Delete from database
	if err := h.db.Delete(&file).Error; err != nil {
		http.Error(w, "Failed to delete file from database", http.StatusInternalServerError)
		return
	}

	// Check if any other File records reference this physical file (deduplication check)
	var refCount int64
	h.db.Model(&models.File{}).Where("user_id = ? AND filename = ?", user.ID, physicalFilename).Count(&refCount)

	// Only delete from disk if no other references exist
	shouldDeleteFromDisk := refCount == 0
	if shouldDeleteFromDisk {
		if err := h.storage.DeleteFile(physicalFilename); err != nil {
			// Log the error but don't fail the request since DB record is already gone
			fmt.Printf("Warning: failed to delete file from disk: %v\n", err)
		}

		// Update user storage quota (only if physical file was deleted)
		if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
			UpdateColumn("storage_used", gorm.Expr("storage_used - ?", fileSize)).Error; err != nil {
			fmt.Printf("Warning: failed to update user storage: %v\n", err)
		}
	}

	flash.Success(w, "File deleted successfully.")

	// Redirect back to the folder
	redirectURL := "/dashboard"
	if folderPath != "/" {
		redirectURL = "/dashboard?folder=" + folderPath
	}
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (h *FileHandler) DeleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get folder name from URL
	folderName := chi.URLParam(r, "name")
	if folderName == "" {
		http.Error(w, "Folder name is required", http.StatusBadRequest)
		return
	}

	currentFolder := sanitizeFolderPath(r.FormValue("current_folder"))

	// Build full folder path
	fullFolderPath := currentFolder
	if currentFolder == "/" {
		fullFolderPath = "/" + folderName
	} else {
		fullFolderPath = currentFolder + "/" + folderName
	}

	// Check if folder has any files
	var fileCount int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND folder_path LIKE ?", user.ID, fullFolderPath+"%").
		Count(&fileCount)

	if fileCount > 0 {
		flash.Error(w, "Cannot delete folder: folder contains files. Please delete or move the files first.")
		http.Redirect(w, r, "/dashboard?folder="+currentFolder, http.StatusSeeOther)
		return
	}

	// Delete the folder record
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, fullFolderPath).
		Delete(&models.Folder{}).Error; err != nil {
		flash.Error(w, "Failed to delete folder. Please try again.")
		http.Redirect(w, r, "/dashboard?folder="+currentFolder, http.StatusSeeOther)
		return
	}

	flash.Success(w, "Folder deleted successfully.")
	// Redirect back to parent folder
	http.Redirect(w, r, "/dashboard?folder="+currentFolder, http.StatusSeeOther)
}

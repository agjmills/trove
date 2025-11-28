package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type uploadJob struct {
	fileID   uint
	tempPath string
}

type FileHandler struct {
	db          *gorm.DB
	cfg         *config.Config
	storage     storage.StorageBackend
	uploadQueue chan uploadJob
	wg          sync.WaitGroup
	pendingJobs sync.WaitGroup // tracks jobs currently being processed
}

func NewFileHandler(db *gorm.DB, cfg *config.Config, storage storage.StorageBackend) *FileHandler {
	h := &FileHandler{
		db:          db,
		cfg:         cfg,
		storage:     storage,
		uploadQueue: make(chan uploadJob, 100), // Buffer up to 100 pending uploads
	}

	// Start background workers (adjust number based on your needs)
	numWorkers := 3
	for i := 0; i < numWorkers; i++ {
		h.wg.Add(1)
		go h.uploadWorker()
	}

	return h
}

// Shutdown gracefully stops the background workers
func (h *FileHandler) Shutdown() {
	close(h.uploadQueue)
	h.wg.Wait()
}

// WaitForPendingUploads waits for all currently queued uploads to complete.
// This is useful in tests to ensure background processing finishes before assertions.
func (h *FileHandler) WaitForPendingUploads() {
	h.pendingJobs.Wait()
}

// uploadWorker processes background uploads
func (h *FileHandler) uploadWorker() {
	defer h.wg.Done()

	for job := range h.uploadQueue {
		h.processUpload(job)
		h.pendingJobs.Done()
	}
}

// processUpload handles the actual S3 upload in the background
func (h *FileHandler) processUpload(job uploadJob) {
	ctx := context.Background()

	// Get file record
	var file models.File
	if err := h.db.First(&file, job.fileID).Error; err != nil {
		log.Printf("Upload worker: failed to find file %d: %v", job.fileID, err)
		os.Remove(job.tempPath)
		return
	}

	// Update status to uploading
	h.db.Model(&file).Update("upload_status", "uploading")

	// Check for deduplication
	var existingFile models.File
	if err := h.db.Where("user_id = ? AND hash = ? AND upload_status = ?", file.UserID, file.Hash, "completed").
		Where("id != ?", file.ID).First(&existingFile).Error; err == nil {
		// Duplicate found - reuse existing storage path
		log.Printf("Upload worker: deduplication - reusing storage path %s", existingFile.StoragePath)

		if err := h.db.Model(&file).Updates(map[string]interface{}{
			"storage_path":  existingFile.StoragePath,
			"upload_status": "completed",
			"temp_path":     "",
		}).Error; err != nil {
			log.Printf("Upload worker: failed to update file record: %v", err)
		}

		// Clean up temp file
		os.Remove(job.tempPath)
		return
	}

	// Open temp file
	tempFile, err := os.Open(job.tempPath)
	if err != nil {
		log.Printf("Upload worker: failed to open temp file: %v", err)
		h.markUploadFailed(file, "Failed to process uploaded file")
		os.Remove(job.tempPath)
		return
	}
	defer tempFile.Close()

	// Upload to storage backend
	log.Printf("Upload worker: uploading file %d to storage backend", job.fileID)
	result, err := h.storage.Save(ctx, tempFile, storage.SaveOptions{
		OriginalFilename: file.OriginalFilename,
		ContentType:      file.MimeType,
	})

	if err != nil {
		log.Printf("Upload worker: failed to upload file %d: %v", job.fileID, err)
		h.markUploadFailed(file, "Storage upload failed: "+err.Error())
		os.Remove(job.tempPath)
		return
	}

	// Update file record with storage path and mark as completed
	if err := h.db.Model(&file).Updates(map[string]interface{}{
		"storage_path":  result.Path,
		"upload_status": "completed",
		"temp_path":     "",
	}).Error; err != nil {
		log.Printf("Upload worker: failed to update file record: %v", err)
		// Try to clean up the uploaded file
		h.storage.Delete(ctx, result.Path)
		os.Remove(job.tempPath)
		return
	}

	log.Printf("Upload worker: successfully uploaded file %d as %s", job.fileID, result.Path)

	// Clean up temp file
	os.Remove(job.tempPath)
}

// CleanupFailedUploads removes failed/pending upload records for a user and restores their storage quota.
// This function is transactional and idempotent: it computes the total size of files to be deleted
// within the transaction, updates storage_used once with that total, then deletes the file rows.
// This prevents race conditions from concurrent cleanup attempts and double-decrementing quota.
//
// Parameters:
//   - db: the GORM database handle
//   - userID: the user whose failed uploads should be cleaned up
//
// Returns the number of files cleaned up and any error encountered.
func CleanupFailedUploads(db *gorm.DB, userID uint) (int64, error) {
	var totalCleaned int64

	err := db.Transaction(func(tx *gorm.DB) error {
		// Calculate total size of files to be deleted (within transaction for consistency)
		var totalSize int64
		if err := tx.Model(&models.File{}).
			Select("COALESCE(SUM(file_size), 0)").
			Where("user_id = ? AND upload_status IN (?)", userID, []string{"pending", "failed"}).
			Scan(&totalSize).Error; err != nil {
			return err
		}

		// Delete all failed/pending files for this user
		result := tx.Where("user_id = ? AND upload_status IN (?)", userID, []string{"pending", "failed"}).
			Delete(&models.File{})
		if result.Error != nil {
			return result.Error
		}

		totalCleaned = result.RowsAffected

		// Only restore quota if we actually deleted records and had size to restore
		if totalCleaned > 0 && totalSize > 0 {
			if err := tx.Model(&models.User{}).Where("id = ?", userID).
				UpdateColumn("storage_used", gorm.Expr("CASE WHEN storage_used >= ? THEN storage_used - ? ELSE 0 END", totalSize, totalSize)).Error; err != nil {
				return err
			}
			log.Printf("Cleanup: removed %d failed uploads, restored %d bytes to user %d", totalCleaned, totalSize, userID)
		}

		return nil
	})

	return totalCleaned, err
}

// markUploadFailed marks a file as failed with an error message.
// This preserves the record so users can see what failed and optionally retry.
func (h *FileHandler) markUploadFailed(file models.File, errorMessage string) {
	// Truncate error message if too long
	if len(errorMessage) > 500 {
		// Truncate at rune boundary to avoid breaking UTF-8
		runes := []rune(errorMessage)
		if len(runes) > 497 {
			errorMessage = string(runes[:497]) + "..."
		}
	}

	if err := h.db.Model(&file).Updates(map[string]interface{}{
		"upload_status": "failed",
		"error_message": errorMessage,
		"temp_path":     "",
	}).Error; err != nil {
		log.Printf("Upload worker: failed to mark file %d as failed: %v", file.ID, err)
	} else {
		log.Printf("Upload worker: marked file %d as failed: %s", file.ID, errorMessage)
	}
}

// cleanupFailedUpload removes a failed upload's database record and restores the user's storage quota.
// This function is idempotent and safe for concurrent calls: it uses a transaction to ensure
// atomicity, and only operates on files with "pending" or "failed" upload status to prevent
// double-subtracting quota if cleanup has already occurred.
// Returns an error if the cleanup fails.
func (h *FileHandler) cleanupFailedUpload(file models.File) error {
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Only clean up if the file is still in a failed/pending state (idempotency guard)
		result := tx.Where("id = ? AND upload_status IN (?)", file.ID, []string{"pending", "failed"}).
			Delete(&models.File{})
		if result.Error != nil {
			return result.Error
		}

		// Only restore quota if we actually deleted a record
		if result.RowsAffected > 0 {
			if err := tx.Model(&models.User{}).Where("id = ?", file.UserID).
				UpdateColumn("storage_used", gorm.Expr("CASE WHEN storage_used >= ? THEN storage_used - ? ELSE 0 END", file.FileSize, file.FileSize)).Error; err != nil {
				return err
			}
			log.Printf("Upload worker: cleaned up failed upload %d, restored %d bytes to user %d", file.ID, file.FileSize, file.UserID)
		} else {
			log.Printf("Upload worker: file %d already cleaned up, skipping", file.ID)
		}

		return nil
	})

	if err != nil {
		log.Printf("Upload worker: failed to cleanup file %d: %v", file.ID, err)
	}

	return err
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

// folderRedirectURL builds a URL-safe redirect path for the files page.
// Returns "/files" for root folder, or "/files?folder=<encoded>" otherwise.
func folderRedirectURL(folderPath string) string {
	if folderPath == "" || folderPath == "/" {
		return "/files"
	}
	return "/files?folder=" + url.QueryEscape(folderPath)
}

// escapeSQLLike escapes special characters in a string for use in SQL LIKE patterns.
// It escapes the backslash itself, percent signs, and underscores to prevent
// them from being interpreted as SQL wildcards.
func escapeSQLLike(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

func (h *FileHandler) Upload(w http.ResponseWriter, r *http.Request) {
	log.Printf("Upload handler: MaxUploadSize configured as %d bytes (%.2f MB)", h.cfg.MaxUploadSize, float64(h.cfg.MaxUploadSize)/(1024*1024))

	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Check Content-Length header first (if provided) for early rejection
	if r.ContentLength > 0 && r.ContentLength > h.cfg.MaxUploadSize {
		log.Printf("Upload rejected: Content-Length %d exceeds limit %d", r.ContentLength, h.cfg.MaxUploadSize)
		http.Error(w, fmt.Sprintf("File too large (max %d MB)", h.cfg.MaxUploadSize/(1024*1024)), http.StatusRequestEntityTooLarge)
		return
	}

	// Wrap request body with MaxBytesReader for streaming protection
	r.Body = http.MaxBytesReader(w, r.Body, h.cfg.MaxUploadSize)

	// Extract boundary from Content-Type header for direct multipart parsing
	contentType := r.Header.Get("Content-Type")
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		http.Error(w, "Invalid content type", http.StatusBadRequest)
		return
	}
	boundary := params["boundary"]
	if boundary == "" {
		http.Error(w, "Missing multipart boundary", http.StatusBadRequest)
		return
	}

	mr := multipart.NewReader(r.Body, boundary)

	var folderPath string = "/"
	var fileProcessed bool
	var originalFilename string
	var hash string
	var actualSize int64
	var mimeType string
	var tempFilePath string

	// Ensure temp file cleanup on all exit paths
	defer func() {
		if tempFilePath != "" {
			os.Remove(tempFilePath)
		}
	}()

	// Parse multipart form: stream file to temp while computing hash
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				log.Printf("Upload rejected: exceeded max size during multipart parsing")
				http.Error(w, fmt.Sprintf("File too large (max %d MB)", h.cfg.MaxUploadSize/(1024*1024)), http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "Failed to parse multipart form", http.StatusBadRequest)
			return
		}

		formName := part.FormName()

		switch formName {
		case "folder":
			data, err := io.ReadAll(io.LimitReader(part, 1024))
			if err != nil {
				log.Printf("Warning: failed to read folder field: %v", err)
				folderPath = "/"
			} else {
				folderPath = sanitizeFolderPath(string(data))
			}

		case "file":
			if fileProcessed {
				part.Close()
				continue
			}

			originalFilename = part.FileName()
			if originalFilename == "" {
				part.Close()
				continue
			}

			mimeType = part.Header.Get("Content-Type")
			if mimeType == "" {
				mimeType = "application/octet-stream"
			}

			log.Printf("Upload: streaming %s to temp file", originalFilename)

			// Create temp file (use configured temp dir, or system default if empty)
			tempFile, err := os.CreateTemp(h.cfg.TempDir, "trove-upload-*")
			if err != nil {
				part.Close()
				log.Printf("Failed to create temp file in %q: %v", h.cfg.TempDir, err)
				http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
				return
			}
			tempFilePath = tempFile.Name()

			// Stream to temp file while computing hash
			hasher := sha256.New()
			multiWriter := io.MultiWriter(tempFile, hasher)

			written, err := io.Copy(multiWriter, part)
			tempFile.Close()
			part.Close()

			if err != nil {
				var maxBytesErr *http.MaxBytesError
				if errors.As(err, &maxBytesErr) {
					log.Printf("Upload rejected: file exceeds limit")
					http.Error(w, fmt.Sprintf("File too large (max %d MB)", h.cfg.MaxUploadSize/(1024*1024)), http.StatusRequestEntityTooLarge)
					return
				}
				http.Error(w, "Failed to process upload", http.StatusInternalServerError)
				return
			}

			hash = hex.EncodeToString(hasher.Sum(nil))
			actualSize = written

			hashPreview := hash
			if len(hash) > 16 {
				hashPreview = hash[:16] + "..."
			}
			log.Printf("Upload: temp file complete, size=%d bytes, hash=%s", actualSize, hashPreview)

			fileProcessed = true
			continue

		default:
			io.Copy(io.Discard, part)
		}

		part.Close()
	}

	if !fileProcessed {
		http.Error(w, "No file provided", http.StatusBadRequest)
		return
	}

	// Check storage quota before proceeding
	if user.StorageUsed+actualSize > user.StorageQuota {
		http.Error(w, "Storage quota exceeded", http.StatusInsufficientStorage)
		return
	}

	// Calculate unique display filename for this folder
	displayFilename := h.getUniqueFilename(user.ID, folderPath, originalFilename)

	// Check if we already have a completed file with this hash (fast deduplication)
	var existingFile models.File
	existingErr := h.db.Where("user_id = ? AND hash = ? AND upload_status = ?", user.ID, hash, "completed").
		First(&existingFile).Error

	var storagePath string
	var uploadStatus string
	var tempPathForDB string
	var isDuplicate bool

	if existingErr == nil {
		// Immediate deduplication - reuse existing storage path
		storagePath = existingFile.StoragePath
		uploadStatus = "completed"
		isDuplicate = true
		log.Printf("Deduplication: hash %s exists, reusing %s (no upload needed)", hash[:16], storagePath)
		// Can delete temp file immediately
		defer os.Remove(tempFilePath)
	} else {
		// New file - will be uploaded in background
		// Use a placeholder path that will be updated by the worker
		storagePath = fmt.Sprintf("pending-%s%s", uuid.New().String(), filepath.Ext(originalFilename))
		uploadStatus = "pending"
		tempPathForDB = tempFilePath
		isDuplicate = false
		log.Printf("Upload: queued for background processing")
		// DON'T delete temp file yet - worker will do it
		tempFilePath = "" // Clear so defer doesn't delete it
	}

	// Create database record immediately
	fileRecord := models.File{
		UserID:           user.ID,
		StoragePath:      storagePath,
		LogicalPath:      folderPath,
		Filename:         displayFilename,
		OriginalFilename: originalFilename,
		FileSize:         actualSize,
		MimeType:         mimeType,
		Hash:             hash,
		UploadStatus:     uploadStatus,
		TempPath:         tempPathForDB,
	}

	if err := h.db.Create(&fileRecord).Error; err != nil {
		http.Error(w, "Failed to save file metadata", http.StatusInternalServerError)
		return
	}

	// Update user storage quota (always count it immediately)
	if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
		UpdateColumn("storage_used", gorm.Expr("storage_used + ?", actualSize)).Error; err != nil {
		log.Printf("Warning: failed to update user storage: %v", err)
	}

	// Queue background upload if not a duplicate
	if !isDuplicate {
		h.pendingJobs.Add(1)
		select {
		case h.uploadQueue <- uploadJob{fileID: fileRecord.ID, tempPath: tempPathForDB}:
			log.Printf("Upload: queued file %d for background upload", fileRecord.ID)
		default:
			h.pendingJobs.Done() // Undo the Add since job won't be processed
			log.Printf("Warning: upload queue full, marking file %d as failed", fileRecord.ID)
			// Queue is full - mark as failed instead of deleting
			h.markUploadFailed(fileRecord, "Upload queue is full. Please try again later.")
			// Clean up temp file since no worker will process it
			os.Remove(tempPathForDB)
		}
	}

	// Success message
	if isDuplicate {
		flash.Success(w, fmt.Sprintf("File '%s' uploaded (deduplicated)", displayFilename))
	} else if displayFilename != originalFilename {
		flash.Success(w, fmt.Sprintf("File uploaded as '%s'", displayFilename))
	} else {
		flash.Success(w, "File uploaded successfully.")
	}

	http.Redirect(w, r, folderRedirectURL(folderPath), http.StatusSeeOther)
}

// getUniqueFilename checks if a file with the same name exists in the folder and returns a unique name
func (h *FileHandler) getUniqueFilename(userID uint, logicalPath, originalFilename string) string {
	// Check if file exists in this folder
	var count int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ? AND filename = ?", userID, logicalPath, originalFilename).
		Count(&count)

	if count == 0 {
		return originalFilename
	}

	// File exists, find a unique name
	ext := filepath.Ext(originalFilename)
	nameWithoutExt := strings.TrimSuffix(originalFilename, ext)

	for i := 1; i <= 10000; i++ {
		newName := fmt.Sprintf("%s (%d)%s", nameWithoutExt, i, ext)
		h.db.Model(&models.File{}).
			Where("user_id = ? AND logical_path = ? AND filename = ?", userID, logicalPath, newName).
			Count(&count)

		if count == 0 {
			return newName
		}
	}

	// Fallback: use UUID suffix if too many collisions
	return fmt.Sprintf("%s (%s)%s", nameWithoutExt, uuid.New().String()[:8], ext)
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
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Create the folder record
	folder := models.Folder{
		UserID:     user.ID,
		FolderPath: newFolderPath,
	}

	if err := h.db.Create(&folder).Error; err != nil {
		flash.Error(w, "Failed to create folder. Please try again.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	flash.Success(w, "Folder created successfully.")
	// Redirect to the new folder
	http.Redirect(w, r, folderRedirectURL(newFolderPath), http.StatusSeeOther)
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

	ctx := r.Context()

	// Check if file exists in storage
	_, err := h.storage.Stat(ctx, file.StoragePath)
	if errors.Is(err, storage.ErrNotFound) {
		http.Error(w, "File not found in storage", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Failed to access file", http.StatusInternalServerError)
		return
	}

	// Open file from storage
	reader, err := h.storage.Open(ctx, file.StoragePath)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "File not found in storage", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer reader.Close()

	// Set headers - use Filename for display name
	w.Header().Set("Content-Type", file.MimeType)
	// Escape quotes in filename and add UTF-8 encoded version for non-ASCII support
	safeFilename := strings.ReplaceAll(file.Filename, `"`, `\"`)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		safeFilename, url.PathEscape(file.Filename)))
	w.Header().Set("Content-Length", strconv.FormatInt(file.FileSize, 10))

	// Stream file to response
	if _, err := io.Copy(w, reader); err != nil {
		log.Printf("Warning: error streaming file %s: %v", file.StoragePath, err)
	}
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

	// Store the size and storage path before deletion
	fileSize := file.FileSize
	storagePath := file.StoragePath

	// Delete from database
	if err := h.db.Delete(&file).Error; err != nil {
		http.Error(w, "Failed to delete file from database", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// Check if any other File records reference this physical file (deduplication check)
	var refCount int64
	h.db.Model(&models.File{}).Where("user_id = ? AND storage_path = ?", user.ID, storagePath).Count(&refCount)

	// Only delete from storage if no other references exist
	shouldDeleteFromStorage := refCount == 0
	if shouldDeleteFromStorage {
		if err := h.storage.Delete(ctx, storagePath); err != nil {
			// Log the error but don't fail the request since DB record is already gone
			fmt.Printf("Warning: failed to delete file from storage: %v\n", err)
		}

		// Update user storage quota (only if physical file was deleted)
		if err := h.db.Model(&models.User{}).Where("id = ?", user.ID).
			UpdateColumn("storage_used", gorm.Expr("storage_used - ?", fileSize)).Error; err != nil {
			fmt.Printf("Warning: failed to update user storage: %v\n", err)
		}
	}

	flash.Success(w, "File deleted successfully.")

	// Redirect back to the folder the file was in
	http.Redirect(w, r, folderRedirectURL(file.LogicalPath), http.StatusSeeOther)
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
		Where("user_id = ? AND logical_path = ?", user.ID, fullFolderPath).
		Count(&fileCount)

	if fileCount > 0 {
		flash.Error(w, "Cannot delete folder: it contains files.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Check if folder has any subfolders
	var subfolderCount int64
	escapedFullFolderPath := escapeSQLLike(fullFolderPath)
	h.db.Model(&models.Folder{}).
		Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\' AND folder_path != ?", user.ID, escapedFullFolderPath+"/%", fullFolderPath).
		Count(&subfolderCount)

	if subfolderCount > 0 {
		flash.Error(w, "Cannot delete folder: it contains subfolders.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Check if folder exists
	var existingFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, fullFolderPath).First(&existingFolder).Error; err != nil {
		flash.Error(w, "Folder not found.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Delete the folder record
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, fullFolderPath).
		Delete(&models.Folder{}).Error; err != nil {
		flash.Error(w, "Failed to delete folder. Please try again.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	flash.Success(w, "Folder deleted successfully.")
	// Redirect back to parent folder
	http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
}

// FileStatusEvent represents a file status update for SSE
type FileStatusEvent struct {
	ID           uint   `json:"id"`
	UploadStatus string `json:"upload_status"`
	ErrorMessage string `json:"error_message,omitempty"`
	Filename     string `json:"filename"`
}

// StatusStream provides Server-Sent Events for file upload status updates.
// Clients connect to this endpoint to receive real-time updates when files
// transition between pending, uploading, completed, and failed states.
func (h *FileHandler) StatusStream(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	// Track last known state to detect changes
	lastState := make(map[uint]string)

	// Poll interval
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Context for cleanup
	ctx := r.Context()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Query for files that are not completed (pending, uploading, failed)
			var files []models.File
			if err := h.db.Where("user_id = ? AND upload_status IN (?)", user.ID, []string{"pending", "uploading", "failed"}).
				Find(&files).Error; err != nil {
				log.Printf("SSE: failed to query files: %v", err)
				continue
			}

			// Also check for recently completed files (completed in last 5 seconds)
			var recentlyCompleted []models.File
			fiveSecondsAgo := time.Now().Add(-5 * time.Second)
			if err := h.db.Where("user_id = ? AND upload_status = ? AND updated_at > ?", user.ID, "completed", fiveSecondsAgo).
				Find(&recentlyCompleted).Error; err != nil {
				log.Printf("SSE: failed to query recently completed files: %v", err)
			}
			files = append(files, recentlyCompleted...)

			// Build current state and detect changes
			currentState := make(map[uint]string)
			for _, file := range files {
				currentState[file.ID] = file.UploadStatus

				// Check if state changed or is new
				if oldStatus, exists := lastState[file.ID]; !exists || oldStatus != file.UploadStatus {
					event := FileStatusEvent{
						ID:           file.ID,
						UploadStatus: file.UploadStatus,
						ErrorMessage: file.ErrorMessage,
						Filename:     file.OriginalFilename,
					}

					data, err := json.Marshal(event)
					if err != nil {
						log.Printf("SSE: failed to marshal event: %v", err)
						continue
					}

					fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
					flusher.Flush()
				}
			}

			// Clean up entries from lastState that are no longer being tracked
			// This prevents the map from growing indefinitely during long-lived connections
			for id := range lastState {
				if _, exists := currentState[id]; !exists {
					delete(lastState, id)
				}
			}

			// Update last known state
			lastState = currentState
		}
	}
}

// DismissFailedUpload removes a failed upload and restores the user's quota.
// This allows users to acknowledge and dismiss failed uploads from the UI.
func (h *FileHandler) DismissFailedUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	fileID := chi.URLParam(r, "id")
	if fileID == "" {
		http.Error(w, "File ID is required", http.StatusBadRequest)
		return
	}

	// Find the file
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Only allow dismissing failed uploads
	if file.UploadStatus != "failed" {
		http.Error(w, "Can only dismiss failed uploads", http.StatusBadRequest)
		return
	}

	// Use the cleanup function to properly restore quota
	if err := h.cleanupFailedUpload(file); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to dismiss upload: " + err.Error(),
		})
		return
	}

	// Return success
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

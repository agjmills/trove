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
	"github.com/gorilla/csrf"
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

// isAllowedOrigin checks if the given origin is in the configured CORS allowlist.
// Returns false if the allowlist is empty or the origin is not found.
func (h *FileHandler) isAllowedOrigin(origin string) bool {
	for _, allowed := range h.cfg.CORSAllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
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
		h.markUploadFailed(file, "Storage upload failed")
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
		flash.Success(w, fmt.Sprintf("File \"%s\" uploaded (deduplicated)", displayFilename))
	} else if displayFilename != originalFilename {
		flash.Success(w, fmt.Sprintf("File uploaded as \"%s\"", displayFilename))
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

// Preview serves a file for in-browser preview (inline disposition)
func (h *FileHandler) Preview(w http.ResponseWriter, r *http.Request) {
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

	// Set headers for inline display
	w.Header().Set("Content-Type", file.MimeType)
	// Use inline disposition to allow browser preview
	safeFilename := strings.ReplaceAll(file.Filename, `"`, `\"`)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="%s"; filename*=UTF-8''%s`,
		safeFilename, url.PathEscape(file.Filename)))
	w.Header().Set("Content-Length", strconv.FormatInt(file.FileSize, 10))

	// Add security headers for preview
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; media-src 'self'; img-src 'self'; script-src 'none';")

	// Stream file to response
	if _, err := io.Copy(w, reader); err != nil {
		log.Printf("Warning: error streaming file %s: %v", file.StoragePath, err)
	}
}

// ViewFile displays a file view page with preview and metadata
func (h *FileHandler) ViewFile(w http.ResponseWriter, r *http.Request) {
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

	// Get all folders for move dropdown
	type FolderData struct {
		Path string
	}
	var folders []FolderData
	
	// Get explicit folders
	h.db.Model(&models.Folder{}).
		Select("folder_path as path").
		Where("user_id = ? AND deleted_at IS NULL AND trashed_at IS NULL", user.ID).
		Order("folder_path").
		Scan(&folders)
	
	// Get implicit folders from files
	h.db.Model(&models.File{}).
		Select("DISTINCT logical_path as path").
		Where("user_id = ? AND trashed_at IS NULL AND logical_path != '/'", user.ID).
		Order("logical_path").
		Scan(&folders)
	
	// Deduplicate folders
	folderMap := make(map[string]bool)
	for _, f := range folders {
		if f.Path != "" && f.Path != "/" {
			folderMap[f.Path] = true
		}
	}
	
	// Convert back to slice
	uniqueFolders := make([]FolderData, 0, len(folderMap))
	for path := range folderMap {
		uniqueFolders = append(uniqueFolders, FolderData{Path: path})
	}

	// Determine preview capabilities
	canPreview := false
	isImage := false
	isPDF := false
	isVideo := false
	isAudio := false
	isText := false

	mimeType := strings.ToLower(file.MimeType)
	filename := strings.ToLower(file.Filename)

	// Check if file can be previewed
	if strings.HasPrefix(mimeType, "image/") {
		canPreview = true
		isImage = true
	} else if mimeType == "application/pdf" {
		canPreview = true
		isPDF = true
	} else if strings.HasPrefix(mimeType, "video/") {
		canPreview = true
		isVideo = true
	} else if strings.HasPrefix(mimeType, "audio/") {
		canPreview = true
		isAudio = true
	} else if strings.HasPrefix(mimeType, "text/") ||
		mimeType == "application/json" ||
		mimeType == "application/xml" ||
		mimeType == "application/javascript" ||
		mimeType == "application/x-sh" ||
		isCodeFile(filename) {
		canPreview = true
		isText = true
	}

	// Render template
	data := map[string]interface{}{
		"Title":      file.Filename,
		"User":       user,
		"CSRFToken":  csrf.Token(r),
		"File":       file,
		"AllFolders": uniqueFolders,
		"CanPreview": canPreview,
		"IsImage":    isImage,
		"IsPDF":      isPDF,
		"IsVideo":    isVideo,
		"IsAudio":    isAudio,
		"IsText":     isText,
		"FullWidth":  true,
	}

	if err := render(w, "file_view.html", data); err != nil {
		log.Printf("Error rendering file view template: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// isCodeFile checks if a filename has a code file extension
func isCodeFile(filename string) bool {
	codeExtensions := []string{
		".go", ".py", ".js", ".ts", ".jsx", ".tsx", ".java", ".c", ".cpp", ".h", ".hpp",
		".cs", ".php", ".rb", ".rs", ".swift", ".kt", ".scala", ".r", ".m", ".mm",
		".sh", ".bash", ".zsh", ".fish", ".ps1", ".bat", ".cmd",
		".html", ".htm", ".css", ".scss", ".sass", ".less",
		".xml", ".yaml", ".yml", ".toml", ".ini", ".conf", ".config",
		".json", ".md", ".markdown", ".rst", ".txt", ".log",
		".sql", ".graphql", ".proto", ".vue", ".svelte",
	}
	
	lowerFilename := strings.ToLower(filename)
	
	// Check exact matches for files without extensions
	if lowerFilename == "makefile" || lowerFilename == "dockerfile" || lowerFilename == "readme" {
		return true
	}
	
	// Check extensions
	for _, ext := range codeExtensions {
		if strings.HasSuffix(lowerFilename, ext) {
			return true
		}
	}
	
	return false
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

	// Fetch file from database (only non-deleted files)
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NULL", fileID, user.ID).First(&file).Error; err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	// Soft delete - keep file at original location, just mark as trashed
	now := time.Now()
	if err := h.db.Model(&file).Updates(map[string]interface{}{
		"trashed_at":            now,
		"original_logical_path": file.LogicalPath,
	}).Error; err != nil {
		http.Error(w, "Failed to delete file", http.StatusInternalServerError)
		return
	}

	flash.Success(w, "File deleted.")

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

	// Check if folder exists (only non-deleted folders)
	var existingFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ? AND trashed_at IS NULL", user.ID, fullFolderPath).First(&existingFolder).Error; err != nil {
		flash.Error(w, "Folder not found.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Soft delete folder and all its contents
	now := time.Now()
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Soft delete the folder itself
		if err := tx.Model(&existingFolder).Updates(map[string]interface{}{
			"trashed_at":           now,
			"original_folder_path": fullFolderPath,
		}).Error; err != nil {
			return err
		}

		// Soft delete all files in this folder
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path = ? AND trashed_at IS NULL", user.ID, fullFolderPath).
			Updates(map[string]interface{}{
				"trashed_at":            now,
				"original_logical_path": gorm.Expr("logical_path"),
			}).Error; err != nil {
			return err
		}

		// Soft delete all subfolders
		escapedFullFolderPath := escapeSQLLike(fullFolderPath)
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\' AND folder_path != ? AND trashed_at IS NULL",
				user.ID, escapedFullFolderPath+"/%", fullFolderPath).
			Updates(map[string]interface{}{
				"trashed_at":           now,
				"original_folder_path": gorm.Expr("folder_path"),
			}).Error; err != nil {
			return err
		}

		// Soft delete all files in subfolders
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path LIKE ? ESCAPE '\\' AND trashed_at IS NULL",
				user.ID, escapedFullFolderPath+"/%").
			Updates(map[string]interface{}{
				"trashed_at":            now,
				"original_logical_path": gorm.Expr("logical_path"),
			}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		flash.Error(w, "Failed to delete folder. Please try again.")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	flash.Success(w, "Folder deleted.")
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

	// Set CORS headers only for validated origins from allowlist.
	// If allowlist is empty, no CORS headers are sent (same-origin only).
	if origin := r.Header.Get("Origin"); origin != "" {
		if h.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		// For untrusted origins, no CORS headers are set - browser will block the request
	}

	w.WriteHeader(http.StatusOK) // Explicitly write the status to ensure headers are sent

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Flush headers immediately so client knows connection is established
	flusher.Flush()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()

	// Track last known state to detect changes
	lastState := make(map[uint]string)

	// Adaptive polling: start with 1s, back off to 5s if no activity detected
	pollInterval := 1 * time.Second
	maxPollInterval := 5 * time.Second
	idleCount := 0
	idleThreshold := 5 // Back off after 5 consecutive idle polls

	ticker := time.NewTicker(pollInterval)
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
			hasChanges := false
			for _, file := range files {
				currentState[file.ID] = file.UploadStatus

				// Check if state changed or is new
				if oldStatus, exists := lastState[file.ID]; !exists || oldStatus != file.UploadStatus {
					hasChanges = true
					// For failed uploads, sanitize error messages to avoid exposing
					// internal details (e.g., S3 connection errors) to the user.
					// Only whitelisted safe messages are shown; others are replaced
					// with a generic message. Detailed errors are logged server-side.
					errorMsg := file.ErrorMessage
					if file.UploadStatus == "failed" && errorMsg != "" {
						safeMessages := []string{
							"Upload queue is full. Please try again later.",
							"Storage quota exceeded",
							"File too large",
							"Invalid file type",
							"File name too long",
						}
						isSafe := false
						for _, safe := range safeMessages {
							if strings.HasPrefix(errorMsg, safe) {
								isSafe = true
								break
							}
						}
						if !isSafe {
							errorMsg = "Upload failed. Check server logs for details."
						}
					}
					event := FileStatusEvent{
						ID:           file.ID,
						UploadStatus: file.UploadStatus,
						ErrorMessage: errorMsg,
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

			// Check for files that were previously tracked but are no longer in our query results.
			// This happens when a file transitions to "completed" after the 5-second window.
			// We need to fetch these files individually to send the completion event.
			for id, oldStatus := range lastState {
				if _, exists := currentState[id]; !exists {
					// File was previously tracked but not in current results
					// Check if it completed (not deleted)
					if oldStatus == "pending" || oldStatus == "uploading" {
						var file models.File
						if err := h.db.Where("id = ? AND user_id = ?", id, user.ID).First(&file).Error; err == nil {
							// File exists, send its current status
							if file.UploadStatus == "completed" {
								hasChanges = true
								event := FileStatusEvent{
									ID:           file.ID,
									UploadStatus: file.UploadStatus,
									ErrorMessage: "",
									Filename:     file.OriginalFilename,
								}
								data, err := json.Marshal(event)
								if err == nil {
									fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
									flusher.Flush()
								}
							}
						}
					}
					delete(lastState, id)
				}
			}

			// Update last known state
			lastState = currentState

			// Adaptive polling: back off when idle, speed up when active
			if len(currentState) == 0 && !hasChanges {
				idleCount++
				if idleCount > idleThreshold && pollInterval != maxPollInterval {
					pollInterval = maxPollInterval
					ticker.Reset(pollInterval)
				}
			} else {
				// Activity detected: reset to fast polling
				if pollInterval != 1*time.Second {
					pollInterval = 1 * time.Second
					ticker.Reset(pollInterval)
				}
				idleCount = 0
			}
		}
	}
}

// RenameFile renames a file, preventing name collisions within the same folder.
func (h *FileHandler) RenameFile(w http.ResponseWriter, r *http.Request) {
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

	newName := strings.TrimSpace(r.FormValue("new_name"))
	if newName == "" {
		flash.Error(w, "New name is required")
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	// Validate filename length (most filesystems limit to 255 bytes)
	if len(newName) > 255 {
		flash.Error(w, "File name is too long (max 255 characters)")
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	// Validate filename (no slashes, no ..)
	if strings.Contains(newName, "/") || strings.Contains(newName, "..") || strings.Contains(newName, "\\") {
		flash.Error(w, "Invalid file name")
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	// Find the file
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		flash.Error(w, "File not found")
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	// Check if the new name is the same as the current name
	if file.Filename == newName {
		flash.Success(w, "File name unchanged")
		http.Redirect(w, r, folderRedirectURL(file.LogicalPath), http.StatusSeeOther)
		return
	}

	// Check for name collision in the same folder
	var count int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ? AND filename = ? AND id != ?", user.ID, file.LogicalPath, newName, file.ID).
		Count(&count)

	if count > 0 {
		flash.Error(w, "A file with that name already exists in this folder")
		http.Redirect(w, r, folderRedirectURL(file.LogicalPath), http.StatusSeeOther)
		return
	}

	// Update the filename
	if err := h.db.Model(&file).Update("filename", newName).Error; err != nil {
		flash.Error(w, "Failed to rename file")
		http.Redirect(w, r, folderRedirectURL(file.LogicalPath), http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("File renamed to %s", newName))
	http.Redirect(w, r, folderRedirectURL(file.LogicalPath), http.StatusSeeOther)
}

// MoveFile moves a file to a different folder.
func (h *FileHandler) MoveFile(w http.ResponseWriter, r *http.Request) {
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

	destinationFolder := sanitizeFolderPath(r.FormValue("destination_folder"))

	// Find the file
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ?", fileID, user.ID).First(&file).Error; err != nil {
		flash.Error(w, "File not found")
		http.Redirect(w, r, "/files", http.StatusSeeOther)
		return
	}

	// Store the original folder for potential redirect
	originalFolder := file.LogicalPath

	// Check if the destination is the same as the current location
	if file.LogicalPath == destinationFolder {
		flash.Success(w, "File is already in this folder")
		http.Redirect(w, r, folderRedirectURL(destinationFolder), http.StatusSeeOther)
		return
	}

	// Validate destination folder exists (root folder "/" is always valid)
	if destinationFolder != "/" {
		// Check if folder exists in folders table
		var folderCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ?", user.ID, destinationFolder).Count(&folderCount)

		// Also check if any files exist in this folder path (implicit folders)
		var fileCount int64
		if folderCount == 0 {
			h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ?", user.ID, destinationFolder).Count(&fileCount)
		}

		// If folder doesn't exist in either table, return error
		if folderCount == 0 && fileCount == 0 {
			flash.Error(w, "Destination folder does not exist")
			http.Redirect(w, r, folderRedirectURL(originalFolder), http.StatusSeeOther)
			return
		}
	}

	// Check for name collision in the destination folder
	var count int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ? AND filename = ? AND id != ?", user.ID, destinationFolder, file.Filename, file.ID).
		Count(&count)

	if count > 0 {
		flash.Error(w, "A file with the same name already exists in the destination folder")
		http.Redirect(w, r, folderRedirectURL(originalFolder), http.StatusSeeOther)
		return
	}

	// Update the file's logical path
	if err := h.db.Model(&file).Update("logical_path", destinationFolder).Error; err != nil {
		flash.Error(w, "Failed to move file")
		http.Redirect(w, r, folderRedirectURL(originalFolder), http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("File moved to %s", destinationFolder))
	http.Redirect(w, r, folderRedirectURL(destinationFolder), http.StatusSeeOther)
}

// RenameFolder renames a folder, preventing name collisions within the same parent folder.
// This also updates the logical_path of all files and subfolders within the folder.
func (h *FileHandler) RenameFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	currentFolder := sanitizeFolderPath(r.FormValue("current_folder"))
	oldName := strings.TrimSpace(r.FormValue("old_name"))
	newName := strings.TrimSpace(r.FormValue("new_name"))

	if oldName == "" || newName == "" {
		flash.Error(w, "Folder name is required")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Validate folder name length (most filesystems limit to 255 bytes)
	if len(newName) > 255 {
		flash.Error(w, "Folder name is too long (max 255 characters)")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Validate folder name (no slashes, no ..)
	if strings.Contains(newName, "/") || strings.Contains(newName, "..") || strings.Contains(newName, "\\") {
		flash.Error(w, "Invalid folder name")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Build full old and new paths
	var oldPath, newPath string
	if currentFolder == "/" {
		oldPath = "/" + oldName
		newPath = "/" + newName
	} else {
		oldPath = currentFolder + "/" + oldName
		newPath = currentFolder + "/" + newName
	}

	// Check if old name equals new name
	if oldPath == newPath {
		flash.Success(w, "Folder name unchanged")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Check if a folder with the new name already exists in the same parent
	var existingFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, newPath).First(&existingFolder).Error; err == nil {
		flash.Error(w, "A folder with that name already exists")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Check for implicit folder collision (folder that exists because files are in it)
	var implicitFolderCount int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ?", user.ID, newPath).
		Count(&implicitFolderCount)
	if implicitFolderCount > 0 {
		flash.Error(w, "A folder with that name already exists")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Verify the source folder exists before attempting rename
	var sourceFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, oldPath).First(&sourceFolder).Error; err != nil {
		flash.Error(w, "Folder not found")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Perform rename within a transaction
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Update the folder record itself
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path = ?", user.ID, oldPath).
			Update("folder_path", newPath).Error; err != nil {
			return err
		}

		// Update all subfolders (replace prefix)
		escapedOldPath := escapeSQLLike(oldPath)
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\'", user.ID, escapedOldPath+"/%").
			Update("folder_path", gorm.Expr("REPLACE(folder_path, ?, ?)", oldPath+"/", newPath+"/")).Error; err != nil {
			return err
		}

		// Update all files in the folder (exact match)
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path = ?", user.ID, oldPath).
			Update("logical_path", newPath).Error; err != nil {
			return err
		}

		// Update all files in subfolders (replace prefix)
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path LIKE ? ESCAPE '\\'", user.ID, escapedOldPath+"/%").
			Update("logical_path", gorm.Expr("REPLACE(logical_path, ?, ?)", oldPath+"/", newPath+"/")).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		flash.Error(w, "Failed to rename folder")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("Folder renamed to %s", newName))
	http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
}

// MoveFolder moves a folder and all its contents to a different destination.
// This updates the folder_path of the folder and all subfolders, as well as
// the logical_path of all files within the folder hierarchy.
func (h *FileHandler) MoveFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	currentFolder := sanitizeFolderPath(r.FormValue("current_folder"))
	folderName := strings.TrimSpace(r.FormValue("folder_name"))
	destinationFolder := sanitizeFolderPath(r.FormValue("destination_folder"))

	if folderName == "" {
		flash.Error(w, "Folder name is required")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Build source folder path
	var sourcePath string
	if currentFolder == "/" {
		sourcePath = "/" + folderName
	} else {
		sourcePath = currentFolder + "/" + folderName
	}

	// Build destination path for the moved folder
	var newPath string
	if destinationFolder == "/" {
		newPath = "/" + folderName
	} else {
		newPath = destinationFolder + "/" + folderName
	}

	// Check if source and destination are the same
	if sourcePath == newPath {
		// Silently redirect - no actual move needed
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Prevent moving a folder into itself or its own subfolder (circular reference)
	// Use proper path boundary check to avoid false positives like /folder vs /folderNew
	if destinationFolder == sourcePath || strings.HasPrefix(destinationFolder+"/", sourcePath+"/") {
		log.Printf("Prevented circular folder move: user_id=%d source=%s destination=%s", user.ID, sourcePath, destinationFolder)
		flash.Error(w, "Cannot move a folder into itself or its subfolder")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Verify the source folder exists
	var sourceFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, sourcePath).First(&sourceFolder).Error; err != nil {
		log.Printf("Folder move failed - source not found: user_id=%d source=%s", user.ID, sourcePath)
		flash.Error(w, "Source folder not found")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Validate destination folder exists (root folder "/" is always valid)
	if destinationFolder != "/" {
		// Check if folder exists in folders table
		var folderCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ?", user.ID, destinationFolder).Count(&folderCount)

		// Also check if any files exist in this folder path (implicit folders)
		var fileCount int64
		if folderCount == 0 {
			h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ?", user.ID, destinationFolder).Count(&fileCount)
		}

		// If folder doesn't exist in either table, return error
		if folderCount == 0 && fileCount == 0 {
			log.Printf("Folder move failed - destination not found: user_id=%d destination=%s", user.ID, destinationFolder)
			flash.Error(w, "Destination folder does not exist")
			http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
			return
		}
	}

	// Check if a folder with the same name already exists in the destination
	var existingFolder models.Folder
	if err := h.db.Where("user_id = ? AND folder_path = ?", user.ID, newPath).First(&existingFolder).Error; err == nil {
		log.Printf("Folder move failed - name collision: user_id=%d source=%s destination=%s", user.ID, sourcePath, newPath)
		flash.Error(w, "A folder with that name already exists in the destination")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Check for implicit folder collision
	var implicitFolderCount int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ?", user.ID, newPath).
		Count(&implicitFolderCount)
	if implicitFolderCount > 0 {
		log.Printf("Folder move failed - implicit folder collision: user_id=%d source=%s destination=%s", user.ID, sourcePath, newPath)
		flash.Error(w, "A folder with that name already exists in the destination")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	// Perform move within a transaction
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Update the folder record itself
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path = ?", user.ID, sourcePath).
			Update("folder_path", newPath).Error; err != nil {
			return err
		}

		// Update all subfolders (replace prefix)
		escapedSourcePath := escapeSQLLike(sourcePath)
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\'", user.ID, escapedSourcePath+"/%").
			Update("folder_path", gorm.Expr("REPLACE(folder_path, ?, ?)", sourcePath+"/", newPath+"/")).Error; err != nil {
			return err
		}

		// Update all files in the folder (exact match)
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path = ?", user.ID, sourcePath).
			Update("logical_path", newPath).Error; err != nil {
			return err
		}

		// Update all files in subfolders (replace prefix)
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path LIKE ? ESCAPE '\\'", user.ID, escapedSourcePath+"/%").
			Update("logical_path", gorm.Expr("REPLACE(logical_path, ?, ?)", sourcePath+"/", newPath+"/")).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		log.Printf("Failed to move folder: user_id=%d source=%s destination=%s error=%v", user.ID, sourcePath, destinationFolder, err)
		flash.Error(w, "Failed to move folder")
		http.Redirect(w, r, folderRedirectURL(currentFolder), http.StatusSeeOther)
		return
	}

	log.Printf("Folder moved successfully: user_id=%d source=%s destination=%s", user.ID, sourcePath, destinationFolder)
	flash.Success(w, fmt.Sprintf("Folder moved to %s", destinationFolder))
	http.Redirect(w, r, folderRedirectURL(destinationFolder), http.StatusSeeOther)
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

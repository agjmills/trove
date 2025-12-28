package handlers

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/flash"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/maruel/natural"
	"gorm.io/gorm"
)

// DeletedHandler handles deleted items operations
type DeletedHandler struct {
	db      *gorm.DB
	cfg     *config.Config
	storage storage.StorageBackend

	// Background cleanup
	stopChan chan struct{}
	wg       sync.WaitGroup
	once     sync.Once
}

// NewDeletedHandler creates a new DeletedHandler and starts the background cleanup job
func NewDeletedHandler(db *gorm.DB, cfg *config.Config, storage storage.StorageBackend) *DeletedHandler {
	h := &DeletedHandler{
		db:       db,
		cfg:      cfg,
		storage:  storage,
		stopChan: make(chan struct{}),
	}

	// Start background cleanup job
	h.wg.Add(1)
	go h.cleanupWorker()

	return h
}

// Shutdown stops the background cleanup worker
func (h *DeletedHandler) Shutdown() {
	h.once.Do(func() {
		close(h.stopChan)
	})
	h.wg.Wait()
}

// cleanupWorker runs the deleted items cleanup job periodically
func (h *DeletedHandler) cleanupWorker() {
	defer h.wg.Done()

	// Skip immediate cleanup in test environment to avoid race conditions
	if h.cfg.Env != "test" {
		h.runCleanup()
	}

	interval := time.Duration(h.cfg.DeletedCleanupIntervalMin) * time.Minute
	if interval < time.Minute {
		interval = time.Minute // Minimum 1 minute interval
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopChan:
			logger.Info("Deleted items cleanup worker stopping")
			return
		case <-ticker.C:
			h.runCleanup()
		}
	}
}

// runCleanup permanently deletes expired deleted items
func (h *DeletedHandler) runCleanup() {
	ctx := context.Background()
	logger.Debug("Running deleted items cleanup")

	totalFiles := 0
	totalFolders := 0
	totalBytes := int64(0)

	// Process users in batches to avoid loading all users into memory
	const batchSize = 100
	var lastID uint = 0

	for {
		var users []models.User
		if err := h.db.Where("id > ?", lastID).Order("id").Limit(batchSize).Find(&users).Error; err != nil {
			logger.Error("Deleted items cleanup: failed to fetch users", "error", err)
			return
		}
		if len(users) == 0 {
			break
		}

		for _, user := range users {
			// Determine retention days for this user
			retentionDays := h.cfg.DeletedRetentionDays
			if user.DeletedRetentionDays != nil {
				retentionDays = *user.DeletedRetentionDays
			}

			// Skip if retention is 0 (means keep forever, which shouldn't happen with deleted items)
			// or negative (disabled)
			if retentionDays <= 0 {
				continue
			}

			cutoffTime := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)

			// Find expired deleted files for this user
			var expiredFiles []models.File
			if err := h.db.Where("user_id = ? AND trashed_at IS NOT NULL AND trashed_at < ?", user.ID, cutoffTime).
				Find(&expiredFiles).Error; err != nil {
				logger.Error("Deleted items cleanup: failed to find expired files", "user_id", user.ID, "error", err)
				continue
			}

			// Permanently delete expired files
			for _, file := range expiredFiles {
				if err := h.permanentlyDeleteFile(ctx, &file); err != nil {
					logger.Error("Deleted items cleanup: failed to delete file", "file_id", file.ID, "error", err)
					continue
				}
				totalFiles++
				totalBytes += file.FileSize
			}

			// Find expired deleted folders for this user
			var expiredFolders []models.Folder
			if err := h.db.Where("user_id = ? AND trashed_at IS NOT NULL AND trashed_at < ?", user.ID, cutoffTime).
				Find(&expiredFolders).Error; err != nil {
				logger.Error("Deleted items cleanup: failed to find expired folders", "user_id", user.ID, "error", err)
				continue
			}

			// Permanently delete expired folders
			for _, folder := range expiredFolders {
				if err := h.db.Delete(&folder).Error; err != nil {
					logger.Error("Deleted items cleanup: failed to delete folder", "folder_id", folder.ID, "error", err)
					continue
				}
				totalFolders++
			}
		}

		lastID = users[len(users)-1].ID
	}

	if totalFiles > 0 || totalFolders > 0 {
		logger.Info("Deleted items cleanup complete", "files", totalFiles, "bytes", totalBytes, "folders", totalFolders)
	}
}

// permanentlyDeleteFile removes a file from storage and database
func (h *DeletedHandler) permanentlyDeleteFile(ctx context.Context, file *models.File) error {
	storagePath := file.StoragePath
	fileSize := file.FileSize
	userID := file.UserID

	// Delete from database first
	if err := h.db.Unscoped().Delete(file).Error; err != nil {
		return fmt.Errorf("failed to delete file record: %w", err)
	}

	// Check if any other File records reference this physical file (deduplication check)
	var refCount int64
	h.db.Model(&models.File{}).Where("user_id = ? AND storage_path = ?", userID, storagePath).Count(&refCount)

	// Only delete from storage if no other references exist
	if refCount == 0 {
		if err := h.storage.Delete(ctx, storagePath); err != nil {
			logger.Warn("Failed to delete file from storage", "path", storagePath, "error", err)
		}

		// Update user storage quota
		if err := h.db.Model(&models.User{}).Where("id = ?", userID).
			UpdateColumn("storage_used", gorm.Expr("CASE WHEN storage_used >= ? THEN storage_used - ? ELSE 0 END", fileSize, fileSize)).Error; err != nil {
			logger.Warn("Failed to update user storage", "user_id", userID, "error", err)
		}
	}

	return nil
}

// ShowDeleted displays the user's deleted items
func (h *DeletedHandler) ShowDeleted(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Pagination parameters
	page := 1
	pageSize := 15
	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	// Get deleted files
	var allFiles []models.File
	if err := h.db.Where("user_id = ? AND trashed_at IS NOT NULL", user.ID).Find(&allFiles).Error; err != nil {
		logger.Error("Failed to fetch deleted files", "user_id", user.ID, "error", err)
		http.Error(w, "Failed to load deleted items", http.StatusInternalServerError)
		return
	}

	// Get deleted folders
	var allFolders []models.Folder
	if err := h.db.Where("user_id = ? AND trashed_at IS NOT NULL", user.ID).Find(&allFolders).Error; err != nil {
		logger.Error("Failed to fetch deleted folders", "user_id", user.ID, "error", err)
		http.Error(w, "Failed to load deleted items", http.StatusInternalServerError)
		return
	}

	// Sort files naturally by filename
	sort.Slice(allFiles, func(i, j int) bool {
		return natural.Less(strings.ToLower(allFiles[i].Filename), strings.ToLower(allFiles[j].Filename))
	})

	// Sort folders naturally
	sort.Slice(allFolders, func(i, j int) bool {
		return natural.Less(strings.ToLower(allFolders[i].FolderPath), strings.ToLower(allFolders[j].FolderPath))
	})

	// Pagination for files
	totalFiles := len(allFiles)
	offset := (page - 1) * pageSize
	var files []models.File
	if offset < totalFiles {
		end := offset + pageSize
		if end > totalFiles {
			end = totalFiles
		}
		files = allFiles[offset:end]
	}

	totalPages := (totalFiles + pageSize - 1) / pageSize
	if totalPages == 0 {
		totalPages = 1
	}

	// Calculate total deleted size
	var totalDeletedSize int64
	for _, f := range allFiles {
		totalDeletedSize += f.FileSize
	}

	// Determine user's retention days
	retentionDays := h.cfg.DeletedRetentionDays
	if user.DeletedRetentionDays != nil {
		retentionDays = *user.DeletedRetentionDays
	}

	// Get flash message
	flashMsg := flash.Get(w, r)

	// FolderInfo for display
	type FolderInfo struct {
		ID            uint
		Name          string
		OriginalPath  string
		SoftDeletedAt *time.Time
		ExpiresIn     string
	}
	folderInfos := make([]FolderInfo, 0, len(allFolders))
	for _, f := range allFolders {
		expiresIn := ""
		if f.SoftDeletedAt != nil && retentionDays > 0 {
			expiresAt := f.SoftDeletedAt.Add(time.Duration(retentionDays) * 24 * time.Hour)
			remaining := time.Until(expiresAt)
			if remaining > 24*time.Hour {
				expiresIn = fmt.Sprintf("%d days", int(remaining.Hours()/24))
			} else if remaining > time.Hour {
				expiresIn = fmt.Sprintf("%d hours", int(remaining.Hours()))
			} else if remaining > 0 {
				expiresIn = "< 1 hour"
			} else {
				expiresIn = "expired"
			}
		}
		folderInfos = append(folderInfos, FolderInfo{
			ID:            f.ID,
			Name:          extractFolderName(f.OriginalFolderPath),
			OriginalPath:  f.OriginalFolderPath,
			SoftDeletedAt: f.SoftDeletedAt,
			ExpiresIn:     expiresIn,
		})
	}

	// FileInfo for display with expiry
	type FileInfo struct {
		models.File
		ExpiresIn string
	}
	fileInfos := make([]FileInfo, 0, len(files))
	for _, f := range files {
		expiresIn := ""
		if f.SoftDeletedAt != nil && retentionDays > 0 {
			expiresAt := f.SoftDeletedAt.Add(time.Duration(retentionDays) * 24 * time.Hour)
			remaining := time.Until(expiresAt)
			if remaining > 24*time.Hour {
				expiresIn = fmt.Sprintf("%d days", int(remaining.Hours()/24))
			} else if remaining > time.Hour {
				expiresIn = fmt.Sprintf("%d hours", int(remaining.Hours()))
			} else if remaining > 0 {
				expiresIn = "< 1 hour"
			} else {
				expiresIn = "expired"
			}
		}
		fileInfos = append(fileInfos, FileInfo{
			File:      f,
			ExpiresIn: expiresIn,
		})
	}

	render(w, "deleted.html", map[string]any{
		"Title":            "Deleted Items",
		"User":             user,
		"Files":            fileInfos,
		"Folders":          folderInfos,
		"Flash":            flashMsg,
		"CSRFToken":        csrf.Token(r),
		"Page":             page,
		"TotalPages":       totalPages,
		"TotalFiles":       totalFiles,
		"TotalFolders":     len(allFolders),
		"TotalDeletedSize": totalDeletedSize,
		"RetentionDays":    retentionDays,
		"FullWidth":        true,
	})
}

// extractFolderName gets the last component of a folder path
func extractFolderName(path string) string {
	path = strings.TrimSuffix(path, "/")
	if path == "" || path == "/" {
		return "/"
	}
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

// RestoreFile restores a file from deleted items
func (h *DeletedHandler) RestoreFile(w http.ResponseWriter, r *http.Request) {
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

	// Find the deleted file
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NOT NULL", fileID, user.ID).First(&file).Error; err != nil {
		flash.Error(w, "File not found in deleted items")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Restore the file to its original location
	originalPath := file.OriginalLogicalPath
	if originalPath == "" {
		originalPath = "/"
	}

	// Check if original folder still exists, if not restore to root
	if originalPath != "/" {
		var folderCount int64
		h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ? AND trashed_at IS NULL", user.ID, originalPath).Count(&folderCount)
		if folderCount == 0 {
			// Check for implicit folder
			var fileCount int64
			h.db.Model(&models.File{}).Where("user_id = ? AND logical_path = ? AND trashed_at IS NULL", user.ID, originalPath).Count(&fileCount)
			if fileCount == 0 {
				originalPath = "/"
			}
		}
	}

	// Check for filename collision
	var count int64
	h.db.Model(&models.File{}).
		Where("user_id = ? AND logical_path = ? AND filename = ? AND trashed_at IS NULL AND id != ?",
			user.ID, originalPath, file.Filename, file.ID).
		Count(&count)

	if count > 0 {
		flash.Error(w, "A file with the same name already exists in the destination folder")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Restore the file
	if err := h.db.Model(&file).Updates(map[string]interface{}{
		"logical_path":          originalPath,
		"trashed_at":            nil,
		"original_logical_path": "",
	}).Error; err != nil {
		flash.Error(w, "Failed to restore file")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("File \"%s\" restored to %s", file.Filename, originalPath))
	http.Redirect(w, r, "/deleted", http.StatusSeeOther)
}

// RestoreFolder restores a folder from deleted items
func (h *DeletedHandler) RestoreFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	folderID := chi.URLParam(r, "id")
	if folderID == "" {
		http.Error(w, "Folder ID is required", http.StatusBadRequest)
		return
	}

	// Find the deleted folder
	var folder models.Folder
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NOT NULL", folderID, user.ID).First(&folder).Error; err != nil {
		flash.Error(w, "Folder not found in deleted items")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	originalPath := folder.OriginalFolderPath
	if originalPath == "" {
		flash.Error(w, "Cannot restore folder: original path unknown")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Check if a folder with the same path already exists
	var existingCount int64
	h.db.Model(&models.Folder{}).Where("user_id = ? AND folder_path = ? AND trashed_at IS NULL", user.ID, originalPath).Count(&existingCount)
	if existingCount > 0 {
		flash.Error(w, "A folder with the same name already exists at the original location")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Restore the folder and all its contents
	err := h.db.Transaction(func(tx *gorm.DB) error {
		// Restore the folder
		if err := tx.Model(&folder).Updates(map[string]interface{}{
			"folder_path":          originalPath,
			"trashed_at":           nil,
			"original_folder_path": "",
		}).Error; err != nil {
			return err
		}

		// Restore all files that were in this folder (using the deleted items path pattern)
		trashFolderPath := folder.FolderPath
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path = ? AND trashed_at IS NOT NULL", user.ID, trashFolderPath).
			Updates(map[string]interface{}{
				"logical_path":          originalPath,
				"trashed_at":            nil,
				"original_logical_path": "",
			}).Error; err != nil {
			return err
		}

		// Restore all subfolders
		escapedTrashPath := escapeSQLLike(trashFolderPath)
		if err := tx.Model(&models.Folder{}).
			Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\' AND trashed_at IS NOT NULL", user.ID, escapedTrashPath+"/%").
			Updates(map[string]interface{}{
				"folder_path":          gorm.Expr("REPLACE(folder_path, ?, ?)", trashFolderPath, originalPath),
				"trashed_at":           nil,
				"original_folder_path": "",
			}).Error; err != nil {
			return err
		}

		// Restore all files in subfolders
		if err := tx.Model(&models.File{}).
			Where("user_id = ? AND logical_path LIKE ? ESCAPE '\\' AND trashed_at IS NOT NULL", user.ID, escapedTrashPath+"/%").
			Updates(map[string]interface{}{
				"logical_path":          gorm.Expr("REPLACE(logical_path, ?, ?)", trashFolderPath, originalPath),
				"trashed_at":            nil,
				"original_logical_path": "",
			}).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		flash.Error(w, "Failed to restore folder")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("Folder \"%s\" restored", extractFolderName(originalPath)))
	http.Redirect(w, r, "/deleted", http.StatusSeeOther)
}

// PermanentlyDeleteFile permanently deletes a file from deleted items
func (h *DeletedHandler) PermanentlyDeleteFile(w http.ResponseWriter, r *http.Request) {
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

	// Find the deleted file
	var file models.File
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NOT NULL", fileID, user.ID).First(&file).Error; err != nil {
		flash.Error(w, "File not found in deleted items")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	if err := h.permanentlyDeleteFile(ctx, &file); err != nil {
		flash.Error(w, "Failed to permanently delete file")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	flash.Success(w, fmt.Sprintf("File \"%s\" permanently deleted", file.Filename))
	http.Redirect(w, r, "/deleted", http.StatusSeeOther)
}

// PermanentlyDeleteFolder permanently deletes a folder and all its contents from deleted items
func (h *DeletedHandler) PermanentlyDeleteFolder(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	folderID := chi.URLParam(r, "id")
	if folderID == "" {
		http.Error(w, "Folder ID is required", http.StatusBadRequest)
		return
	}

	// Find the deleted folder
	var folder models.Folder
	if err := h.db.Where("id = ? AND user_id = ? AND trashed_at IS NOT NULL", folderID, user.ID).First(&folder).Error; err != nil {
		flash.Error(w, "Folder not found in deleted items")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	ctx := r.Context()
	folderPath := folder.FolderPath

	// Find and delete all files in this folder and subfolders
	var files []models.File
	escapedFolderPath := escapeSQLLike(folderPath)
	if err := h.db.Where("user_id = ? AND (logical_path = ? OR logical_path LIKE ? ESCAPE '\\') AND trashed_at IS NOT NULL",
		user.ID, folderPath, escapedFolderPath+"/%").Find(&files).Error; err != nil {
		logger.Error("Failed to fetch files in folder", "folder_id", folderID, "error", err)
		flash.Error(w, "Failed to permanently delete folder")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	for _, file := range files {
		if err := h.permanentlyDeleteFile(ctx, &file); err != nil {
			logger.Error("Failed to delete file", "file_id", file.ID, "error", err)
		}
	}

	// Delete all subfolders
	if err := h.db.Unscoped().Where("user_id = ? AND folder_path LIKE ? ESCAPE '\\' AND trashed_at IS NOT NULL",
		user.ID, escapedFolderPath+"/%").Delete(&models.Folder{}).Error; err != nil {
		logger.Error("Failed to delete subfolders", "folder_id", folderID, "error", err)
		flash.Error(w, "Failed to permanently delete folder")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Delete the folder itself
	if err := h.db.Unscoped().Delete(&folder).Error; err != nil {
		logger.Error("Failed to delete folder", "folder_id", folderID, "error", err)
		flash.Error(w, "Failed to permanently delete folder")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	// Use original path if available, otherwise fall back to a generic message
	folderName := extractFolderName(folder.OriginalFolderPath)
	if folderName == "" || folderName == "/" {
		flash.Success(w, "Folder permanently deleted")
	} else {
		flash.Success(w, fmt.Sprintf("Folder \"%s\" permanently deleted", folderName))
	}
	http.Redirect(w, r, "/deleted", http.StatusSeeOther)
}

// EmptyDeleted permanently deletes all items in the user's deleted items
func (h *DeletedHandler) EmptyDeleted(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	ctx := r.Context()

	// Find all deleted files
	var files []models.File
	if err := h.db.Where("user_id = ? AND trashed_at IS NOT NULL", user.ID).Find(&files).Error; err != nil {
		flash.Error(w, "Failed to empty deleted items")
		http.Redirect(w, r, "/deleted", http.StatusSeeOther)
		return
	}

	deletedCount := 0
	for _, file := range files {
		if err := h.permanentlyDeleteFile(ctx, &file); err != nil {
			logger.Error("Failed to delete file", "file_id", file.ID, "error", err)
			continue
		}
		deletedCount++
	}

	// Delete all deleted folders
	if err := h.db.Unscoped().Where("user_id = ? AND trashed_at IS NOT NULL", user.ID).Delete(&models.Folder{}).Error; err != nil {
		logger.Error("Failed to delete folders", "user_id", user.ID, "error", err)
	}

	flash.Success(w, fmt.Sprintf("All deleted items permanently removed: %d files", deletedCount))
	http.Redirect(w, r, "/deleted", http.StatusSeeOther)
}

// AdminEmptyAllDeleted permanently deletes all items in all users' deleted items (admin only)
func (h *DeletedHandler) AdminEmptyAllDeleted(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if !user.IsAdmin {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	ctx := r.Context()

	// Find all deleted files for all users
	var files []models.File
	h.db.Where("trashed_at IS NOT NULL").Find(&files)

	deletedCount := 0
	for _, file := range files {
		if err := h.permanentlyDeleteFile(ctx, &file); err != nil {
			logger.Error("Failed to delete file", "file_id", file.ID, "error", err)
			continue
		}
		deletedCount++
	}

	// Delete all deleted folders for all users
	h.db.Unscoped().Where("trashed_at IS NOT NULL").Delete(&models.Folder{})

	flash.Success(w, fmt.Sprintf("All deleted items permanently removed: %d files", deletedCount))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

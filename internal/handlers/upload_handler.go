package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type UploadHandler struct {
	db      *gorm.DB
	cfg     *config.Config
	storage storage.StorageBackend
}

func NewUploadHandler(db *gorm.DB, cfg *config.Config, storage storage.StorageBackend) *UploadHandler {
	return &UploadHandler{
		db:      db,
		cfg:     cfg,
		storage: storage,
	}
}

// InitUploadRequest represents the request to initialize a chunked upload
type InitUploadRequest struct {
	Filename    string `json:"filename"`
	TotalSize   int64  `json:"total_size"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
	LogicalPath string `json:"logical_path"`
	MimeType    string `json:"mime_type"`
	Hash        string `json:"hash,omitempty"` // Optional client-side hash for verification
}

// InitUploadResponse represents the response after initializing an upload
type InitUploadResponse struct {
	UploadID       string `json:"upload_id"`
	ChunksReceived []int  `json:"chunks_received"` // List of chunks already received (for resume)
}

// ChunkStatusResponse represents the status of a specific upload session
type ChunkStatusResponse struct {
	UploadID       string `json:"upload_id"`
	Status         string `json:"status"`
	ReceivedChunks int    `json:"received_chunks"`
	TotalChunks    int    `json:"total_chunks"`
	ChunksReceived []int  `json:"chunks_received"`
}

// normalizeLogicalPath validates and normalizes a logical path to prevent
// directory traversal attacks. It returns an empty string if the path is invalid.
func normalizeLogicalPath(path string) string {
	// Default to root if empty
	if path == "" {
		return "/"
	}

	// Replace backslashes with forward slashes for consistency
	path = strings.ReplaceAll(path, "\\", "/")

	// Use filepath.Clean to normalize the path (resolves .., ., multiple slashes)
	// We work with the path without the leading slash to properly detect traversal
	cleanPath := filepath.Clean(path)

	// Convert back to forward slashes (filepath.Clean uses OS separator)
	cleanPath = filepath.ToSlash(cleanPath)

	// Reject paths that try to escape root (start with .. or resolve to ..)
	if cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
		return ""
	}

	// Ensure path starts with /
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}

	// Reject paths containing null bytes
	if strings.ContainsRune(cleanPath, 0) {
		return ""
	}

	return cleanPath
}

// InitUpload initializes a new chunked upload session
func (h *UploadHandler) InitUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	var req InitUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Error("failed to decode init upload request", "error", err)
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.Filename == "" || req.TotalSize <= 0 || req.ChunkSize <= 0 || req.TotalChunks <= 0 {
		http.Error(w, "Invalid upload parameters", http.StatusBadRequest)
		return
	}

	// Validate and normalize LogicalPath to prevent directory traversal
	req.LogicalPath = normalizeLogicalPath(req.LogicalPath)
	if req.LogicalPath == "" {
		http.Error(w, "Invalid logical path", http.StatusBadRequest)
		return
	}

	// Check user quota
	if user.StorageUsed+req.TotalSize > user.StorageQuota {
		http.Error(w, "Storage quota exceeded", http.StatusForbidden)
		return
	}

	// Create temporary directory for chunks
	uploadID := uuid.New().String()
	tempDir := filepath.Join(os.TempDir(), "trove-uploads", uploadID)
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		logger.Error("failed to create temp directory", "error", err, "dir", tempDir)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create upload session
	session := models.UploadSession{
		ID:             uploadID,
		UserID:         userID,
		Filename:       req.Filename,
		LogicalPath:    req.LogicalPath,
		TotalSize:      req.TotalSize,
		TotalChunks:    req.TotalChunks,
		ChunkSize:      req.ChunkSize,
		ReceivedChunks: 0,
		ChunksReceived: []byte("[]"), // Empty JSON array
		Status:         "active",
		Hash:           req.Hash,
		MimeType:       req.MimeType,
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(h.cfg.UploadSessionTimeout),
	}

	if err := h.db.Create(&session).Error; err != nil {
		logger.Error("failed to create upload session", "error", err)
		os.RemoveAll(tempDir) // Clean up temp dir
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	logger.Info("upload session initialized",
		"upload_id", uploadID,
		"user_id", userID,
		"filename", req.Filename,
		"size", req.TotalSize,
		"chunks", req.TotalChunks,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InitUploadResponse{
		UploadID:       uploadID,
		ChunksReceived: []int{},
	})
}

// UploadChunk handles uploading a single chunk
func (h *UploadHandler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	uploadID := chi.URLParam(r, "id")
	if uploadID == "" {
		http.Error(w, "Missing upload ID", http.StatusBadRequest)
		return
	}

	chunkNumStr := r.URL.Query().Get("chunk")
	if chunkNumStr == "" {
		http.Error(w, "Missing chunk number", http.StatusBadRequest)
		return
	}

	chunkNum, err := strconv.Atoi(chunkNumStr)
	if err != nil || chunkNum < 0 {
		http.Error(w, "Invalid chunk number", http.StatusBadRequest)
		return
	}

	// Get upload session
	var session models.UploadSession
	if err := h.db.Where("id = ? AND user_id = ?", uploadID, userID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "Upload session not found", http.StatusNotFound)
		} else {
			logger.Error("failed to get upload session", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Check session status
	if session.Status != "active" {
		http.Error(w, fmt.Sprintf("Upload session is %s", session.Status), http.StatusBadRequest)
		return
	}

	// Check if session has expired
	if time.Now().After(session.ExpiresAt) {
		if err := h.db.Model(&session).Update("status", "expired").Error; err != nil {
			logger.Error("failed to mark session as expired", "error", err, "upload_id", uploadID)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		http.Error(w, "Upload session has expired", http.StatusGone)
		return
	}

	// Validate chunk number
	if chunkNum >= session.TotalChunks {
		http.Error(w, "Invalid chunk number", http.StatusBadRequest)
		return
	}

	// Save chunk to temp file first (before acquiring lock)
	chunkPath := filepath.Join(session.TempDir, fmt.Sprintf("chunk_%d", chunkNum))
	chunkFile, err := os.Create(chunkPath)
	if err != nil {
		logger.Error("failed to create chunk file", "error", err, "path", chunkPath)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer chunkFile.Close()

	written, err := io.Copy(chunkFile, r.Body)
	if err != nil {
		logger.Error("failed to write chunk", "error", err)
		os.Remove(chunkPath)
		http.Error(w, "Failed to save chunk", http.StatusInternalServerError)
		return
	}

	logger.Debug("chunk saved",
		"upload_id", uploadID,
		"chunk", chunkNum,
		"size", written,
	)

	// Use a database transaction with SELECT ... FOR UPDATE to prevent lost updates
	// This locks the row during the read-modify-write sequence
	var chunksReceived []int
	err = h.db.Transaction(func(tx *gorm.DB) error {
		// Re-fetch session with row lock to get current state
		var lockedSession models.UploadSession
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", uploadID).First(&lockedSession).Error; err != nil {
			return err
		}

		// Parse chunks received from locked row
		if err := json.Unmarshal(lockedSession.ChunksReceived, &chunksReceived); err != nil {
			logger.Error("failed to parse chunks received", "error", err)
			chunksReceived = []int{}
		}

		// Check if chunk already received (idempotent)
		for _, num := range chunksReceived {
			if num == chunkNum {
				// Chunk already received, nothing to update
				return nil
			}
		}

		// Append new chunk and update
		chunksReceived = append(chunksReceived, chunkNum)
		chunksReceivedJSON, _ := json.Marshal(chunksReceived)

		updates := map[string]interface{}{
			"received_chunks": len(chunksReceived),
			"chunks_received": chunksReceivedJSON,
			"updated_at":      time.Now(),
		}

		return tx.Model(&lockedSession).Updates(updates).Error
	})

	if err != nil {
		logger.Error("failed to update upload session", "error", err)
		os.Remove(chunkPath)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"chunk":           chunkNum,
		"received_chunks": len(chunksReceived),
		"total_chunks":    session.TotalChunks,
	})
}

// CompleteUpload finalizes the upload by assembling chunks and storing the file
func (h *UploadHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	uploadID := chi.URLParam(r, "id")
	if uploadID == "" {
		http.Error(w, "Missing upload ID", http.StatusBadRequest)
		return
	}

	// Get upload session
	var session models.UploadSession
	if err := h.db.Where("id = ? AND user_id = ?", uploadID, userID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "Upload session not found", http.StatusNotFound)
		} else {
			logger.Error("failed to get upload session", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Check if all chunks received
	if session.ReceivedChunks != session.TotalChunks {
		http.Error(w, fmt.Sprintf("Missing chunks: %d/%d received", session.ReceivedChunks, session.TotalChunks), http.StatusBadRequest)
		return
	}

	// Parse chunks received
	var chunksReceived []int
	if err := json.Unmarshal(session.ChunksReceived, &chunksReceived); err != nil {
		logger.Error("failed to parse chunks received", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Sort chunks to ensure correct order
	sort.Ints(chunksReceived)

	// Verify chunk contiguity: ensure we have exactly chunks 0 through N-1
	// This catches duplicate chunks or gaps that could slip past the count check
	if len(chunksReceived) != session.TotalChunks {
		http.Error(w, fmt.Sprintf("Chunk count mismatch: expected %d, got %d unique chunks", session.TotalChunks, len(chunksReceived)), http.StatusBadRequest)
		return
	}
	for i, chunkNum := range chunksReceived {
		if chunkNum != i {
			http.Error(w, fmt.Sprintf("Missing chunk %d", i), http.StatusBadRequest)
			return
		}
	}

	// Create final file by assembling chunks
	finalPath := filepath.Join(session.TempDir, "complete")
	finalFile, err := os.Create(finalPath)
	if err != nil {
		logger.Error("failed to create final file", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	defer finalFile.Close()

	// Calculate hash while assembling
	hasher := sha256.New()
	multiWriter := io.MultiWriter(finalFile, hasher)

	// Assemble chunks in order
	for _, chunkNum := range chunksReceived {
		chunkPath := filepath.Join(session.TempDir, fmt.Sprintf("chunk_%d", chunkNum))
		chunkFile, err := os.Open(chunkPath)
		if err != nil {
			logger.Error("failed to open chunk file", "error", err, "chunk", chunkNum)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}

		if _, err := io.Copy(multiWriter, chunkFile); err != nil {
			chunkFile.Close()
			logger.Error("failed to copy chunk", "error", err, "chunk", chunkNum)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		chunkFile.Close()
	}

	// Verify hash if provided
	calculatedHash := hex.EncodeToString(hasher.Sum(nil))
	if session.Hash != "" && session.Hash != calculatedHash {
		logger.Error("hash mismatch",
			"expected", session.Hash,
			"calculated", calculatedHash,
		)
		http.Error(w, "File integrity check failed", http.StatusBadRequest)
		return
	}

	// Get file size
	fileInfo, err := finalFile.Stat()
	if err != nil {
		logger.Error("failed to stat final file", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if fileInfo.Size() != session.TotalSize {
		logger.Error("file size mismatch",
			"expected", session.TotalSize,
			"actual", fileInfo.Size(),
		)
		http.Error(w, "File size mismatch", http.StatusBadRequest)
		return
	}

	// Upload to storage backend first to get the generated path
	// Reset file pointer before saving to storage
	if _, err := finalFile.Seek(0, 0); err != nil {
		logger.Error("failed to seek to beginning of file",
			"error", err,
			"upload_id", uploadID,
			"filename", session.Filename,
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	saveResult, err := h.storage.Save(r.Context(), finalFile, storage.SaveOptions{
		OriginalFilename: session.Filename,
		ContentType:      session.MimeType,
	})
	if err != nil {
		logger.Error("failed to upload to storage", "error", err)
		http.Error(w, "Failed to upload file", http.StatusInternalServerError)
		return
	}

	// Create file record with storage-generated path
	// Create directly with "completed" status to avoid inconsistency window
	file := models.File{
		UserID:           userID,
		StoragePath:      saveResult.Path,
		LogicalPath:      session.LogicalPath,
		Filename:         session.Filename,
		OriginalFilename: session.Filename,
		FileSize:         saveResult.Size,
		MimeType:         session.MimeType,
		Hash:             calculatedHash,
		UploadStatus:     "completed",
	}

	if err := h.db.Create(&file).Error; err != nil {
		logger.Error("failed to create file record", "error", err)
		// Clean up uploaded file
		h.storage.Delete(r.Context(), saveResult.Path)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update user storage usage
	if err := h.db.Model(&models.User{}).Where("id = ?", userID).
		UpdateColumn("storage_used", gorm.Expr("storage_used + ?", session.TotalSize)).Error; err != nil {
		logger.Error("failed to update user storage usage", "error", err, "user_id", userID, "size", session.TotalSize)
	}

	// Mark session as completed
	h.db.Model(&session).Update("status", "completed")

	// Clean up temp directory
	go func() {
		if err := os.RemoveAll(session.TempDir); err != nil {
			logger.Error("failed to clean up temp directory", "error", err, "dir", session.TempDir)
		}
	}()

	logger.Info("upload completed",
		"upload_id", uploadID,
		"file_id", file.ID,
		"filename", session.Filename,
		"size", session.TotalSize,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"file_id":  file.ID,
		"filename": file.Filename,
		"size":     file.FileSize,
		"hash":     file.Hash,
	})
}

// CancelUpload cancels an upload session and cleans up chunks
func (h *UploadHandler) CancelUpload(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	uploadID := chi.URLParam(r, "id")
	if uploadID == "" {
		http.Error(w, "Missing upload ID", http.StatusBadRequest)
		return
	}

	// Get upload session
	var session models.UploadSession
	if err := h.db.Where("id = ? AND user_id = ?", uploadID, userID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "Upload session not found", http.StatusNotFound)
		} else {
			logger.Error("failed to get upload session", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Mark as cancelled
	if err := h.db.Model(&session).Update("status", "cancelled").Error; err != nil {
		logger.Error("failed to update session status", "error", err)
	}

	// Clean up temp directory
	go func() {
		if err := os.RemoveAll(session.TempDir); err != nil {
			logger.Error("failed to clean up temp directory", "error", err, "dir", session.TempDir)
		}
	}()

	logger.Info("upload cancelled", "upload_id", uploadID, "user_id", userID)

	w.WriteHeader(http.StatusNoContent)
}

// GetUploadStatus returns the status of an upload session
func (h *UploadHandler) GetUploadStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	userID := user.ID

	uploadID := chi.URLParam(r, "id")
	if uploadID == "" {
		http.Error(w, "Missing upload ID", http.StatusBadRequest)
		return
	}

	// Get upload session
	var session models.UploadSession
	if err := h.db.Where("id = ? AND user_id = ?", uploadID, userID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			http.Error(w, "Upload session not found", http.StatusNotFound)
		} else {
			logger.Error("failed to get upload session", "error", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
		return
	}

	// Parse chunks received
	var chunksReceived []int
	if err := json.Unmarshal(session.ChunksReceived, &chunksReceived); err != nil {
		logger.Error("failed to parse chunks received", "error", err)
		chunksReceived = []int{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ChunkStatusResponse{
		UploadID:       session.ID,
		Status:         session.Status,
		ReceivedChunks: session.ReceivedChunks,
		TotalChunks:    session.TotalChunks,
		ChunksReceived: chunksReceived,
	})
}

// CleanupExpiredSessions removes expired upload sessions and their temp files
func (h *UploadHandler) CleanupExpiredSessions() error {
	var expiredSessions []models.UploadSession

	// Find sessions that are expired or have been inactive for too long
	err := h.db.Where("status = ? AND expires_at < ?", "active", time.Now()).
		Find(&expiredSessions).Error
	if err != nil {
		return fmt.Errorf("failed to query expired sessions: %w", err)
	}

	for _, session := range expiredSessions {
		// Mark as expired
		h.db.Model(&session).Update("status", "expired")

		// Clean up temp directory
		if session.TempDir != "" {
			if err := os.RemoveAll(session.TempDir); err != nil {
				logger.Error("failed to clean up temp directory",
					"error", err,
					"dir", session.TempDir,
					"upload_id", session.ID,
				)
			}
		}

		logger.Info("cleaned up expired upload session",
			"upload_id", session.ID,
			"user_id", session.UserID,
			"filename", session.Filename,
		)
	}

	// Delete old completed/cancelled/expired sessions (older than configured retention period)
	retentionDays := h.cfg.UploadSessionRetentionDays
	if retentionDays <= 0 {
		retentionDays = 7 // Default to 7 days if not configured
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result := h.db.Where("status IN ? AND updated_at < ?", []string{"completed", "cancelled", "expired"}, cutoff).
		Delete(&models.UploadSession{})

	if result.Error != nil {
		return fmt.Errorf("failed to delete old sessions: %w", result.Error)
	}

	if result.RowsAffected > 0 {
		logger.Info("deleted old upload sessions", "count", result.RowsAffected)
	}

	return nil
}

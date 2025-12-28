package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUploadHandlerTest(t *testing.T) (*UploadHandler, *gorm.DB, *models.User) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	if err := db.AutoMigrate(&models.User{}, &models.File{}, &models.UploadSession{}); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	user := &models.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hash",
		StorageQuota: 100 * 1024 * 1024, // 100MB
		StorageUsed:  0,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	cfg := &config.Config{
		UploadChunkSize:            5 * 1024 * 1024, // 5MB
		UploadSessionTimeout:       24 * time.Hour,
		UploadSessionRetentionDays: 7,
	}

	memStorage := storage.NewMemoryBackend()
	handler := NewUploadHandler(db, cfg, memStorage)

	return handler, db, user
}

func TestInitUpload(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	reqBody := InitUploadRequest{
		Filename:    "test.txt",
		TotalSize:   10 * 1024 * 1024, // 10MB
		ChunkSize:   5 * 1024 * 1024,  // 5MB
		TotalChunks: 2,
		LogicalPath: "/",
		MimeType:    "text/plain",
	}

	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest(http.MethodPost, "/api/uploads/init", bytes.NewReader(body))
	req = withUser(req, user)

	w := httptest.NewRecorder()
	handler.InitUpload(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp InitUploadResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.UploadID == "" {
		t.Error("Expected upload ID, got empty string")
	}

	// Verify session created in database
	var session models.UploadSession
	if err := db.Where("id = ?", resp.UploadID).First(&session).Error; err != nil {
		t.Fatalf("Session not found in database: %v", err)
	}

	if session.Filename != "test.txt" {
		t.Errorf("Expected filename 'test.txt', got '%s'", session.Filename)
	}
}

func TestUploadChunk(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	// Create a session first
	tempDir := t.TempDir()
	session := &models.UploadSession{
		ID:             "test-upload-123",
		UserID:         user.ID,
		Filename:       "test.txt",
		LogicalPath:    "/",
		TotalSize:      1024,
		TotalChunks:    2,
		ChunkSize:      512,
		ReceivedChunks: 0,
		ChunksReceived: []byte("[]"),
		Status:         "active",
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Upload first chunk
	chunkData := bytes.Repeat([]byte("test"), 128) // 512 bytes
	req := httptest.NewRequest(http.MethodPost, "/api/uploads/test-upload-123/chunk?chunk=0", bytes.NewReader(chunkData))
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-123")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.UploadChunk(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify chunk file exists and content matches
	chunkPath := tempDir + "/chunk_0"
	if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
		t.Error("Chunk file not created")
	}

	// Verify chunk content matches uploaded data
	savedChunkData, err := os.ReadFile(chunkPath)
	if err != nil {
		t.Fatalf("Failed to read chunk file: %v", err)
	}
	if !bytes.Equal(savedChunkData, chunkData) {
		t.Errorf("Chunk content mismatch: expected %d bytes, got %d bytes", len(chunkData), len(savedChunkData))
	}

	// Verify session updated
	if err := db.Where("id = ?", session.ID).First(session).Error; err != nil {
		t.Fatalf("Failed to reload session: %v", err)
	}

	if session.ReceivedChunks != 1 {
		t.Errorf("Expected 1 received chunk, got %d", session.ReceivedChunks)
	}
}

func TestCancelUpload(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	tempDir := t.TempDir()
	session := &models.UploadSession{
		ID:          "test-upload-cancel",
		UserID:      user.ID,
		Filename:    "test.txt",
		LogicalPath: "/",
		TotalSize:   1024,
		TotalChunks: 2,
		ChunkSize:   512,
		Status:      "active",
		TempDir:     tempDir,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	// Create a temp file to ensure cleanup
	testFile := tempDir + "/chunk_0"
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/uploads/test-upload-cancel", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-cancel")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.CancelUpload(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify session marked as cancelled
	if err := db.Where("id = ?", session.ID).First(session).Error; err != nil {
		t.Fatalf("Failed to reload session: %v", err)
	}

	if session.Status != "cancelled" {
		t.Errorf("Expected status 'cancelled', got '%s'", session.Status)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	handler, db, _ := setupUploadHandlerTest(t)

	tempDir := t.TempDir()

	// Create expired session
	expiredSession := &models.UploadSession{
		ID:          "expired-session",
		UserID:      1,
		Filename:    "expired.txt",
		LogicalPath: "/",
		TotalSize:   1024,
		TotalChunks: 1,
		ChunkSize:   1024,
		Status:      "active",
		TempDir:     tempDir,
		ExpiresAt:   time.Now().Add(-1 * time.Hour), // Expired 1 hour ago
	}
	if err := db.Create(expiredSession).Error; err != nil {
		t.Fatalf("Failed to create expired session: %v", err)
	}

	// Run cleanup
	if err := handler.CleanupExpiredSessions(); err != nil {
		t.Fatalf("Cleanup failed: %v", err)
	}

	// Verify session marked as expired
	if err := db.Where("id = ?", expiredSession.ID).First(expiredSession).Error; err != nil {
		t.Fatalf("Failed to reload session: %v", err)
	}

	if expiredSession.Status != "expired" {
		t.Errorf("Expected status 'expired', got '%s'", expiredSession.Status)
	}
}

func TestGetUploadStatus(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	tempDir := t.TempDir()
	chunksReceived := []int{0, 2}
	chunksReceivedJSON, _ := json.Marshal(chunksReceived)

	session := &models.UploadSession{
		ID:             "test-upload-status",
		UserID:         user.ID,
		Filename:       "status-test.txt",
		LogicalPath:    "/docs",
		TotalSize:      1536,
		TotalChunks:    3,
		ChunkSize:      512,
		ReceivedChunks: 2,
		ChunksReceived: chunksReceivedJSON,
		Status:         "active",
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/uploads/test-upload-status", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-status")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.GetUploadStatus(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp ChunkStatusResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.UploadID != "test-upload-status" {
		t.Errorf("Expected upload ID 'test-upload-status', got '%s'", resp.UploadID)
	}
	if resp.Status != "active" {
		t.Errorf("Expected status 'active', got '%s'", resp.Status)
	}
	if resp.ReceivedChunks != 2 {
		t.Errorf("Expected 2 received chunks, got %d", resp.ReceivedChunks)
	}
	if resp.TotalChunks != 3 {
		t.Errorf("Expected 3 total chunks, got %d", resp.TotalChunks)
	}
	if len(resp.ChunksReceived) != 2 || resp.ChunksReceived[0] != 0 || resp.ChunksReceived[1] != 2 {
		t.Errorf("Expected chunks received [0, 2], got %v", resp.ChunksReceived)
	}
}

func TestGetUploadStatus_NotFound(t *testing.T) {
	handler, _, user := setupUploadHandlerTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/uploads/nonexistent-id", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "nonexistent-id")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.GetUploadStatus(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCompleteUpload(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	tempDir := t.TempDir()

	// Create chunks with known content
	chunk0Data := []byte("Hello, ")
	chunk1Data := []byte("World!")
	totalSize := int64(len(chunk0Data) + len(chunk1Data))

	// Write chunk files
	if err := os.WriteFile(filepath.Join(tempDir, "chunk_0"), chunk0Data, 0644); err != nil {
		t.Fatalf("Failed to write chunk 0: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "chunk_1"), chunk1Data, 0644); err != nil {
		t.Fatalf("Failed to write chunk 1: %v", err)
	}

	chunksReceived := []int{0, 1}
	chunksReceivedJSON, _ := json.Marshal(chunksReceived)

	session := &models.UploadSession{
		ID:             "test-upload-complete",
		UserID:         user.ID,
		Filename:       "complete-test.txt",
		LogicalPath:    "/",
		TotalSize:      totalSize,
		TotalChunks:    2,
		ChunkSize:      7,
		ReceivedChunks: 2,
		ChunksReceived: chunksReceivedJSON,
		Status:         "active",
		MimeType:       "text/plain",
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/test-upload-complete/complete", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-complete")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.CompleteUpload(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp["filename"] != "complete-test.txt" {
		t.Errorf("Expected filename 'complete-test.txt', got '%v'", resp["filename"])
	}
	if resp["size"].(float64) != float64(totalSize) {
		t.Errorf("Expected size %d, got %v", totalSize, resp["size"])
	}
	if resp["hash"] == nil || resp["hash"].(string) == "" {
		t.Error("Expected non-empty hash in response")
	}

	// Verify session marked as completed
	if err := db.Where("id = ?", session.ID).First(session).Error; err != nil {
		t.Fatalf("Failed to reload session: %v", err)
	}
	if session.Status != "completed" {
		t.Errorf("Expected session status 'completed', got '%s'", session.Status)
	}

	// Verify file record created
	var file models.File
	if err := db.Where("filename = ?", "complete-test.txt").First(&file).Error; err != nil {
		t.Fatalf("File record not found: %v", err)
	}
	if file.FileSize != totalSize {
		t.Errorf("Expected file size %d, got %d", totalSize, file.FileSize)
	}

	// Verify user storage usage updated
	var updatedUser models.User
	if err := db.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("Failed to reload user: %v", err)
	}
	if updatedUser.StorageUsed != totalSize {
		t.Errorf("Expected storage used %d, got %d", totalSize, updatedUser.StorageUsed)
	}
}

func TestCompleteUpload_HashMismatch(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	tempDir := t.TempDir()

	// Create chunk with known content
	chunkData := []byte("test content")
	if err := os.WriteFile(filepath.Join(tempDir, "chunk_0"), chunkData, 0644); err != nil {
		t.Fatalf("Failed to write chunk: %v", err)
	}

	chunksReceived := []int{0}
	chunksReceivedJSON, _ := json.Marshal(chunksReceived)

	session := &models.UploadSession{
		ID:             "test-upload-hash-mismatch",
		UserID:         user.ID,
		Filename:       "hash-test.txt",
		LogicalPath:    "/",
		TotalSize:      int64(len(chunkData)),
		TotalChunks:    1,
		ChunkSize:      int64(len(chunkData)),
		ReceivedChunks: 1,
		ChunksReceived: chunksReceivedJSON,
		Status:         "active",
		Hash:           "invalid-hash-value", // Intentionally wrong hash
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/test-upload-hash-mismatch/complete", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-hash-mismatch")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.CompleteUpload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for hash mismatch, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCompleteUpload_MissingChunks(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)

	tempDir := t.TempDir()
	chunksReceived := []int{0} // Only 1 chunk received out of 2
	chunksReceivedJSON, _ := json.Marshal(chunksReceived)

	session := &models.UploadSession{
		ID:             "test-upload-missing",
		UserID:         user.ID,
		Filename:       "missing-test.txt",
		LogicalPath:    "/",
		TotalSize:      1024,
		TotalChunks:    2,
		ChunkSize:      512,
		ReceivedChunks: 1, // Only 1 out of 2 received
		ChunksReceived: chunksReceivedJSON,
		Status:         "active",
		TempDir:        tempDir,
		ExpiresAt:      time.Now().Add(24 * time.Hour),
	}
	if err := db.Create(session).Error; err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/uploads/test-upload-missing/complete", nil)
	req = withUser(req, user)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "test-upload-missing")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	handler.CompleteUpload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 for missing chunks, got %d", w.Code)
	}
}

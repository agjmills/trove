package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
		UploadChunkSize:      5 * 1024 * 1024, // 5MB
		UploadSessionTimeout: 24 * time.Hour,
	}

	memStorage := storage.NewMemoryBackend()
	handler := NewUploadHandler(db, cfg, memStorage)

	return handler, db, user
}

func TestInitUpload(t *testing.T) {
	handler, db, user := setupUploadHandlerTest(t)
	defer db.Exec("DROP TABLE IF EXISTS upload_sessions")

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
	defer db.Exec("DROP TABLE IF EXISTS upload_sessions")

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

	// Verify chunk file exists
	chunkPath := tempDir + "/chunk_0"
	if _, err := os.Stat(chunkPath); os.IsNotExist(err) {
		t.Error("Chunk file not created")
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
	defer db.Exec("DROP TABLE IF EXISTS upload_sessions")

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
	defer db.Exec("DROP TABLE IF EXISTS upload_sessions")

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

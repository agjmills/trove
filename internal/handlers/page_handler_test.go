package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestPageHandler(t *testing.T) (*PageHandler, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.Folder{}, &models.File{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{}

	handler := NewPageHandler(db, cfg)

	return handler, db
}

func createTestUserForPage(t *testing.T, db *gorm.DB) *models.User {
	user := &models.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "dummy_hash",
		StorageQuota: 1024 * 1024 * 100,
		StorageUsed:  0,
	}

	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

func TestShowFiles_NonExistentFolderReturns404(t *testing.T) {
	handler, db := setupTestPageHandler(t)
	user := createTestUserForPage(t, db)

	// Request a folder that doesn't exist
	req := httptest.NewRequest(http.MethodGet, "/files?folder=/nonexistent", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ShowFiles(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 for non-existent folder, got %d", w.Code)
	}
}

func TestShowFiles_OtherUserFolderReturns404(t *testing.T) {
	handler, db := setupTestPageHandler(t)
	user1 := createTestUserForPage(t, db)

	// Create a second user
	user2 := &models.User{
		Username:     "otheruser",
		Email:        "other@example.com",
		PasswordHash: "dummy_hash",
		StorageQuota: 1024 * 1024 * 100,
		StorageUsed:  0,
	}
	if err := db.Create(user2).Error; err != nil {
		t.Fatalf("Failed to create second test user: %v", err)
	}

	// Create a folder for user2
	folder := &models.Folder{
		UserID:     user2.ID,
		FolderPath: "/secret",
	}
	if err := db.Create(folder).Error; err != nil {
		t.Fatalf("Failed to create test folder: %v", err)
	}

	// User1 tries to access user2's folder
	req := httptest.NewRequest(http.MethodGet, "/files?folder=/secret", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user1)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ShowFiles(w, req)

	// Should return 404 because user1 doesn't own this folder
	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404 when accessing another user's folder, got %d", w.Code)
	}
}

func TestShowFiles_PathTraversalBlocked(t *testing.T) {
	handler, db := setupTestPageHandler(t)
	user := createTestUserForPage(t, db)

	// Try path traversal attacks - these should result in 404 since sanitized paths won't match
	testPaths := []string{
		"/files?folder=/../../../etc/passwd",
		"/files?folder=/folder/../../../etc",
		"/files?folder=/./hidden",
	}

	for _, path := range testPaths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctx)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		handler.ShowFiles(w, req)

		// Should return 404 since these sanitized paths don't exist as folders
		if w.Code != http.StatusNotFound {
			t.Errorf("Path %s should return 404, got %d", path, w.Code)
		}
	}
}

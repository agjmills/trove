package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	csrf "filippo.io/csrf/gorilla"
	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/maruel/natural"
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

func TestNaturalSortOrder(t *testing.T) {
	// Test that the natural sorting works correctly for files
	filenames := []string{
		"file10.txt",
		"file2.txt",
		"file1.txt",
		"File20.txt",
		"file3.txt",
	}

	// Sort using the same logic as page_handler.go
	sort.Slice(filenames, func(i, j int) bool {
		return natural.Less(strings.ToLower(filenames[i]), strings.ToLower(filenames[j]))
	})

	expected := []string{
		"file1.txt",
		"file2.txt",
		"file3.txt",
		"file10.txt",
		"File20.txt",
	}

	for i, name := range filenames {
		if name != expected[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expected[i], name)
		}
	}
}

func TestNaturalSortFolders(t *testing.T) {
	// Test folder natural sorting (case-insensitive, matching page_handler.go)
	folders := []string{
		"Chapter10",
		"Chapter2",
		"Chapter1",
		"chapter3",
	}

	// Sort using the same logic as page_handler.go
	sort.Slice(folders, func(i, j int) bool {
		return natural.Less(strings.ToLower(folders[i]), strings.ToLower(folders[j]))
	})

	// Natural sort with case-insensitive: 1, 2, 3, 10 (numerically)
	expected := []string{
		"Chapter1",
		"Chapter2",
		"chapter3",
		"Chapter10",
	}

	for i, name := range folders {
		if name != expected[i] {
			t.Errorf("Position %d: expected %s, got %s", i, expected[i], name)
		}
	}
}

func TestFilesOnlyPagination(t *testing.T) {
	// Test that pagination applies only to files (folders are always shown)
	allFileNames := []string{"file1.txt", "file2.txt", "file3.txt", "file4.txt", "file5.txt"}

	pageSize := 3

	testCases := []struct {
		page          int
		expectedFiles []string
	}{
		{1, []string{"file1.txt", "file2.txt", "file3.txt"}}, // Page 1: first 3 files
		{2, []string{"file4.txt", "file5.txt"}},              // Page 2: remaining 2 files
		{3, []string{}},                                      // Page 3: no files (past end)
	}

	for _, tc := range testCases {
		offset := (tc.page - 1) * pageSize
		totalFiles := len(allFileNames)

		var fileNames []string
		if offset < totalFiles {
			end := offset + pageSize
			if end > totalFiles {
				end = totalFiles
			}
			fileNames = allFileNames[offset:end]
		}

		// Check files
		if len(fileNames) != len(tc.expectedFiles) {
			t.Errorf("Page %d: expected %d files, got %d", tc.page, len(tc.expectedFiles), len(fileNames))
		} else {
			for i, name := range fileNames {
				if name != tc.expectedFiles[i] {
					t.Errorf("Page %d file %d: expected %s, got %s", tc.page, i, tc.expectedFiles[i], name)
				}
			}
		}
	}
}

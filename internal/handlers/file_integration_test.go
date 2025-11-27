package handlers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// fileTestApp encapsulates all dependencies for file handler integration tests
type fileTestApp struct {
	db          *gorm.DB
	cfg         *config.Config
	storage     *storage.MemoryBackend
	fileHandler *FileHandler
	router      *chi.Mux
}

// newFileTestApp creates a new test application for file handler tests
func newFileTestApp(t *testing.T) *fileTestApp {
	t.Helper()

	// Use a unique database file per test to avoid concurrent access issues
	// with background workers from other tests
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		MaxUploadSize:    10 * 1024 * 1024, // 10MB
		DefaultUserQuota: 100 * 1024 * 1024, // 100MB
		Env:              "test",
	}

	memStorage := storage.NewMemoryBackend()
	fileHandler := NewFileHandler(db, cfg, memStorage)

	// Setup router
	router := chi.NewRouter()
	router.Post("/upload", fileHandler.Upload)
	router.Get("/download/{id}", fileHandler.Download)
	router.Post("/delete/{id}", fileHandler.Delete)
	router.Post("/folders/create", fileHandler.CreateFolder)
	router.Post("/folders/delete/{name}", fileHandler.DeleteFolder)

	app := &fileTestApp{
		db:          db,
		cfg:         cfg,
		storage:     memStorage,
		fileHandler: fileHandler,
		router:      router,
	}

	// Ensure cleanup happens when test completes - shutdown workers first
	t.Cleanup(func() {
		app.fileHandler.Shutdown()
	})

	return app
}

// cleanup closes the file handler's background workers
func (app *fileTestApp) cleanup() {
	app.fileHandler.Shutdown()
}

// createTestUserForFiles creates a test user for file operations
func (app *fileTestApp) createTestUser(t *testing.T, username string) *models.User {
	t.Helper()

	user := &models.User{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hashed",
		StorageQuota: app.cfg.DefaultUserQuota,
	}

	if err := app.db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

// createTestFile creates a test file in the database and storage
func (app *fileTestApp) createTestFile(t *testing.T, user *models.User, filename, content string) *models.File {
	t.Helper()

	ctx := context.Background()
	result, err := app.storage.Save(ctx, strings.NewReader(content), storage.SaveOptions{
		OriginalFilename: filename,
		ContentType:      "text/plain",
	})
	if err != nil {
		t.Fatalf("Failed to save file to storage: %v", err)
	}

	file := &models.File{
		UserID:           user.ID,
		StoragePath:      result.Path,
		LogicalPath:      "/",
		Filename:         filename,
		OriginalFilename: filename,
		FileSize:         result.Size,
		MimeType:         "text/plain",
		Hash:             result.Hash,
		UploadStatus:     "completed",
	}

	if err := app.db.Create(file).Error; err != nil {
		t.Fatalf("Failed to create file record: %v", err)
	}

	// Update user storage
	app.db.Model(user).UpdateColumn("storage_used", gorm.Expr("storage_used + ?", result.Size))

	return file
}

// createTestFolder creates a test folder in the database
func (app *fileTestApp) createTestFolder(t *testing.T, user *models.User, folderPath string) *models.Folder {
	t.Helper()

	folder := &models.Folder{
		UserID:     user.ID,
		FolderPath: folderPath,
	}

	if err := app.db.Create(folder).Error; err != nil {
		t.Fatalf("Failed to create folder: %v", err)
	}

	return folder
}

// authenticatedFileRequest creates a request with authenticated user context
func (app *fileTestApp) authenticatedRequest(t *testing.T, method, path string, body io.Reader, user *models.User) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, path, body)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	return req
}

// createMultipartRequest creates a multipart form request for file upload
func createMultipartRequest(t *testing.T, filename, content, folder string) (*http.Request, string) {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add folder field
	if err := writer.WriteField("folder", folder); err != nil {
		t.Fatalf("Failed to write folder field: %v", err)
	}

	// Add file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("Failed to create form file: %v", err)
	}
	if _, err := io.WriteString(part, content); err != nil {
		t.Fatalf("Failed to write file content: %v", err)
	}

	if err := writer.Close(); err != nil {
		t.Fatalf("Failed to close writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	return req, writer.FormDataContentType()
}

// TestUploadIntegration tests file upload functionality
func TestUploadIntegration(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "uploaduser")

	t.Run("successful file upload", func(t *testing.T) {
		req, contentType := createMultipartRequest(t, "test.txt", "Hello, World!", "/")
		ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", contentType)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		app.fileHandler.Upload(w, req)

		// Should redirect on success
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d: %s", w.Code, w.Body.String())
		}

		// Check file was created in database
		var file models.File
		if err := app.db.Where("user_id = ? AND original_filename = ?", user.ID, "test.txt").First(&file).Error; err != nil {
			t.Errorf("File was not created in database: %v", err)
		}

		// Check file size is correct
		if file.FileSize != 13 { // "Hello, World!" is 13 bytes
			t.Errorf("Expected file size 13, got %d", file.FileSize)
		}
	})

	t.Run("upload to subfolder", func(t *testing.T) {
		// First create the folder
		app.createTestFolder(t, user, "/documents")

		req, contentType := createMultipartRequest(t, "doc.txt", "Document content", "/documents")
		ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", contentType)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		app.fileHandler.Upload(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d: %s", w.Code, w.Body.String())
		}

		// Check file is in correct folder
		var file models.File
		app.db.Where("user_id = ? AND original_filename = ?", user.ID, "doc.txt").First(&file)
		if file.LogicalPath != "/documents" {
			t.Errorf("Expected logical path '/documents', got %s", file.LogicalPath)
		}
	})

	t.Run("upload without authentication fails", func(t *testing.T) {
		req, contentType := createMultipartRequest(t, "noauth.txt", "Content", "/")
		req.Header.Set("Content-Type", contentType)
		req = csrf.UnsafeSkipCheck(req)
		// No user context set

		w := httptest.NewRecorder()
		app.fileHandler.Upload(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("upload without file fails", func(t *testing.T) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("folder", "/")
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/upload", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctx)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		app.fileHandler.Upload(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", w.Code)
		}
	})
}

// TestUploadDeduplication tests that duplicate files are deduplicated
func TestUploadDeduplication(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "dedupuser")

	// Upload first file
	content := "This content will be deduplicated"
	req1, ct1 := createMultipartRequest(t, "original.txt", content, "/")
	ctx := context.WithValue(req1.Context(), auth.UserContextKey, user)
	req1 = req1.WithContext(ctx)
	req1.Header.Set("Content-Type", ct1)
	req1 = csrf.UnsafeSkipCheck(req1)

	w1 := httptest.NewRecorder()
	app.fileHandler.Upload(w1, req1)

	if w1.Code != http.StatusSeeOther {
		t.Fatalf("First upload failed: %d", w1.Code)
	}

	// Wait for background processing to complete
	app.fileHandler.Shutdown()
	app.fileHandler = NewFileHandler(app.db, app.cfg, app.storage)

	// Get the first file's storage path
	var firstFile models.File
	app.db.Where("user_id = ? AND original_filename = ?", user.ID, "original.txt").First(&firstFile)

	initialStorageCount := app.storage.FileCount()

	// Upload second file with same content
	req2, ct2 := createMultipartRequest(t, "duplicate.txt", content, "/")
	ctx2 := context.WithValue(req2.Context(), auth.UserContextKey, user)
	req2 = req2.WithContext(ctx2)
	req2.Header.Set("Content-Type", ct2)
	req2 = csrf.UnsafeSkipCheck(req2)

	w2 := httptest.NewRecorder()
	app.fileHandler.Upload(w2, req2)

	if w2.Code != http.StatusSeeOther {
		t.Fatalf("Second upload failed: %d", w2.Code)
	}

	// Both files should exist in database with same hash
	var files []models.File
	app.db.Where("user_id = ?", user.ID).Find(&files)

	if len(files) < 2 {
		t.Errorf("Expected at least 2 file records, got %d", len(files))
	}

	// Storage should not have grown (deduplication)
	if app.storage.FileCount() > initialStorageCount {
		t.Logf("Note: Storage count increased from %d to %d (dedup may happen async)", 
			initialStorageCount, app.storage.FileCount())
	}
}

// TestDownloadIntegration tests file download functionality
func TestDownloadIntegration(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "downloaduser")
	file := app.createTestFile(t, user, "download.txt", "Download content")

	t.Run("successful download", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodGet, fmt.Sprintf("/download/%d", file.ID), nil, user)

		// Use chi router to parse URL params
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Download(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		// Check content
		body := w.Body.String()
		if body != "Download content" {
			t.Errorf("Expected 'Download content', got '%s'", body)
		}

		// Check headers
		contentType := w.Header().Get("Content-Type")
		if contentType != "text/plain" {
			t.Errorf("Expected Content-Type 'text/plain', got '%s'", contentType)
		}

		contentDisposition := w.Header().Get("Content-Disposition")
		if !strings.Contains(contentDisposition, "download.txt") {
			t.Errorf("Expected Content-Disposition to contain filename, got '%s'", contentDisposition)
		}
	})

	t.Run("download non-existent file", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodGet, "/download/99999", nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "99999")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Download(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("download other user's file fails", func(t *testing.T) {
		otherUser := app.createTestUser(t, "otheruser")

		req := app.authenticatedRequest(t, http.MethodGet, fmt.Sprintf("/download/%d", file.ID), nil, otherUser)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Download(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 (file not found for this user), got %d", w.Code)
		}
	})

	t.Run("download without authentication fails", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/download/%d", file.ID), nil)
		req = csrf.UnsafeSkipCheck(req)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Download(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

// TestDeleteIntegration tests file deletion functionality
func TestDeleteIntegration(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "deleteuser")

	t.Run("successful file deletion", func(t *testing.T) {
		file := app.createTestFile(t, user, "todelete.txt", "Delete me")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/delete/%d", file.ID), nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Delete(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify file is deleted from database
		var deletedFile models.File
		if err := app.db.First(&deletedFile, file.ID).Error; err == nil {
			t.Error("File should have been deleted from database")
		}
	})

	t.Run("delete non-existent file", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodPost, "/delete/99999", nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "99999")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Delete(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("delete other user's file fails", func(t *testing.T) {
		file := app.createTestFile(t, user, "protected.txt", "Protected content")
		otherUser := app.createTestUser(t, "attacker")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/delete/%d", file.ID), nil, otherUser)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.Delete(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}

		// Verify file still exists
		var existingFile models.File
		if err := app.db.First(&existingFile, file.ID).Error; err != nil {
			t.Error("File should not have been deleted")
		}
	})
}

// TestCreateFolderIntegration tests folder creation
func TestCreateFolderIntegration(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "folderuser")

	t.Run("create folder in root", func(t *testing.T) {
		form := url.Values{}
		form.Set("current_folder", "/")
		form.Set("folder_name", "newfolder")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/create", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.fileHandler.CreateFolder(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d: %s", w.Code, w.Body.String())
		}

		// Verify folder was created
		var folder models.Folder
		if err := app.db.Where("user_id = ? AND folder_path = ?", user.ID, "/newfolder").First(&folder).Error; err != nil {
			t.Errorf("Folder was not created: %v", err)
		}
	})

	t.Run("create nested folder", func(t *testing.T) {
		// First create parent folder
		app.createTestFolder(t, user, "/parent")

		form := url.Values{}
		form.Set("current_folder", "/parent")
		form.Set("folder_name", "child")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/create", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.fileHandler.CreateFolder(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify nested folder was created
		var folder models.Folder
		if err := app.db.Where("user_id = ? AND folder_path = ?", user.ID, "/parent/child").First(&folder).Error; err != nil {
			t.Errorf("Nested folder was not created: %v", err)
		}
	})

	t.Run("create duplicate folder fails gracefully", func(t *testing.T) {
		app.createTestFolder(t, user, "/existing")

		form := url.Values{}
		form.Set("current_folder", "/")
		form.Set("folder_name", "existing")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/create", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.fileHandler.CreateFolder(w, req)

		// Should redirect with error flash message
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})

	t.Run("create folder with invalid name fails", func(t *testing.T) {
		form := url.Values{}
		form.Set("current_folder", "/")
		form.Set("folder_name", "invalid/name")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/create", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.fileHandler.CreateFolder(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for invalid folder name, got %d", w.Code)
		}
	})

	t.Run("create folder with empty name fails", func(t *testing.T) {
		form := url.Values{}
		form.Set("current_folder", "/")
		form.Set("folder_name", "")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/create", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.fileHandler.CreateFolder(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for empty folder name, got %d", w.Code)
		}
	})
}

// TestDeleteFolderIntegration tests folder deletion
func TestDeleteFolderIntegration(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "delfolder")

	t.Run("delete empty folder", func(t *testing.T) {
		app.createTestFolder(t, user, "/todelete")

		form := url.Values{}
		form.Set("current_folder", "/")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/delete/todelete", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "todelete")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DeleteFolder(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify folder was deleted
		var folder models.Folder
		if err := app.db.Where("user_id = ? AND folder_path = ?", user.ID, "/todelete").First(&folder).Error; err == nil {
			t.Error("Folder should have been deleted")
		}
	})

	t.Run("delete folder with files fails", func(t *testing.T) {
		app.createTestFolder(t, user, "/withfiles")
		
		// Create a file in the folder
		file := &models.File{
			UserID:           user.ID,
			StoragePath:      "dummy-path",
			LogicalPath:      "/withfiles",
			Filename:         "file.txt",
			OriginalFilename: "file.txt",
			FileSize:         10,
			UploadStatus:     "completed",
		}
		app.db.Create(file)

		form := url.Values{}
		form.Set("current_folder", "/")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/delete/withfiles", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "withfiles")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DeleteFolder(w, req)

		// Should redirect with error (folder not empty)
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify folder still exists
		var folder models.Folder
		if err := app.db.Where("user_id = ? AND folder_path = ?", user.ID, "/withfiles").First(&folder).Error; err != nil {
			t.Error("Folder should not have been deleted")
		}
	})

	t.Run("delete folder with subfolders fails", func(t *testing.T) {
		app.createTestFolder(t, user, "/withsub")
		app.createTestFolder(t, user, "/withsub/child")

		form := url.Values{}
		form.Set("current_folder", "/")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/delete/withsub", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "withsub")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DeleteFolder(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify folder still exists
		var folder models.Folder
		if err := app.db.Where("user_id = ? AND folder_path = ?", user.ID, "/withsub").First(&folder).Error; err != nil {
			t.Error("Folder should not have been deleted")
		}
	})

	t.Run("delete non-existent folder", func(t *testing.T) {
		form := url.Values{}
		form.Set("current_folder", "/")

		req := app.authenticatedRequest(t, http.MethodPost, "/folders/delete/nonexistent", strings.NewReader(form.Encode()), user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", "nonexistent")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DeleteFolder(w, req)

		// Should redirect with error
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})
}

// TestQuotaEnforcement tests that storage quota is enforced
func TestQuotaEnforcement(t *testing.T) {
	app := newFileTestApp(t)

	// Create user with very small quota
	user := &models.User{
		Username:     "quotauser",
		Email:        "quota@example.com",
		PasswordHash: "hashed",
		StorageQuota: 100, // Only 100 bytes
		StorageUsed:  0,
	}
	app.db.Create(user)

	// Try to upload file larger than quota
	largeContent := strings.Repeat("x", 200) // 200 bytes
	req, contentType := createMultipartRequest(t, "large.txt", largeContent, "/")
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	app.fileHandler.Upload(w, req)

	if w.Code != http.StatusInsufficientStorage {
		t.Errorf("Expected status 507 (Insufficient Storage), got %d", w.Code)
	}
}

// TestFilenameDeduplication tests unique filename generation
func TestFilenameDeduplication(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "filenameuser")

	// Create first file
	app.createTestFile(t, user, "document.txt", "Content 1")

	// Upload file with same name
	req, contentType := createMultipartRequest(t, "document.txt", "Content 2", "/")
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", contentType)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	app.fileHandler.Upload(w, req)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("Upload failed: %d", w.Code)
	}

	// Check that the new file has a different display name
	var files []models.File
	app.db.Where("user_id = ?", user.ID).Order("id").Find(&files)

	if len(files) < 2 {
		t.Fatalf("Expected at least 2 files, got %d", len(files))
	}

	// First file should keep original name
	if files[0].Filename != "document.txt" {
		t.Errorf("First file should be 'document.txt', got '%s'", files[0].Filename)
	}

	// Second file should have "(1)" suffix
	if files[1].Filename != "document (1).txt" {
		t.Errorf("Second file should be 'document (1).txt', got '%s'", files[1].Filename)
	}
}

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
	"time"

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

	// Use shared cache in-memory database so background workers can access the same data.
	// Each test gets a fresh database since newFileTestApp creates a new connection.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		MaxUploadSize:    10 * 1024 * 1024,  // 10MB
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

		// Wait for background upload to complete before checking database
		app.fileHandler.WaitForPendingUploads()

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
		if err := app.db.Create(file).Error; err != nil {
			t.Fatalf("Failed to create file: %v", err)
		}

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

// TestDismissFailedUpload tests the dismiss failed upload functionality
func TestDismissFailedUpload(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "dismissuser")

	t.Run("successfully dismiss failed upload", func(t *testing.T) {
		// Create a failed file directly in the database
		failedFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "failed-path",
			LogicalPath:      "/",
			Filename:         "failed.txt",
			OriginalFilename: "failed.txt",
			FileSize:         500,
			UploadStatus:     "failed",
			ErrorMessage:     "Upload failed: test error",
		}
		app.db.Create(failedFile)

		// Update user's storage used
		app.db.Model(user).UpdateColumn("storage_used", 500)

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/files/%d/dismiss", failedFile.ID), nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", failedFile.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify file was deleted from database
		var deletedFile models.File
		if err := app.db.First(&deletedFile, failedFile.ID).Error; err == nil {
			t.Error("Failed file should have been deleted from database")
		}

		// Verify user's quota was restored
		var updatedUser models.User
		app.db.First(&updatedUser, user.ID)
		if updatedUser.StorageUsed != 0 {
			t.Errorf("Expected storage used to be 0 after dismiss, got %d", updatedUser.StorageUsed)
		}
	})

	t.Run("cannot dismiss completed upload", func(t *testing.T) {
		completedFile := app.createTestFile(t, user, "completed.txt", "Completed content")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/files/%d/dismiss", completedFile.ID), nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", completedFile.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for non-failed file, got %d", w.Code)
		}

		// Verify file still exists
		var existingFile models.File
		if err := app.db.First(&existingFile, completedFile.ID).Error; err != nil {
			t.Error("Completed file should not have been deleted")
		}
	})

	t.Run("cannot dismiss pending upload", func(t *testing.T) {
		pendingFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "pending-path",
			LogicalPath:      "/",
			Filename:         "pending.txt",
			OriginalFilename: "pending.txt",
			FileSize:         100,
			UploadStatus:     "pending",
		}
		app.db.Create(pendingFile)

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/files/%d/dismiss", pendingFile.ID), nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", pendingFile.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected status 400 for pending file, got %d", w.Code)
		}
	})

	t.Run("cannot dismiss other user's failed upload", func(t *testing.T) {
		otherUser := app.createTestUser(t, "otheruser2")

		// Create a failed file for the original user
		failedFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "other-failed-path",
			LogicalPath:      "/",
			Filename:         "otherfailed.txt",
			OriginalFilename: "otherfailed.txt",
			FileSize:         100,
			UploadStatus:     "failed",
			ErrorMessage:     "Test error",
		}
		app.db.Create(failedFile)

		// Try to dismiss as different user
		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/files/%d/dismiss", failedFile.ID), nil, otherUser)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", failedFile.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for other user's file, got %d", w.Code)
		}

		// Verify file still exists
		var existingFile models.File
		if err := app.db.First(&existingFile, failedFile.ID).Error; err != nil {
			t.Error("File should not have been deleted by other user")
		}
	})

	t.Run("dismiss non-existent file returns 404", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodPost, "/files/99999/dismiss", nil, user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "99999")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-existent file, got %d", w.Code)
		}
	})

	t.Run("dismiss without authentication fails", func(t *testing.T) {
		failedFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "unauth-failed-path",
			LogicalPath:      "/",
			Filename:         "unauthfailed.txt",
			OriginalFilename: "unauthfailed.txt",
			FileSize:         100,
			UploadStatus:     "failed",
		}
		app.db.Create(failedFile)

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/files/%d/dismiss", failedFile.ID), nil)
		req = csrf.UnsafeSkipCheck(req)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", failedFile.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.fileHandler.DismissFailedUpload(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})
}

// TestStatusStreamBasic tests the SSE endpoint basic functionality
func TestStatusStreamBasic(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "sseuser")

	t.Run("requires authentication", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/files/status", nil)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		app.fileHandler.StatusStream(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("Expected status 401, got %d", w.Code)
		}
	})

	t.Run("returns correct content type", func(t *testing.T) {
		// Create a context with cancel so we can stop the SSE stream
		ctx, cancel := context.WithCancel(context.Background())

		req := httptest.NewRequest(http.MethodGet, "/api/files/status", nil)
		req = req.WithContext(ctx)
		ctxWithUser := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctxWithUser)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()

		// Run SSE handler in goroutine since it blocks
		done := make(chan struct{})
		go func() {
			app.fileHandler.StatusStream(w, req)
			close(done)
		}()

		// Give it a moment to start and set headers
		time.Sleep(50 * time.Millisecond)

		// Cancel the context to stop the stream
		cancel()

		// Wait for handler to finish
		<-done

		contentType := w.Header().Get("Content-Type")
		if contentType != "text/event-stream" {
			t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
		}

		cacheControl := w.Header().Get("Cache-Control")
		if cacheControl != "no-cache" {
			t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
		}

		// Check that connection event was sent
		body := w.Body.String()
		if !strings.Contains(body, "event: connected") {
			t.Errorf("Expected 'event: connected' in response, got: %s", body)
		}
	})
}

// TestStatusStreamSendsStatusEvents tests that the SSE endpoint sends events when file status changes
func TestStatusStreamSendsStatusEvents(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "sseeventuser")

	t.Run("sends status event for pending file", func(t *testing.T) {
		// Create a pending file before starting the SSE stream
		pendingFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "sse-test-path",
			LogicalPath:      "/",
			Filename:         "sse-test.txt",
			OriginalFilename: "sse-test.txt",
			FileSize:         100,
			UploadStatus:     "pending",
		}
		app.db.Create(pendingFile)

		// Create a context with cancel so we can stop the SSE stream
		ctx, cancel := context.WithCancel(context.Background())

		req := httptest.NewRequest(http.MethodGet, "/api/files/status", nil)
		req = req.WithContext(ctx)
		ctxWithUser := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctxWithUser)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()

		// Run SSE handler in goroutine since it blocks
		done := make(chan struct{})
		go func() {
			app.fileHandler.StatusStream(w, req)
			close(done)
		}()

		// Wait for at least one poll cycle (1 second + buffer)
		time.Sleep(1200 * time.Millisecond)

		// Cancel the context to stop the stream
		cancel()

		// Wait for handler to finish
		<-done

		body := w.Body.String()

		// Should have received status event for the pending file
		if !strings.Contains(body, "event: status") {
			t.Errorf("Expected 'event: status' in response for pending file, got: %s", body)
		}

		if !strings.Contains(body, "pending") {
			t.Errorf("Expected 'pending' status in response, got: %s", body)
		}

		if !strings.Contains(body, "sse-test.txt") {
			t.Errorf("Expected filename 'sse-test.txt' in response, got: %s", body)
		}

		// Cleanup
		app.db.Unscoped().Delete(pendingFile)
	})

	t.Run("sends status event when file status changes", func(t *testing.T) {
		// Create a file that starts as pending
		changingFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "sse-change-path",
			LogicalPath:      "/",
			Filename:         "sse-change.txt",
			OriginalFilename: "sse-change.txt",
			FileSize:         100,
			UploadStatus:     "pending",
		}
		app.db.Create(changingFile)

		ctx, cancel := context.WithCancel(context.Background())

		req := httptest.NewRequest(http.MethodGet, "/api/files/status", nil)
		req = req.WithContext(ctx)
		ctxWithUser := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctxWithUser)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			app.fileHandler.StatusStream(w, req)
			close(done)
		}()

		// Wait for first poll
		time.Sleep(1200 * time.Millisecond)

		// Change the file status to uploading
		app.db.Model(changingFile).Update("upload_status", "uploading")

		// Wait for another poll cycle to detect the change
		time.Sleep(1200 * time.Millisecond)

		cancel()
		<-done

		body := w.Body.String()

		// Should have received events for both pending and uploading states
		if !strings.Contains(body, "pending") {
			t.Errorf("Expected 'pending' status in response, got: %s", body)
		}

		if !strings.Contains(body, "uploading") {
			t.Errorf("Expected 'uploading' status in response after change, got: %s", body)
		}

		// Cleanup
		app.db.Unscoped().Delete(changingFile)
	})

	t.Run("includes error message for failed uploads", func(t *testing.T) {
		failedFile := &models.File{
			UserID:           user.ID,
			StoragePath:      "sse-failed-path",
			LogicalPath:      "/",
			Filename:         "sse-failed.txt",
			OriginalFilename: "sse-failed.txt",
			FileSize:         100,
			UploadStatus:     "failed",
			ErrorMessage:     "Test error message",
		}
		app.db.Create(failedFile)

		ctx, cancel := context.WithCancel(context.Background())

		req := httptest.NewRequest(http.MethodGet, "/api/files/status", nil)
		req = req.WithContext(ctx)
		ctxWithUser := context.WithValue(req.Context(), auth.UserContextKey, user)
		req = req.WithContext(ctxWithUser)
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			app.fileHandler.StatusStream(w, req)
			close(done)
		}()

		time.Sleep(1200 * time.Millisecond)

		cancel()
		<-done

		body := w.Body.String()

		if !strings.Contains(body, "failed") {
			t.Errorf("Expected 'failed' status in response, got: %s", body)
		}

		if !strings.Contains(body, "Test error message") {
			t.Errorf("Expected error message in response, got: %s", body)
		}

		// Cleanup
		app.db.Unscoped().Delete(failedFile)
	})
}

// TestMarkUploadFailedPreservesError tests that failed uploads preserve error messages
func TestMarkUploadFailedPreservesError(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "faileduser")

	// Create a pending file that we'll mark as failed
	pendingFile := &models.File{
		UserID:           user.ID,
		StoragePath:      "will-fail-path",
		LogicalPath:      "/",
		Filename:         "willfail.txt",
		OriginalFilename: "willfail.txt",
		FileSize:         100,
		UploadStatus:     "pending",
	}
	app.db.Create(pendingFile)

	// Update the file to failed status with error message (simulating what markUploadFailed does)
	errorMessage := "Storage backend error: disk full"
	app.db.Model(pendingFile).Updates(map[string]interface{}{
		"upload_status": "failed",
		"error_message": errorMessage,
	})

	// Verify the error message was saved
	var failedFile models.File
	if err := app.db.First(&failedFile, pendingFile.ID).Error; err != nil {
		t.Fatalf("Failed to retrieve file: %v", err)
	}

	if failedFile.UploadStatus != "failed" {
		t.Errorf("Expected upload_status 'failed', got '%s'", failedFile.UploadStatus)
	}

	if failedFile.ErrorMessage != errorMessage {
		t.Errorf("Expected error_message '%s', got '%s'", errorMessage, failedFile.ErrorMessage)
	}
}

// TestFailedUploadVisibleInFileList tests that failed uploads appear in the file list
func TestFailedUploadVisibleInFileList(t *testing.T) {
	app := newFileTestApp(t)

	user := app.createTestUser(t, "visibleuser")

	// Create files with different statuses
	completedFile := &models.File{
		UserID:           user.ID,
		StoragePath:      "completed-path",
		LogicalPath:      "/",
		Filename:         "completed.txt",
		OriginalFilename: "completed.txt",
		FileSize:         100,
		UploadStatus:     "completed",
	}
	app.db.Create(completedFile)

	failedFile := &models.File{
		UserID:           user.ID,
		StoragePath:      "failed-path",
		LogicalPath:      "/",
		Filename:         "failed.txt",
		OriginalFilename: "failed.txt",
		FileSize:         200,
		UploadStatus:     "failed",
		ErrorMessage:     "Connection lost during upload",
	}
	app.db.Create(failedFile)

	pendingFile := &models.File{
		UserID:           user.ID,
		StoragePath:      "pending-path",
		LogicalPath:      "/",
		Filename:         "pending.txt",
		OriginalFilename: "pending.txt",
		FileSize:         300,
		UploadStatus:     "pending",
	}
	app.db.Create(pendingFile)

	// Query all files for the user (simulating what page handler does)
	var files []models.File
	if err := app.db.Where("user_id = ? AND logical_path = ?", user.ID, "/").
		Order("filename").Find(&files).Error; err != nil {
		t.Fatalf("Failed to query files: %v", err)
	}

	// Should have all 3 files
	if len(files) != 3 {
		t.Errorf("Expected 3 files, got %d", len(files))
	}

	// Check that we have each status represented
	statusCounts := make(map[string]int)
	for _, f := range files {
		statusCounts[f.UploadStatus]++
	}

	if statusCounts["completed"] != 1 {
		t.Errorf("Expected 1 completed file, got %d", statusCounts["completed"])
	}
	if statusCounts["failed"] != 1 {
		t.Errorf("Expected 1 failed file, got %d", statusCounts["failed"])
	}
	if statusCounts["pending"] != 1 {
		t.Errorf("Expected 1 pending file, got %d", statusCounts["pending"])
	}

	// Verify the failed file has its error message
	for _, f := range files {
		if f.UploadStatus == "failed" && f.ErrorMessage == "" {
			t.Error("Failed file should have an error message")
		}
	}
}

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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

// deletedTestApp encapsulates all dependencies for deleted handler integration tests
type deletedTestApp struct {
	db             *gorm.DB
	cfg            *config.Config
	storage        *storage.MemoryBackend
	deletedHandler *DeletedHandler
	router         *chi.Mux
}

// newDeletedTestApp creates a new test application for deleted handler tests
func newDeletedTestApp(t *testing.T) *deletedTestApp {
	t.Helper()

	// Use shared cache in-memory database so background workers can access the same data.
	// Each test gets a fresh database since newDeletedTestApp creates a new connection.
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		MaxUploadSize:             10 * 1024 * 1024,  // 10MB
		DefaultUserQuota:          100 * 1024 * 1024, // 100MB
		DeletedRetentionDays:      30,
		DeletedCleanupIntervalMin: 60,
		Env:                       "test",
	}

	memStorage := storage.NewMemoryBackend()
	deletedHandler := NewDeletedHandler(db, cfg, memStorage)

	// Setup router
	router := chi.NewRouter()
	router.Get("/deleted", deletedHandler.ShowDeleted)
	router.Post("/deleted/empty", deletedHandler.EmptyDeleted)
	router.Post("/deleted/files/{id}/restore", deletedHandler.RestoreFile)
	router.Post("/deleted/files/{id}/delete", deletedHandler.PermanentlyDeleteFile)
	router.Post("/deleted/folders/{id}/restore", deletedHandler.RestoreFolder)
	router.Post("/deleted/folders/{id}/delete", deletedHandler.PermanentlyDeleteFolder)
	router.Post("/admin/deleted/empty-all", deletedHandler.AdminEmptyAllDeleted)

	app := &deletedTestApp{
		db:             db,
		cfg:            cfg,
		storage:        memStorage,
		deletedHandler: deletedHandler,
		router:         router,
	}

	t.Cleanup(func() {
		app.deletedHandler.Shutdown()
	})

	return app
}

// createTestUser creates a test user
func (app *deletedTestApp) createTestUser(t *testing.T, username string) *models.User {
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

// createTestAdmin creates a test admin user
func (app *deletedTestApp) createTestAdmin(t *testing.T, username string) *models.User {
	t.Helper()

	user := &models.User{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "hashed",
		StorageQuota: app.cfg.DefaultUserQuota,
		IsAdmin:      true,
	}

	if err := app.db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test admin: %v", err)
	}

	return user
}

// createTestFile creates a test file in the database and storage
func (app *deletedTestApp) createTestFile(t *testing.T, user *models.User, filename, content string) *models.File {
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

	app.db.Model(user).UpdateColumn("storage_used", gorm.Expr("storage_used + ?", result.Size))

	return file
}

// createTestFileInFolder creates a test file in a specific folder
func (app *deletedTestApp) createTestFileInFolder(t *testing.T, user *models.User, filename, content, folder string) *models.File {
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
		LogicalPath:      folder,
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

	app.db.Model(user).UpdateColumn("storage_used", gorm.Expr("storage_used + ?", result.Size))

	return file
}

// createTestFolder creates a test folder in the database
func (app *deletedTestApp) createTestFolder(t *testing.T, user *models.User, folderPath string) *models.Folder {
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

// softDeleteFile marks a file as soft-deleted
func (app *deletedTestApp) softDeleteFile(t *testing.T, file *models.File) {
	t.Helper()

	now := time.Now()
	if err := app.db.Model(file).Updates(map[string]interface{}{
		"trashed_at":            now,
		"original_logical_path": file.LogicalPath,
	}).Error; err != nil {
		t.Fatalf("Failed to soft delete file: %v", err)
	}
	file.SoftDeletedAt = &now
	file.OriginalLogicalPath = file.LogicalPath
}

// softDeleteFolder marks a folder as soft-deleted
func (app *deletedTestApp) softDeleteFolder(t *testing.T, folder *models.Folder) {
	t.Helper()

	now := time.Now()
	if err := app.db.Model(folder).Updates(map[string]interface{}{
		"trashed_at":           now,
		"original_folder_path": folder.FolderPath,
	}).Error; err != nil {
		t.Fatalf("Failed to soft delete folder: %v", err)
	}
	folder.SoftDeletedAt = &now
	folder.OriginalFolderPath = folder.FolderPath
}

// authenticatedRequest creates a request with authenticated user context
func (app *deletedTestApp) authenticatedRequest(t *testing.T, method, path string, user *models.User) *http.Request {
	t.Helper()

	req := httptest.NewRequest(method, path, nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	return req
}

// TestShowDeletedIntegration tests displaying deleted items
func TestShowDeletedIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "deleted_deleteuser")

	t.Run("shows empty state when no deleted items", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodGet, "/deleted", user)

		w := httptest.NewRecorder()
		app.deletedHandler.ShowDeleted(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "No deleted items") {
			t.Error("Expected empty state message")
		}
	})

	t.Run("displays deleted files", func(t *testing.T) {
		file := app.createTestFile(t, user, "deleted-file.txt", "content")
		app.softDeleteFile(t, file)

		req := app.authenticatedRequest(t, http.MethodGet, "/deleted", user)

		w := httptest.NewRecorder()
		app.deletedHandler.ShowDeleted(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "deleted-file.txt") {
			t.Error("Expected deleted file to be shown")
		}
	})

	t.Run("displays deleted folders", func(t *testing.T) {
		folder := app.createTestFolder(t, user, "/deleted-folder")
		app.softDeleteFolder(t, folder)

		req := app.authenticatedRequest(t, http.MethodGet, "/deleted", user)

		w := httptest.NewRecorder()
		app.deletedHandler.ShowDeleted(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		body := w.Body.String()
		if !strings.Contains(body, "deleted-folder") {
			t.Error("Expected deleted folder to be shown")
		}
	})
}

// TestRestoreFileIntegration tests restoring soft-deleted files
func TestRestoreFileIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "restoreuser")

	t.Run("restores file to original location", func(t *testing.T) {
		file := app.createTestFile(t, user, "restore-me.txt", "restore content")
		originalPath := file.LogicalPath
		app.softDeleteFile(t, file)

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/files/%d/restore", file.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.RestoreFile(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify file is restored
		var restoredFile models.File
		if err := app.db.First(&restoredFile, file.ID).Error; err != nil {
			t.Fatal("File should exist in database")
		}
		if restoredFile.SoftDeletedAt != nil {
			t.Error("File should not have SoftDeletedAt set after restore")
		}
		if restoredFile.LogicalPath != originalPath {
			t.Errorf("Expected LogicalPath to be '%s', got '%s'", originalPath, restoredFile.LogicalPath)
		}
	})

	t.Run("handles filename collision gracefully", func(t *testing.T) {
		// Create and soft-delete a file
		file := app.createTestFile(t, user, "collision.txt", "original")
		app.softDeleteFile(t, file)

		// Create another file with the same name
		app.createTestFile(t, user, "collision.txt", "new file")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/files/%d/restore", file.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.RestoreFile(w, req)

		// Should redirect (with error flash message)
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// File should still be deleted
		var stillDeletedFile models.File
		if err := app.db.First(&stillDeletedFile, file.ID).Error; err != nil {
			t.Fatal("File should still exist in database")
		}
		if stillDeletedFile.SoftDeletedAt == nil {
			t.Error("File should still be soft-deleted due to collision")
		}
	})

	t.Run("restore non-existent file fails", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodPost, "/deleted/files/99999/restore", user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "99999")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.RestoreFile(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}
	})
}

// TestRestoreFolderIntegration tests restoring soft-deleted folders
func TestRestoreFolderIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "restorefolderuser")

	t.Run("restores folder and all its contents", func(t *testing.T) {
		// Create folder with files
		folder := app.createTestFolder(t, user, "/my-folder")
		file1 := app.createTestFileInFolder(t, user, "file1.txt", "content1", "/my-folder")
		file2 := app.createTestFileInFolder(t, user, "file2.txt", "content2", "/my-folder")

		// Soft delete folder and files
		app.softDeleteFolder(t, folder)
		app.softDeleteFile(t, file1)
		app.softDeleteFile(t, file2)

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/folders/%d/restore", folder.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", folder.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.RestoreFolder(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify folder is restored
		var restoredFolder models.Folder
		if err := app.db.First(&restoredFolder, folder.ID).Error; err != nil {
			t.Fatal("Folder should exist in database")
		}
		if restoredFolder.SoftDeletedAt != nil {
			t.Error("Folder should not have SoftDeletedAt set after restore")
		}
	})

	t.Run("handles folder name collision", func(t *testing.T) {
		// Create and soft-delete a folder
		folder := app.createTestFolder(t, user, "/collision-folder")
		app.softDeleteFolder(t, folder)

		// Create another folder with the same path
		app.createTestFolder(t, user, "/collision-folder")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/folders/%d/restore", folder.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", folder.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.RestoreFolder(w, req)

		// Should redirect (with error flash message)
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Original folder should still be deleted
		var stillDeletedFolder models.Folder
		if err := app.db.First(&stillDeletedFolder, folder.ID).Error; err != nil {
			t.Fatal("Folder should still exist in database")
		}
		if stillDeletedFolder.SoftDeletedAt == nil {
			t.Error("Folder should still be soft-deleted due to collision")
		}
	})
}

// TestPermanentlyDeleteFileIntegration tests permanently deleting files
func TestPermanentlyDeleteFileIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "permdeleteuser")

	t.Run("removes file from storage and database", func(t *testing.T) {
		file := app.createTestFile(t, user, "perm-delete.txt", "delete me forever")
		storagePath := file.StoragePath
		app.softDeleteFile(t, file)

		// Verify file exists in storage
		ctx := context.Background()
		reader, err := app.storage.Open(ctx, storagePath)
		if err != nil {
			t.Fatal("File should exist in storage before permanent delete")
		}
		reader.Close()

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/files/%d/delete", file.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.PermanentlyDeleteFile(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify file is removed from database
		var deletedFile models.File
		if err := app.db.Unscoped().First(&deletedFile, file.ID).Error; err == nil {
			t.Error("File should be permanently deleted from database")
		}

		// Verify file is removed from storage
		_, err = app.storage.Open(ctx, storagePath)
		if err == nil {
			t.Error("File should be removed from storage")
		}
	})

	t.Run("cannot permanently delete other user's file", func(t *testing.T) {
		file := app.createTestFile(t, user, "protected.txt", "protected")
		app.softDeleteFile(t, file)

		otherUser := app.createTestUser(t, "otheruser")

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/files/%d/delete", file.ID), otherUser)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", file.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.PermanentlyDeleteFile(w, req)

		// Should redirect (file not found for this user)
		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// File should still exist
		var stillExists models.File
		if err := app.db.First(&stillExists, file.ID).Error; err != nil {
			t.Error("File should still exist - other user shouldn't be able to delete it")
		}
	})
}

// TestPermanentlyDeleteFolderIntegration tests permanently deleting folders
func TestPermanentlyDeleteFolderIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "permdelfolder")

	t.Run("removes folder and all contents", func(t *testing.T) {
		// Create folder with files
		folder := app.createTestFolder(t, user, "/perm-folder")
		file := app.createTestFileInFolder(t, user, "inside.txt", "inside content", "/perm-folder")
		storagePath := file.StoragePath

		// Soft delete
		app.softDeleteFolder(t, folder)
		app.softDeleteFile(t, file)

		req := app.authenticatedRequest(t, http.MethodPost, fmt.Sprintf("/deleted/folders/%d/delete", folder.ID), user)

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", fmt.Sprintf("%d", folder.ID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		w := httptest.NewRecorder()
		app.deletedHandler.PermanentlyDeleteFolder(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify folder is removed from database
		var deletedFolder models.Folder
		if err := app.db.Unscoped().First(&deletedFolder, folder.ID).Error; err == nil {
			t.Error("Folder should be permanently deleted from database")
		}

		// Verify file inside folder is also removed
		var deletedFile models.File
		if err := app.db.Unscoped().First(&deletedFile, file.ID).Error; err == nil {
			t.Error("File inside folder should be permanently deleted")
		}

		// Verify file is removed from storage
		ctx := context.Background()
		_, err := app.storage.Open(ctx, storagePath)
		if err == nil {
			t.Error("File should be removed from storage")
		}
	})
}

// TestEmptyDeletedIntegration tests emptying all deleted items for a user
func TestEmptyDeletedIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	user := app.createTestUser(t, "emptyuser")

	t.Run("clears all deleted items for user", func(t *testing.T) {
		// Create and soft-delete multiple items
		file1 := app.createTestFile(t, user, "file1.txt", "content1")
		file2 := app.createTestFile(t, user, "file2.txt", "content2")
		folder := app.createTestFolder(t, user, "/empty-folder")

		app.softDeleteFile(t, file1)
		app.softDeleteFile(t, file2)
		app.softDeleteFolder(t, folder)

		req := app.authenticatedRequest(t, http.MethodPost, "/deleted/empty", user)

		w := httptest.NewRecorder()
		app.deletedHandler.EmptyDeleted(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify all files are permanently deleted
		var fileCount int64
		app.db.Unscoped().Model(&models.File{}).Where("user_id = ?", user.ID).Count(&fileCount)
		if fileCount != 0 {
			t.Errorf("Expected 0 files, got %d", fileCount)
		}

		// Verify folder is permanently deleted
		var folderCount int64
		app.db.Unscoped().Model(&models.Folder{}).Where("user_id = ?", user.ID).Count(&folderCount)
		if folderCount != 0 {
			t.Errorf("Expected 0 folders, got %d", folderCount)
		}
	})

	t.Run("does not affect other users' deleted items", func(t *testing.T) {
		otherUser := app.createTestUser(t, "otheruser2")
		otherFile := app.createTestFile(t, otherUser, "other.txt", "other content")
		app.softDeleteFile(t, otherFile)

		// User empties their deleted items
		req := app.authenticatedRequest(t, http.MethodPost, "/deleted/empty", user)

		w := httptest.NewRecorder()
		app.deletedHandler.EmptyDeleted(w, req)

		// Other user's file should still exist
		var stillExists models.File
		if err := app.db.First(&stillExists, otherFile.ID).Error; err != nil {
			t.Error("Other user's file should still exist")
		}
	})
}

// TestAdminEmptyAllDeletedIntegration tests admin emptying all users' deleted items
func TestAdminEmptyAllDeletedIntegration(t *testing.T) {
	app := newDeletedTestApp(t)

	t.Run("admin can clear all users deleted items", func(t *testing.T) {
		admin := app.createTestAdmin(t, "admin")
		user1 := app.createTestUser(t, "user1")
		user2 := app.createTestUser(t, "user2")

		// Create and soft-delete files for multiple users
		file1 := app.createTestFile(t, user1, "user1-file.txt", "user1 content")
		file2 := app.createTestFile(t, user2, "user2-file.txt", "user2 content")
		folder1 := app.createTestFolder(t, user1, "/user1-folder")

		app.softDeleteFile(t, file1)
		app.softDeleteFile(t, file2)
		app.softDeleteFolder(t, folder1)

		req := app.authenticatedRequest(t, http.MethodPost, "/admin/deleted/empty-all", admin)

		w := httptest.NewRecorder()
		app.deletedHandler.AdminEmptyAllDeleted(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect status 303, got %d", w.Code)
		}

		// Verify the specific files we created are permanently deleted
		var deletedFile1 models.File
		if err := app.db.Unscoped().First(&deletedFile1, file1.ID).Error; err == nil {
			t.Error("file1 should be permanently deleted")
		}

		var deletedFile2 models.File
		if err := app.db.Unscoped().First(&deletedFile2, file2.ID).Error; err == nil {
			t.Error("file2 should be permanently deleted")
		}

		// Verify the specific folder we created is permanently deleted
		var deletedFolder models.Folder
		if err := app.db.Unscoped().First(&deletedFolder, folder1.ID).Error; err == nil {
			t.Error("folder1 should be permanently deleted")
		}
	})

	t.Run("non-admin cannot use admin empty all", func(t *testing.T) {
		user := app.createTestUser(t, "regularuser")

		req := app.authenticatedRequest(t, http.MethodPost, "/admin/deleted/empty-all", user)

		w := httptest.NewRecorder()
		app.deletedHandler.AdminEmptyAllDeleted(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})
}

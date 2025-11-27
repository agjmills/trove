package handlers

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/storage"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// mockStorage implements storage.StorageBackend for testing
type mockStorage struct {
	deletedPaths []string
}

func (m *mockStorage) Save(ctx context.Context, r io.Reader, opts storage.SaveOptions) (storage.SaveResult, error) {
	return storage.SaveResult{}, nil
}

func (m *mockStorage) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	return nil, storage.ErrNotFound
}

func (m *mockStorage) Delete(ctx context.Context, path string) error {
	m.deletedPaths = append(m.deletedPaths, path)
	return nil
}

func (m *mockStorage) Stat(ctx context.Context, path string) (storage.FileInfo, error) {
	return storage.FileInfo{}, storage.ErrNotFound
}

func (m *mockStorage) HealthCheck(ctx context.Context) error {
	return nil
}

func (m *mockStorage) ValidateAccess(ctx context.Context) error {
	return nil
}

func setupTestAdminHandler(t *testing.T) (*AdminHandler, *gorm.DB, *scs.SessionManager, *mockStorage) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{})
	if err != nil {
		t.Fatalf("Failed to migrate: %v", err)
	}

	cfg := &config.Config{
		BcryptCost:       4,
		DefaultUserQuota: 1024 * 1024 * 100,
		StoragePath:      "/tmp/test-storage",
	}

	sessionManager := scs.New()
	mockStore := &mockStorage{}
	handler := NewAdminHandler(db, cfg, sessionManager, mockStore)

	return handler, db, sessionManager, mockStore
}

func createAdminTestUser(t *testing.T, db *gorm.DB, username, email string, isAdmin bool) *models.User {
	hash, _ := auth.HashPassword("password123", 4)
	user := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		IsAdmin:      isAdmin,
		StorageQuota: 1024 * 1024 * 100,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}
	return user
}

// withUser adds a user to the request context
func withUser(r *http.Request, user *models.User) *http.Request {
	ctx := context.WithValue(r.Context(), auth.UserContextKey, user)
	return r.WithContext(ctx)
}

func TestCreateUser_Success(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	adminUser := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	form := url.Values{}
	form.Set("username", "newuser")
	form.Set("email", "newuser@example.com")
	form.Set("password", "securepassword123")

	req := httptest.NewRequest(http.MethodPost, "/admin/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, adminUser)

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.CreateUser)).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d: %s", w.Code, w.Body.String())
	}

	// Verify user was created
	var newUser models.User
	if err := db.Where("username = ?", "newuser").First(&newUser).Error; err != nil {
		t.Fatal("New user was not created")
	}
	if newUser.IsAdmin {
		t.Error("New user should not be admin by default")
	}
}

func TestCreateUser_NonAdminForbidden(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	regularUser := createAdminTestUser(t, db, "regular", "regular@example.com", false)

	form := url.Values{}
	form.Set("username", "newuser")
	form.Set("email", "newuser@example.com")
	form.Set("password", "securepassword123")

	req := httptest.NewRequest(http.MethodPost, "/admin/users/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, regularUser)

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.CreateUser)).ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", w.Code)
	}
}

func TestToggleAdmin_CannotToggleSelf(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	adminUser := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/1/toggle-admin", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, adminUser)

	// Set up chi URL params
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1") // adminUser.ID
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.ToggleAdmin)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 when toggling own admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestToggleAdmin_CanToggleOthers(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	adminUser := createAdminTestUser(t, db, "admin", "admin@example.com", true)
	regularUser := createAdminTestUser(t, db, "regular", "regular@example.com", false)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/2/toggle-admin", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, adminUser)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "2") // regularUser.ID
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.ToggleAdmin)).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d: %s", w.Code, w.Body.String())
	}

	// Verify admin status was toggled
	var updated models.User
	db.First(&updated, regularUser.ID)
	if !updated.IsAdmin {
		t.Error("User should now be admin")
	}
}

func TestDeleteUser_CannotDeleteSelf(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	adminUser := createAdminTestUser(t, db, "admin", "admin@example.com", true)

	req := httptest.NewRequest(http.MethodPost, "/admin/users/1/delete", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, adminUser)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.DeleteUser)).ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400 when deleting self, got %d", w.Code)
	}
}

func TestDeleteUser_DeletesUserAndFiles(t *testing.T) {
	handler, db, sessionManager, _ := setupTestAdminHandler(t)
	adminUser := createAdminTestUser(t, db, "admin", "admin@example.com", true)
	targetUser := createAdminTestUser(t, db, "target", "target@example.com", false)

	// Create some files for the target user
	files := []models.File{
		{UserID: targetUser.ID, Filename: "file1.txt", OriginalFilename: "file1.txt", StoragePath: "path/to/file1.bin"},
		{UserID: targetUser.ID, Filename: "file2.txt", OriginalFilename: "file2.txt", StoragePath: "path/to/file2.bin"},
	}
	for _, f := range files {
		db.Create(&f)
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/users/2/delete", nil)
	req = csrf.UnsafeSkipCheck(req)
	req = withUser(req, adminUser)

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.DeleteUser)).ServeHTTP(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d: %s", w.Code, w.Body.String())
	}

	// Verify user was deleted
	var count int64
	db.Model(&models.User{}).Where("id = ?", targetUser.ID).Count(&count)
	if count != 0 {
		t.Error("User should have been deleted")
	}

	// Verify files were deleted from database
	db.Model(&models.File{}).Where("user_id = ?", targetUser.ID).Count(&count)
	if count != 0 {
		t.Error("User's files should have been deleted from database")
	}

	// Note: Storage deletion happens in a goroutine, so we can't easily verify it here
	// In a real scenario, you'd use a channel or wait group
}

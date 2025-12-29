package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agjmills/trove/internal/database/models"
	"github.com/alexedwards/scs/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Auto-migrate the User model
	if err := db.AutoMigrate(&models.User{}); err != nil {
		t.Fatalf("Failed to migrate User model: %v", err)
	}

	return db
}

func setupTestSessionManager(t *testing.T) *scs.SessionManager {
	t.Helper()

	sm := scs.New()
	sm.Cookie.Name = "test_session"
	return sm
}

func TestRequireAuth_Authenticated(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	// Create test user
	user := &models.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashedpassword",
		StorageQuota: 1000000,
		IsAdmin:      false,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	// Create a handler that checks if user is in context
	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		contextUser := GetUser(r)
		if contextUser == nil {
			t.Error("User should be in context")
			return
		}
		if contextUser.ID != user.ID {
			t.Errorf("Expected user ID %d, got %d", user.ID, contextUser.ID)
		}
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with auth middleware and session middleware
	authHandler := sm.LoadAndSave(RequireAuth(db, sm)(handler))

	// Create request with session
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	
	// Set user ID in session via middleware
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", int(user.ID))
		authHandler.ServeHTTP(w, r)
	})).ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Handler should have been called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestRequireAuth_NoSession(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without authentication")
	})

	authHandler := sm.LoadAndSave(RequireAuth(db, sm)(handler))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()

	authHandler.ServeHTTP(rec, req)

	// Should redirect to login
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

func TestRequireAuth_InvalidUser(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with invalid user")
	})

	authHandler := sm.LoadAndSave(RequireAuth(db, sm)(handler))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rec := httptest.NewRecorder()
	
	// Set invalid user ID in session
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", 99999) // Non-existent user ID
		authHandler.ServeHTTP(w, r)
	})).ServeHTTP(rec, req)

	// Should redirect to login
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

func TestGetUser(t *testing.T) {
	// Test with user in context
	user := &models.User{
		ID:           1,
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashedpassword",
	}

	ctx := context.WithValue(context.Background(), UserContextKey, user)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(ctx)

	retrievedUser := GetUser(req)
	if retrievedUser == nil {
		t.Fatal("GetUser should return user from context")
	}

	if retrievedUser.ID != user.ID {
		t.Errorf("Expected user ID %d, got %d", user.ID, retrievedUser.ID)
	}
}

func TestGetUser_NoUser(t *testing.T) {
	// Test without user in context
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	user := GetUser(req)
	if user != nil {
		t.Error("GetUser should return nil when no user in context")
	}
}

func TestOptionalAuth_WithUser(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	// Create test user
	user := &models.User{
		Username:     "testuser",
		Email:        "test@example.com",
		PasswordHash: "hashedpassword",
		StorageQuota: 1000000,
		IsAdmin:      false,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		contextUser := GetUser(r)
		if contextUser == nil {
			t.Error("User should be in context")
			return
		}
		if contextUser.ID != user.ID {
			t.Errorf("Expected user ID %d, got %d", user.ID, contextUser.ID)
		}
		w.WriteHeader(http.StatusOK)
	})

	authHandler := sm.LoadAndSave(OptionalAuth(db, sm)(handler))

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	rec := httptest.NewRecorder()

	// Set user ID in session
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", int(user.ID))
		authHandler.ServeHTTP(w, r)
	})).ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Handler should have been called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestOptionalAuth_WithoutUser(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		user := GetUser(r)
		if user != nil {
			t.Error("User should not be in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	authHandler := sm.LoadAndSave(OptionalAuth(db, sm)(handler))

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	rec := httptest.NewRecorder()

	authHandler.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Handler should have been called even without authentication")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

func TestOptionalAuth_InvalidUser(t *testing.T) {
	db := setupTestDB(t)
	sm := setupTestSessionManager(t)

	handlerCalled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		user := GetUser(r)
		if user != nil {
			t.Error("Invalid user should not be in context")
		}
		w.WriteHeader(http.StatusOK)
	})

	authHandler := sm.LoadAndSave(OptionalAuth(db, sm)(handler))

	req := httptest.NewRequest(http.MethodGet, "/page", nil)
	rec := httptest.NewRecorder()

	// Set invalid user ID in session
	sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sm.Put(r.Context(), "user_id", 99999) // Non-existent user
		authHandler.ServeHTTP(w, r)
	})).ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("Handler should have been called even with invalid user")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

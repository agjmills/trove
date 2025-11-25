package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestAuthHandler(t *testing.T) (*AuthHandler, *gorm.DB, *scs.SessionManager) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		BcryptCost: 10,
	}

	sessionManager := scs.New()

	handler := NewAuthHandler(db, cfg, sessionManager)

	return handler, db, sessionManager
}

func createTestUser(t *testing.T, db *gorm.DB, username, email, password string) *models.User {
	hashedPassword, err := auth.HashPassword(password, 10)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: hashedPassword,
		StorageQuota: 1024 * 1024 * 100,
		StorageUsed:  0,
	}

	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

func TestChangePassword_Success(t *testing.T) {
	handler, db, _ := setupTestAuthHandler(t)

	// Create test user with password "oldpassword123"
	user := createTestUser(t, db, "testuser", "test@example.com", "oldpassword123")

	// Prepare request
	reqBody := ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "newpassword456",
		ConfirmPassword: "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	// Add user to context
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)

	// Bypass CSRF for testing
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()

	// Call handler
	handler.ChangePassword(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]string
	json.Unmarshal(w.Body.Bytes(), &response)

	if response["message"] != "Password changed successfully" {
		t.Errorf("Expected success message, got: %s", response["message"])
	}

	// Verify password was actually changed in database
	var updatedUser models.User
	db.First(&updatedUser, user.ID)

	if !auth.VerifyPassword(updatedUser.PasswordHash, "newpassword456") {
		t.Error("New password does not match in database")
	}

	if auth.VerifyPassword(updatedUser.PasswordHash, "oldpassword123") {
		t.Error("Old password still works - password was not changed")
	}
}

func TestChangePassword_WrongCurrentPassword(t *testing.T) {
	handler, db, _ := setupTestAuthHandler(t)

	user := createTestUser(t, db, "testuser", "test@example.com", "oldpassword123")

	reqBody := ChangePasswordRequest{
		CurrentPassword: "wrongpassword",
		NewPassword:     "newpassword456",
		ConfirmPassword: "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ChangePassword(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}

	errorMessage := strings.TrimSpace(w.Body.String())
	if !strings.Contains(errorMessage, "Current password is incorrect") {
		t.Errorf("Expected 'Current password is incorrect' in error, got: %s", errorMessage)
	}

	// Verify password was NOT changed
	var unchangedUser models.User
	db.First(&unchangedUser, user.ID)

	if !auth.VerifyPassword(unchangedUser.PasswordHash, "oldpassword123") {
		t.Error("Original password no longer works")
	}
}

func TestChangePassword_PasswordMismatch(t *testing.T) {
	handler, db, _ := setupTestAuthHandler(t)

	user := createTestUser(t, db, "testuser", "test@example.com", "oldpassword123")

	reqBody := ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "newpassword456",
		ConfirmPassword: "differentpassword",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ChangePassword(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	errorMessage := strings.TrimSpace(w.Body.String())
	if !strings.Contains(errorMessage, "New passwords do not match") {
		t.Errorf("Expected 'New passwords do not match' in error, got: %s", errorMessage)
	}

	// Verify password was NOT changed
	var unchangedUser models.User
	db.First(&unchangedUser, user.ID)

	if !auth.VerifyPassword(unchangedUser.PasswordHash, "oldpassword123") {
		t.Error("Original password no longer works")
	}
}

func TestChangePassword_TooShort(t *testing.T) {
	handler, db, _ := setupTestAuthHandler(t)

	user := createTestUser(t, db, "testuser", "test@example.com", "oldpassword123")

	reqBody := ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "short",
		ConfirmPassword: "short",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ChangePassword(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}

	errorMessage := strings.TrimSpace(w.Body.String())
	if !strings.Contains(errorMessage, "at least 8 characters") {
		t.Errorf("Expected 'at least 8 characters' in error, got: %s", errorMessage)
	}

	// Verify password was NOT changed
	var unchangedUser models.User
	db.First(&unchangedUser, user.ID)

	if !auth.VerifyPassword(unchangedUser.PasswordHash, "oldpassword123") {
		t.Error("Original password no longer works")
	}
}

func TestChangePassword_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestAuthHandler(t)

	reqBody := ChangePasswordRequest{
		CurrentPassword: "oldpassword123",
		NewPassword:     "newpassword456",
		ConfirmPassword: "newpassword456",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/settings/change-password", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ChangePassword(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Code)
	}
}

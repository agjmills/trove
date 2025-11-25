package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/alexedwards/scs/v2"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestAuthHandlerWithConfig(t *testing.T, cfg *config.Config) (*AuthHandler, *gorm.DB, *scs.SessionManager) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	sessionManager := scs.New()

	handler := NewAuthHandler(db, cfg, sessionManager)

	return handler, db, sessionManager
}

func TestShowRegister_DisabledRegistration(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: false,
	}
	handler, _, _ := setupTestAuthHandlerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.ShowRegister(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected 404 when registration disabled, got %d", w.Code)
	}
}

func TestRegister_EnabledRegistration_JSON(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: true,
		BcryptCost:         4, // Low cost for faster tests
		DefaultUserQuota:   1024 * 1024 * 100,
	}
	handler, db, sessionManager := setupTestAuthHandlerWithConfig(t, cfg)

	reqBody := RegisterRequest{
		Username: "newuser",
		Email:    "newuser@example.com",
		Password: "securepassword123",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = csrf.UnsafeSkipCheck(req)

	// Wrap with session manager
	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.Register)).ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	// Verify user was created in database
	var user models.User
	if err := db.Where("username = ?", "newuser").First(&user).Error; err != nil {
		t.Errorf("User was not created in database: %v", err)
	}

	if user.Email != "newuser@example.com" {
		t.Errorf("Expected email 'newuser@example.com', got '%s'", user.Email)
	}
}

func TestRegister_DisabledRegistration_JSON(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: false,
	}
	handler, db, _ := setupTestAuthHandlerWithConfig(t, cfg)

	reqBody := RegisterRequest{
		Username: "newuser",
		Email:    "newuser@example.com",
		Password: "securepassword123",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.Register(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 when registration disabled, got %d", w.Code)
	}

	// Verify response message
	if !strings.Contains(w.Body.String(), "Registration is disabled") {
		t.Errorf("Expected 'Registration is disabled' error, got: %s", w.Body.String())
	}

	// Verify user was NOT created
	var count int64
	db.Model(&models.User{}).Where("username = ?", "newuser").Count(&count)
	if count != 0 {
		t.Error("User should not have been created when registration is disabled")
	}
}

func TestRegister_DisabledRegistration_Form(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: false,
	}
	handler, db, _ := setupTestAuthHandlerWithConfig(t, cfg)

	form := url.Values{}
	form.Set("username", "newuser")
	form.Set("email", "newuser@example.com")
	form.Set("password", "securepassword123")

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	handler.Register(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 when registration disabled, got %d", w.Code)
	}

	// Verify user was NOT created
	var count int64
	db.Model(&models.User{}).Where("username = ?", "newuser").Count(&count)
	if count != 0 {
		t.Error("User should not have been created when registration is disabled")
	}
}

func TestRegister_EnabledRegistration_Form(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: true,
		BcryptCost:         4,
		DefaultUserQuota:   1024 * 1024 * 100,
	}
	handler, db, sessionManager := setupTestAuthHandlerWithConfig(t, cfg)

	form := url.Values{}
	form.Set("username", "formuser")
	form.Set("email", "formuser@example.com")
	form.Set("password", "securepassword123")

	req := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	sessionManager.LoadAndSave(http.HandlerFunc(handler.Register)).ServeHTTP(w, req)

	// Form submission should redirect on success
	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303 redirect, got %d: %s", w.Code, w.Body.String())
	}

	// Verify user was created
	var user models.User
	if err := db.Where("username = ?", "formuser").First(&user).Error; err != nil {
		t.Errorf("User was not created in database: %v", err)
	}
}

func TestRegister_MissingFields(t *testing.T) {
	cfg := &config.Config{
		EnableRegistration: true,
	}
	handler, _, _ := setupTestAuthHandlerWithConfig(t, cfg)

	tests := []struct {
		name     string
		username string
		email    string
		password string
	}{
		{"missing username", "", "test@example.com", "password123"},
		{"missing email", "testuser", "", "password123"},
		{"missing password", "testuser", "test@example.com", ""},
		{"all empty", "", "", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reqBody := RegisterRequest{
				Username: tc.username,
				Email:    tc.email,
				Password: tc.password,
			}
			body, _ := json.Marshal(reqBody)

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = csrf.UnsafeSkipCheck(req)

			w := httptest.NewRecorder()
			handler.Register(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status 400 for %s, got %d", tc.name, w.Code)
			}
		})
	}
}

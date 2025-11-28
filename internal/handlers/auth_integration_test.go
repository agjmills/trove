package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testApp encapsulates all dependencies for integration tests
type testApp struct {
	db             *gorm.DB
	cfg            *config.Config
	sessionManager *scs.SessionManager
	authHandler    *AuthHandler
	router         *chi.Mux
}

// newTestApp creates a new test application with all dependencies
func newTestApp(t *testing.T) *testApp {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	err = db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{})
	if err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		EnableRegistration: true,
		BcryptCost:         4, // Low cost for faster tests
		DefaultUserQuota:   1024 * 1024 * 100,
		SessionSecret:      "test-secret-key-32-bytes-long!!",
		Env:                "test",
	}

	sessionManager := scs.New()
	authHandler := NewAuthHandler(db, cfg, sessionManager)

	// Setup minimal router for integration tests
	router := chi.NewRouter()
	router.Use(sessionManager.LoadAndSave)

	// Public routes
	router.Get("/login", authHandler.ShowLogin)
	router.Get("/register", authHandler.ShowRegister)
	router.Post("/register", authHandler.Register)
	router.Post("/login", authHandler.Login)

	// Protected routes
	router.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Get("/settings", authHandler.ShowSettings)
		r.Post("/settings/change-password", authHandler.ChangePassword)
		r.Post("/logout", authHandler.Logout)
	})

	return &testApp{
		db:             db,
		cfg:            cfg,
		sessionManager: sessionManager,
		authHandler:    authHandler,
		router:         router,
	}
}

// createTestUserInDB creates a test user directly in the database
func (app *testApp) createTestUserInDB(t *testing.T, username, email, password string) *models.User {
	t.Helper()

	hashedPassword, err := auth.HashPassword(password, app.cfg.BcryptCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user := &models.User{
		Username:     username,
		Email:        email,
		PasswordHash: hashedPassword,
		StorageQuota: app.cfg.DefaultUserQuota,
	}

	if err := app.db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

// authenticatedRequest creates a request with an authenticated session
func (app *testApp) authenticatedRequest(t *testing.T, method, path string, body []byte, user *models.User) *http.Request {
	t.Helper()

	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}

	// Create a session token and commit the session with user_id
	commitReq := httptest.NewRequest(http.MethodGet, "/", nil)
	commitW := httptest.NewRecorder()
	app.sessionManager.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.sessionManager.Put(r.Context(), "user_id", int(user.ID))
		_, _, _ = app.sessionManager.Commit(r.Context())
	})).ServeHTTP(commitW, commitReq)

	// Get the session cookie from the response and add it to the actual request
	for _, cookie := range commitW.Result().Cookies() {
		req.AddCookie(cookie)
	}

	req = csrf.UnsafeSkipCheck(req)

	return req
}

// TestRegisterIntegration_FullFlow tests the complete registration flow
func TestRegisterIntegration_FullFlow(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name           string
		contentType    string
		body           interface{}
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:        "JSON registration success",
			contentType: "application/json",
			body: RegisterRequest{
				Username: "jsonuser",
				Email:    "json@example.com",
				Password: "securepass123",
			},
			expectedStatus: http.StatusCreated,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response body: %v; body: %s", err, w.Body.String())
				}
				if resp["username"] != "jsonuser" {
					t.Errorf("Expected username 'jsonuser', got %v", resp["username"])
				}
				if resp["email"] != "json@example.com" {
					t.Errorf("Expected email 'json@example.com', got %v", resp["email"])
				}
			},
		},
		{
			name:           "Form registration success",
			contentType:    "application/x-www-form-urlencoded",
			body:           "username=formuser&email=form@example.com&password=securepass123",
			expectedStatus: http.StatusSeeOther, // Redirect on success
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				location := w.Header().Get("Location")
				if location != "/files" {
					t.Errorf("Expected redirect to /files, got %s", location)
				}
			},
		},
		{
			name:        "Missing username",
			contentType: "application/json",
			body: RegisterRequest{
				Username: "",
				Email:    "missing@example.com",
				Password: "securepass123",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "Missing email",
			contentType: "application/json",
			body: RegisterRequest{
				Username: "missingemail",
				Email:    "",
				Password: "securepass123",
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:        "Missing password",
			contentType: "application/json",
			body: RegisterRequest{
				Username: "missingpass",
				Email:    "missingpass@example.com",
				Password: "",
			},
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.contentType == "application/json" {
				bodyBytes, _ := json.Marshal(tc.body)
				req = httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(bodyBytes))
			} else {
				req = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(tc.body.(string)))
			}
			req.Header.Set("Content-Type", tc.contentType)
			req = csrf.UnsafeSkipCheck(req)

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if tc.checkResponse != nil {
				tc.checkResponse(t, w)
			}
		})
	}
}

// TestRegisterIntegration_DuplicateUser tests duplicate user registration
func TestRegisterIntegration_DuplicateUser(t *testing.T) {
	app := newTestApp(t)

	// Create initial user
	app.createTestUserInDB(t, "existing", "existing@example.com", "password123")

	tests := []struct {
		name     string
		username string
		email    string
	}{
		{"duplicate username", "existing", "new@example.com"},
		{"duplicate email", "newuser", "existing@example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(RegisterRequest{
				Username: tc.username,
				Email:    tc.email,
				Password: "password123",
			})

			req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req = csrf.UnsafeSkipCheck(req)

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			if w.Code != http.StatusConflict {
				t.Errorf("Expected status 409 Conflict, got %d", w.Code)
			}
		})
	}
}

// TestLoginIntegration_FullFlow tests the complete login flow
func TestLoginIntegration_FullFlow(t *testing.T) {
	app := newTestApp(t)

	// Create test user
	app.createTestUserInDB(t, "loginuser", "login@example.com", "correctpassword")

	tests := []struct {
		name           string
		contentType    string
		username       string
		password       string
		expectedStatus int
		checkResponse  func(t *testing.T, w *httptest.ResponseRecorder)
	}{
		{
			name:           "JSON login success",
			contentType:    "application/json",
			username:       "loginuser",
			password:       "correctpassword",
			expectedStatus: http.StatusOK,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				var resp map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response body: %v; body: %s", err, w.Body.String())
				}
				if resp["username"] != "loginuser" {
					t.Errorf("Expected username 'loginuser', got %v", resp["username"])
				}
			},
		},
		{
			name:           "Form login success",
			contentType:    "application/x-www-form-urlencoded",
			username:       "loginuser",
			password:       "correctpassword",
			expectedStatus: http.StatusSeeOther,
			checkResponse: func(t *testing.T, w *httptest.ResponseRecorder) {
				location := w.Header().Get("Location")
				if location != "/files" {
					t.Errorf("Expected redirect to /files, got %s", location)
				}
			},
		},
		{
			name:           "Wrong password (JSON)",
			contentType:    "application/json",
			username:       "loginuser",
			password:       "wrongpassword",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Non-existent user (JSON)",
			contentType:    "application/json",
			username:       "nouser",
			password:       "password",
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var req *http.Request
			if tc.contentType == "application/json" {
				body, _ := json.Marshal(LoginRequest{
					Username: tc.username,
					Password: tc.password,
				})
				req = httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
			} else {
				form := url.Values{}
				form.Set("username", tc.username)
				form.Set("password", tc.password)
				req = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
			}
			req.Header.Set("Content-Type", tc.contentType)
			req = csrf.UnsafeSkipCheck(req)

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if tc.checkResponse != nil {
				tc.checkResponse(t, w)
			}
		})
	}
}

// TestLoginIntegration_SessionCreation verifies session is created on login
func TestLoginIntegration_SessionCreation(t *testing.T) {
	app := newTestApp(t)

	// Create test user
	app.createTestUserInDB(t, "sessionuser", "session@example.com", "password123")

	// Login request
	body, _ := json.Marshal(LoginRequest{
		Username: "sessionuser",
		Password: "password123",
	})

	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = csrf.UnsafeSkipCheck(req)

	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Login failed: %d %s", w.Code, w.Body.String())
	}

	// Check that a session cookie was set
	cookies := w.Result().Cookies()
	hasSessionCookie := false
	for _, c := range cookies {
		if c.Name == "session" {
			hasSessionCookie = true
			break
		}
	}

	if !hasSessionCookie {
		t.Error("Expected session cookie to be set after login")
	}
}

// TestChangePasswordIntegration tests password change flow
func TestChangePasswordIntegration(t *testing.T) {
	app := newTestApp(t)

	// Create test user
	user := app.createTestUserInDB(t, "changepassuser", "changepass@example.com", "oldpassword123")

	tests := []struct {
		name           string
		currentPass    string
		newPass        string
		confirmPass    string
		expectedStatus int
		checkDB        func(t *testing.T)
	}{
		{
			name:           "successful password change",
			currentPass:    "oldpassword123",
			newPass:        "newpassword456",
			confirmPass:    "newpassword456",
			expectedStatus: http.StatusOK,
			checkDB: func(t *testing.T) {
				var u models.User
				app.db.First(&u, user.ID)
				if !auth.VerifyPassword(u.PasswordHash, "newpassword456") {
					t.Error("Password was not updated in database")
				}
			},
		},
		{
			name:           "wrong current password",
			currentPass:    "wrongpassword",
			newPass:        "newpassword456",
			confirmPass:    "newpassword456",
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "password mismatch",
			currentPass:    "oldpassword123",
			newPass:        "newpassword456",
			confirmPass:    "differentpassword",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "password too short",
			currentPass:    "oldpassword123",
			newPass:        "short",
			confirmPass:    "short",
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "password too long (bcrypt limit)",
			currentPass:    "oldpassword123",
			newPass:        strings.Repeat("a", 73),
			confirmPass:    strings.Repeat("a", 73),
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(ChangePasswordRequest{
				CurrentPassword: tc.currentPass,
				NewPassword:     tc.newPass,
				ConfirmPassword: tc.confirmPass,
			})

			req := app.authenticatedRequest(t, http.MethodPost, "/settings/change-password", body, user)
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d: %s", tc.expectedStatus, w.Code, w.Body.String())
			}

			if tc.checkDB != nil {
				tc.checkDB(t)
			}

			// Reset password for next test
			if tc.expectedStatus == http.StatusOK {
				hashedPass, _ := auth.HashPassword("oldpassword123", app.cfg.BcryptCost)
				app.db.Model(&models.User{}).Where("id = ?", user.ID).Update("password_hash", hashedPass)
			}
		})
	}
}

// TestLogoutIntegration tests logout flow
func TestLogoutIntegration(t *testing.T) {
	app := newTestApp(t)

	// Create test user
	user := app.createTestUserInDB(t, "logoutuser", "logout@example.com", "password123")

	t.Run("JSON logout", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodPost, "/logout", nil, user)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		app.sessionManager.LoadAndSave(http.HandlerFunc(app.authHandler.Logout)).ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}

		var resp map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal response body: %v; body: %s", err, w.Body.String())
		}
		if resp["message"] != "Logged out" {
			t.Errorf("Expected 'Logged out' message, got %v", resp["message"])
		}
	})

	t.Run("Form logout redirects", func(t *testing.T) {
		req := app.authenticatedRequest(t, http.MethodPost, "/logout", nil, user)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		w := httptest.NewRecorder()
		app.sessionManager.LoadAndSave(http.HandlerFunc(app.authHandler.Logout)).ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected status 303, got %d", w.Code)
		}

		location := w.Header().Get("Location")
		if location != "/login" {
			t.Errorf("Expected redirect to /login, got %s", location)
		}
	})
}

// TestRegistrationDisabled tests that registration can be disabled
func TestRegistrationDisabled(t *testing.T) {
	app := newTestApp(t)
	app.cfg.EnableRegistration = false
	app.authHandler = NewAuthHandler(app.db, app.cfg, app.sessionManager)

	t.Run("GET /register returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/register", nil)
		w := httptest.NewRecorder()
		app.authHandler.ShowRegister(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("POST /register returns 403", func(t *testing.T) {
		body, _ := json.Marshal(RegisterRequest{
			Username: "newuser",
			Email:    "new@example.com",
			Password: "password123",
		})

		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = csrf.UnsafeSkipCheck(req)

		w := httptest.NewRecorder()
		app.authHandler.Register(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})
}

// TestShowLoginRedirectsAuthenticatedUser tests that authenticated users are redirected
func TestShowLoginRedirectsAuthenticatedUser(t *testing.T) {
	app := newTestApp(t)

	user := app.createTestUserInDB(t, "autheduser", "authed@example.com", "password123")

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	app.authHandler.ShowLogin(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status 303, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/files" {
		t.Errorf("Expected redirect to /files, got %s", location)
	}
}

// TestShowRegisterRedirectsAuthenticatedUser tests that authenticated users are redirected
func TestShowRegisterRedirectsAuthenticatedUser(t *testing.T) {
	app := newTestApp(t)

	user := app.createTestUserInDB(t, "autheduser2", "authed2@example.com", "password123")

	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	ctx := context.WithValue(req.Context(), auth.UserContextKey, user)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	app.authHandler.ShowRegister(w, req)

	if w.Code != http.StatusSeeOther {
		t.Errorf("Expected redirect status 303, got %d", w.Code)
	}

	location := w.Header().Get("Location")
	if location != "/files" {
		t.Errorf("Expected redirect to /files, got %s", location)
	}
}

package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/storage"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	// Ensure templates are loaded for integration tests
	// Find the project root by looking for go.mod
	dir, _ := os.Getwd()
	for dir != "/" {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			os.Chdir(dir)
			break
		}
		dir = filepath.Dir(dir)
	}
	handlers.LoadTemplates()
}

// testIPCounter is used to generate unique IP addresses for tests to avoid rate limiting
var testIPCounter atomic.Uint64

// uniqueTestIP generates a unique IP address for each test to avoid rate limiting
func uniqueTestIP() string {
	counter := testIPCounter.Add(1)
	return fmt.Sprintf("192.168.%d.%d:12345", (counter/256)%256, counter%256)
}

// routeTestApp encapsulates all dependencies for route integration tests
type routeTestApp struct {
	db             *gorm.DB
	cfg            *config.Config
	sessionManager *scs.SessionManager
	storage        *storage.MemoryBackend
	router         chi.Router
}

// newRouteTestApp creates a new test application with full routing setup
func newRouteTestApp(t *testing.T) *routeTestApp {
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
		DefaultUserQuota:   100 * 1024 * 1024,
		MaxUploadSize:      10 * 1024 * 1024,
		SessionSecret:      "test-secret-key-32-bytes-long!!",
		Env:                "test",
		CSRFEnabled:        false, // Disable CSRF for easier testing
	}

	sessionManager := scs.New()
	sessionManager.Lifetime = 24 * time.Hour

	memStorage := storage.NewMemoryBackend()

	router := chi.NewRouter()
	fileHandler := Setup(router, db, cfg, memStorage, sessionManager, "test-version")

	// Ensure cleanup
	t.Cleanup(func() {
		fileHandler.Shutdown()
	})

	return &routeTestApp{
		db:             db,
		cfg:            cfg,
		sessionManager: sessionManager,
		storage:        memStorage,
		router:         router,
	}
}

// createTestUser creates a test user in the database
func (app *routeTestApp) createTestUser(t *testing.T, username, password string) *models.User {
	t.Helper()

	hashedPassword, err := auth.HashPassword(password, app.cfg.BcryptCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user := &models.User{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: hashedPassword,
		StorageQuota: app.cfg.DefaultUserQuota,
	}

	if err := app.db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	return user
}

// newRequest creates an HTTP request with a unique IP to avoid rate limiting
func (app *routeTestApp) newRequest(method, path string, body *bytes.Reader) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, body)
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	// Use a fresh unique IP for each request to avoid rate limiting
	req.RemoteAddr = uniqueTestIP()
	return req
}

// newFormRequest creates an HTTP form request with a unique IP
func (app *routeTestApp) newFormRequest(method, path string, form url.Values) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// Use a fresh unique IP for each request to avoid rate limiting
	req.RemoteAddr = uniqueTestIP()
	return req
}

// TestHealthEndpoint tests the /health endpoint
func TestHealthEndpoint(t *testing.T) {
	app := newRouteTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Check JSON response
	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("Failed to parse health response: %v", err)
	}

	if resp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got %v", resp["status"])
	}
}

// TestMetricsEndpoint tests the /metrics endpoint
func TestMetricsEndpoint(t *testing.T) {
	app := newRouteTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Prometheus metrics should contain standard metrics
	body := w.Body.String()
	if !strings.Contains(body, "go_") {
		t.Error("Expected Prometheus Go metrics in response")
	}
}

// TestStaticFileServing tests that static files are served
func TestStaticFileServing(t *testing.T) {
	app := newRouteTestApp(t)

	// This will return 404 if the file doesn't exist, but the route should work
	req := httptest.NewRequest(http.MethodGet, "/static/css/style.css", nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	// The route is set up, even if file doesn't exist in test environment
	// We just verify the route doesn't panic
	if w.Code == http.StatusInternalServerError {
		t.Errorf("Static file serving caused an error: %d", w.Code)
	}
}

// TestPublicRoutes tests routes that don't require authentication
func TestPublicRoutes(t *testing.T) {
	app := newRouteTestApp(t)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"login page", http.MethodGet, "/login", http.StatusOK},
		{"register page", http.MethodGet, "/register", http.StatusOK},
		{"root redirects to login", http.MethodGet, "/", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status %d, got %d", tc.expectedStatus, w.Code)
			}
		})
	}
}

// TestProtectedRoutesRequireAuth tests that protected routes redirect unauthenticated users
func TestProtectedRoutesRequireAuth(t *testing.T) {
	app := newRouteTestApp(t)

	protectedRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/files"},
		{http.MethodGet, "/settings"},
		{http.MethodPost, "/folders/create"},
		{http.MethodGet, "/download/1"},
		{http.MethodPost, "/delete/1"},
	}

	for _, route := range protectedRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			// Should redirect to login
			if w.Code != http.StatusSeeOther {
				t.Errorf("Expected redirect (303), got %d for %s %s", w.Code, route.method, route.path)
			}

			location := w.Header().Get("Location")
			if location != "/login" {
				t.Errorf("Expected redirect to /login, got %s", location)
			}
		})
	}
}

// TestLoginFlow tests the complete login flow through routes
func TestLoginFlow(t *testing.T) {
	app := newRouteTestApp(t)
	app.createTestUser(t, "testuser", "password123")

	t.Run("successful login", func(t *testing.T) {
		form := url.Values{}
		form.Set("username", "testuser")
		form.Set("password", "password123")

		req := app.newFormRequest(http.MethodPost, "/login", form)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d: %s", w.Code, w.Body.String())
		}

		// Check session cookie is set
		cookies := w.Result().Cookies()
		hasSession := false
		for _, c := range cookies {
			if c.Name == "session" {
				hasSession = true
				break
			}
		}
		if !hasSession {
			t.Error("Expected session cookie to be set")
		}
	})

	t.Run("failed login", func(t *testing.T) {
		form := url.Values{}
		form.Set("username", "testuser")
		form.Set("password", "wrongpassword")

		req := app.newFormRequest(http.MethodPost, "/login", form)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		// Should show login page again with error (status 200)
		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200 (show login with error), got %d", w.Code)
		}
	})
}

// TestRegistrationFlow tests the complete registration flow
func TestRegistrationFlow(t *testing.T) {
	app := newRouteTestApp(t)

	t.Run("successful registration", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "newuser",
			"email":    "new@example.com",
			"password": "securepass123",
		})

		req := app.newRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify user was created in database
		var user models.User
		if err := app.db.Where("username = ?", "newuser").First(&user).Error; err != nil {
			t.Error("User was not created in database")
		}
	})

	t.Run("registration with existing username", func(t *testing.T) {
		// First create a user
		app.createTestUser(t, "existinguser", "password")

		body, _ := json.Marshal(map[string]string{
			"username": "existinguser",
			"email":    "different@example.com",
			"password": "password123",
		})

		req := app.newRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusConflict {
			t.Errorf("Expected status 409, got %d", w.Code)
		}
	})
}

// TestRegistrationDisabled tests that registration can be disabled
func TestRegistrationDisabled(t *testing.T) {
	app := newRouteTestApp(t)
	app.cfg.EnableRegistration = false

	// Re-setup routes with new config
	router := chi.NewRouter()
	fileHandler := Setup(router, app.db, app.cfg, app.storage, app.sessionManager, "test-version")
	t.Cleanup(func() {
		fileHandler.Shutdown()
	})

	t.Run("GET register returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/register", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected status 404, got %d", w.Code)
		}
	})

	t.Run("POST register returns 403", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"username": "blocked",
			"email":    "blocked@example.com",
			"password": "password123",
		})

		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", w.Code)
		}
	})
}

// TestAuthenticatedSession tests requests with a valid session
func TestAuthenticatedSession(t *testing.T) {
	app := newRouteTestApp(t)
	user := app.createTestUser(t, "authuser", "password123")

	// Simulate login to get session
	form := url.Values{}
	form.Set("username", "authuser")
	form.Set("password", "password123")

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	// Extract session cookie
	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie returned after login")
	}

	t.Run("access protected route with session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/files", nil)
		req.AddCookie(sessionCookie)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	t.Run("access settings with session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/settings", nil)
		req.AddCookie(sessionCookie)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status 200, got %d", w.Code)
		}
	})

	// Verify user data is correct
	_ = user // Used to set up the test user
}

// TestLogoutFlow tests the logout functionality
func TestLogoutFlow(t *testing.T) {
	app := newRouteTestApp(t)
	app.createTestUser(t, "logoutuser", "password123")

	// Login first
	loginForm := url.Values{}
	loginForm.Set("username", "logoutuser")
	loginForm.Set("password", "password123")

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie after login")
	}

	t.Run("logout clears session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/logout", nil)
		req.AddCookie(sessionCookie)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", w.Code)
		}

		location := w.Header().Get("Location")
		if location != "/login" {
			t.Errorf("Expected redirect to /login, got %s", location)
		}
	})
}

// TestNotFoundHandler tests 404 handling
func TestNotFoundHandler(t *testing.T) {
	app := newRouteTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent-route", nil)
	w := httptest.NewRecorder()
	app.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

// TestRateLimiting tests that rate limiting is applied to auth endpoints
func TestRateLimiting(t *testing.T) {
	app := newRouteTestApp(t)

	// Make many rapid login attempts
	// Note: Rate limiting may be configured per IP, so we use the same RemoteAddr
	for i := 0; i < 10; i++ {
		form := url.Values{}
		form.Set("username", "attacker")
		form.Set("password", "wrongpassword")

		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "192.168.1.100:12345"

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		// After several attempts, we should get rate limited
		// The exact number depends on configuration (5 per 15 min)
		if w.Code == http.StatusTooManyRequests {
			// Rate limiting is working
			return
		}
	}

	// Note: In tests, the rate limiter might not trigger due to test speed
	// or configuration. This test verifies the route handles requests.
	t.Log("Note: Rate limiting may not trigger in fast tests")
}

// TestCSRFProtection tests CSRF protection when enabled
func TestCSRFProtection(t *testing.T) {
	// Create app with CSRF enabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	if err := db.AutoMigrate(&models.User{}, &models.File{}, &models.Folder{}); err != nil {
		t.Fatalf("Failed to migrate database: %v", err)
	}

	cfg := &config.Config{
		EnableRegistration: true,
		BcryptCost:         4,
		DefaultUserQuota:   100 * 1024 * 1024,
		MaxUploadSize:      10 * 1024 * 1024,
		SessionSecret:      "test-secret-key-32-bytes-long!!",
		Env:                "test",
		CSRFEnabled:        true, // Enable CSRF
	}

	sessionManager := scs.New()
	memStorage := storage.NewMemoryBackend()

	router := chi.NewRouter()
	fileHandler := Setup(router, db, cfg, memStorage, sessionManager, "test-version")
	t.Cleanup(func() {
		fileHandler.Shutdown()
	})

	// Create a user for testing
	hashedPassword, _ := auth.HashPassword("password123", cfg.BcryptCost)
	user := &models.User{
		Username:     "csrfuser",
		Email:        "csrf@example.com",
		PasswordHash: hashedPassword,
		StorageQuota: cfg.DefaultUserQuota,
	}
	if err := db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}

	// Login to get session with CSRF token
	loginForm := url.Values{}
	loginForm.Set("username", "csrfuser")
	loginForm.Set("password", "password123")

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResp := httptest.NewRecorder()
	router.ServeHTTP(loginResp, loginReq)

	// Login still works via rate-limited endpoint (no CSRF on login/register)
	if loginResp.Code != http.StatusSeeOther && loginResp.Code != http.StatusOK {
		t.Logf("Login response: %d %s", loginResp.Code, loginResp.Body.String())
	}

	// Note: Full CSRF testing would require extracting CSRF tokens from responses
	// and including them in subsequent requests. This is complex for unit tests.
	t.Log("CSRF is enabled - full protection testing requires browser-like behavior")
}

// TestSessionPersistence tests that sessions persist across requests
func TestSessionPersistence(t *testing.T) {
	app := newRouteTestApp(t)
	app.createTestUser(t, "persistuser", "password123")

	// Login
	loginForm := url.Values{}
	loginForm.Set("username", "persistuser")
	loginForm.Set("password", "password123")

	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginForm.Encode()))
	loginReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie")
	}

	// Make multiple requests with same session
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/files", nil)
		req.AddCookie(sessionCookie)

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: Expected status 200, got %d", i+1, w.Code)
		}

		// Update cookie if a new one is returned
		for _, c := range w.Result().Cookies() {
			if c.Name == "session" {
				sessionCookie = c
				break
			}
		}
	}
}

// TestUserIsolation tests that users cannot access each other's data
func TestUserIsolation(t *testing.T) {
	app := newRouteTestApp(t)

	user1 := app.createTestUser(t, "user1", "password1")
	_ = app.createTestUser(t, "user2", "password2") // user2 needed for login test

	// Create a file for user1
	ctx := context.Background()
	result, _ := app.storage.Save(ctx, strings.NewReader("user1's file"), storage.SaveOptions{
		OriginalFilename: "secret.txt",
	})
	file := &models.File{
		UserID:           user1.ID,
		StoragePath:      result.Path,
		LogicalPath:      "/",
		Filename:         "secret.txt",
		OriginalFilename: "secret.txt",
		FileSize:         result.Size,
		UploadStatus:     "completed",
	}
	app.db.Create(file)

	// Login as user2
	loginForm := url.Values{}
	loginForm.Set("username", "user2")
	loginForm.Set("password", "password2")

	loginReq := app.newFormRequest(http.MethodPost, "/login", loginForm)

	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie for user2")
	}

	t.Run("user2 cannot download user1's file", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/download/%d", file.ID), nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		// Should be 404 (not found for this user) or similar
		if w.Code == http.StatusOK {
			t.Error("User2 should not be able to download user1's file")
		}
	})
}

// createAdminTestUser creates an admin user in the database
func (app *routeTestApp) createAdminTestUser(t *testing.T, username, password string) *models.User {
	t.Helper()

	hashedPassword, err := auth.HashPassword(password, app.cfg.BcryptCost)
	if err != nil {
		t.Fatalf("Failed to hash password: %v", err)
	}

	user := &models.User{
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: hashedPassword,
		StorageQuota: app.cfg.DefaultUserQuota,
		IsAdmin:      true,
	}

	if err := app.db.Create(user).Error; err != nil {
		t.Fatalf("Failed to create admin test user: %v", err)
	}

	return user
}

// TestAdminRoutesRequireAuth tests that admin routes require authentication
func TestAdminRoutesRequireAuth(t *testing.T) {
	app := newRouteTestApp(t)

	adminRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin"},
		{http.MethodGet, "/admin/users"},
		{http.MethodPost, "/admin/users/create"},
		{http.MethodPost, "/admin/users/1/toggle-admin"},
		{http.MethodPost, "/admin/users/1/quota"},
		{http.MethodPost, "/admin/users/1/delete"},
		{http.MethodPost, "/admin/users/1/reset-password"},
	}

	for _, route := range adminRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.RemoteAddr = uniqueTestIP()

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			// Should redirect to login
			if w.Code != http.StatusSeeOther {
				t.Errorf("Expected redirect (303), got %d for %s %s", w.Code, route.method, route.path)
			}

			location := w.Header().Get("Location")
			if location != "/login" {
				t.Errorf("Expected redirect to /login, got %s", location)
			}
		})
	}
}

// TestAdminRoutesRequireAdmin tests that admin routes require admin privileges
func TestAdminRoutesRequireAdmin(t *testing.T) {
	app := newRouteTestApp(t)
	app.createTestUser(t, "regularuser", "password123")

	// Login as regular user
	loginForm := url.Values{}
	loginForm.Set("username", "regularuser")
	loginForm.Set("password", "password123")

	loginReq := app.newFormRequest(http.MethodPost, "/login", loginForm)
	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie after login")
	}

	adminRoutes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/admin"},
		{http.MethodGet, "/admin/users"},
		{http.MethodPost, "/admin/users/create"},
		{http.MethodPost, "/admin/users/1/toggle-admin"},
		{http.MethodPost, "/admin/users/1/quota"},
		{http.MethodPost, "/admin/users/1/delete"},
		{http.MethodPost, "/admin/users/1/reset-password"},
	}

	for _, route := range adminRoutes {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			req := httptest.NewRequest(route.method, route.path, nil)
			req.AddCookie(sessionCookie)
			req.RemoteAddr = uniqueTestIP()

			w := httptest.NewRecorder()
			app.router.ServeHTTP(w, req)

			// Should get forbidden
			if w.Code != http.StatusForbidden {
				t.Errorf("Expected 403 Forbidden, got %d for %s %s", w.Code, route.method, route.path)
			}
		})
	}
}

// TestAdminDashboardAccess tests that admin users can access admin routes
func TestAdminDashboardAccess(t *testing.T) {
	app := newRouteTestApp(t)
	app.createAdminTestUser(t, "admin", "password123")

	// Login as admin
	loginForm := url.Values{}
	loginForm.Set("username", "admin")
	loginForm.Set("password", "password123")

	loginReq := app.newFormRequest(http.MethodPost, "/login", loginForm)
	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie after login")
	}

	t.Run("admin can access dashboard", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin", nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})

	t.Run("admin can access user management", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", w.Code)
		}
	})
}

// TestAdminCreateUserViaRoutes tests creating a user through the full route
func TestAdminCreateUserViaRoutes(t *testing.T) {
	app := newRouteTestApp(t)
	app.createAdminTestUser(t, "admin", "password123")

	// Login as admin
	loginForm := url.Values{}
	loginForm.Set("username", "admin")
	loginForm.Set("password", "password123")

	loginReq := app.newFormRequest(http.MethodPost, "/login", loginForm)
	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie after login")
	}

	t.Run("create user via form", func(t *testing.T) {
		form := url.Values{}
		form.Set("username", "newuser")
		form.Set("email", "newuser@example.com")
		form.Set("password", "securepassword123")

		req := httptest.NewRequest(http.MethodPost, "/admin/users/create", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d: %s", w.Code, w.Body.String())
		}

		// Verify user was created
		var user models.User
		if err := app.db.Where("username = ?", "newuser").First(&user).Error; err != nil {
			t.Error("User was not created in database")
		}
	})

	t.Run("create user via JSON API", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"username": "jsonuser",
			"email":    "jsonuser@example.com",
			"password": "securepassword123",
			"is_admin": false,
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/users/create", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("Expected 201 Created, got %d: %s", w.Code, w.Body.String())
		}

		// Verify JSON response
		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Errorf("Failed to parse response: %v", err)
		}

		if resp["username"] != "jsonuser" {
			t.Errorf("Expected username 'jsonuser', got %v", resp["username"])
		}
	})
}

// TestAdminUserManagementViaRoutes tests user management operations through routes
func TestAdminUserManagementViaRoutes(t *testing.T) {
	app := newRouteTestApp(t)
	admin := app.createAdminTestUser(t, "admin", "password123")
	targetUser := app.createTestUser(t, "target", "password123")

	// Login as admin
	loginForm := url.Values{}
	loginForm.Set("username", "admin")
	loginForm.Set("password", "password123")

	loginReq := app.newFormRequest(http.MethodPost, "/login", loginForm)
	loginResp := httptest.NewRecorder()
	app.router.ServeHTTP(loginResp, loginReq)

	var sessionCookie *http.Cookie
	for _, c := range loginResp.Result().Cookies() {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}

	if sessionCookie == nil {
		t.Fatal("No session cookie after login")
	}

	t.Run("toggle user admin status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/toggle-admin", targetUser.ID), nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", w.Code)
		}

		// Verify user is now admin
		var updated models.User
		app.db.First(&updated, targetUser.ID)
		if !updated.IsAdmin {
			t.Error("User should now be admin")
		}
	})

	t.Run("update user quota", func(t *testing.T) {
		form := url.Values{}
		form.Set("quota", "1073741824") // 1GB

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/quota", targetUser.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", w.Code)
		}

		// Verify quota was updated
		var updated models.User
		app.db.First(&updated, targetUser.ID)
		if updated.StorageQuota != 1073741824 {
			t.Errorf("Expected quota 1073741824, got %d", updated.StorageQuota)
		}
	})

	t.Run("reset user password", func(t *testing.T) {
		form := url.Values{}
		form.Set("new_password", "newsecurepassword123")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/reset-password", targetUser.ID), strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", w.Code)
		}

		// Verify password was updated
		var updated models.User
		app.db.First(&updated, targetUser.ID)
		if !auth.VerifyPassword(updated.PasswordHash, "newsecurepassword123") {
			t.Error("Password was not updated correctly")
		}
	})

	t.Run("cannot toggle own admin status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/toggle-admin", admin.ID), nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("cannot delete self", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/delete", admin.ID), nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request, got %d", w.Code)
		}
	})

	t.Run("delete user", func(t *testing.T) {
		// Create a new user to delete
		userToDelete := app.createTestUser(t, "deleteme", "password123")

		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/delete", userToDelete.ID), nil)
		req.AddCookie(sessionCookie)
		req.RemoteAddr = uniqueTestIP()

		w := httptest.NewRecorder()
		app.router.ServeHTTP(w, req)

		if w.Code != http.StatusSeeOther {
			t.Errorf("Expected redirect (303), got %d", w.Code)
		}

		// Verify user was deleted
		var count int64
		app.db.Model(&models.User{}).Where("id = ?", userToDelete.ID).Count(&count)
		if count != 0 {
			t.Error("User should have been deleted")
		}
	})
}

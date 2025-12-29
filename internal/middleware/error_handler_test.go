package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadErrorTemplates(t *testing.T) {
	// Create temporary template directory structure
	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)

	// Create web/templates directory in temp location
	templatesDir := filepath.Join(tempDir, "web", "templates")
	err := os.MkdirAll(templatesDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create templates directory: %v", err)
	}

	// Create basic layout template
	layoutContent := `{{define "layout.html"}}<!DOCTYPE html><html><body>{{template "content" .}}</body></html>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "layout.html"), []byte(layoutContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create layout template: %v", err)
	}

	// Create 404 template
	notFoundContent := `{{define "content"}}<h1>404 - {{.Title}}</h1>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "404.html"), []byte(notFoundContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create 404 template: %v", err)
	}

	// Create 500 template
	errorContent := `{{define "content"}}<h1>500 - {{.Title}}</h1>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "500.html"), []byte(errorContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create 500 template: %v", err)
	}

	// Change to temp directory so templates can be found
	os.Chdir(tempDir)

	// Test successful template loading
	err = LoadErrorTemplates()
	if err != nil {
		t.Errorf("LoadErrorTemplates() failed: %v", err)
	}

	if errorTemplates == nil {
		t.Error("errorTemplates should not be nil after loading")
	}

	// Reset for next test
	errorTemplates = nil
}

func TestNotFoundHandler(t *testing.T) {
	// Setup templates
	setupTestTemplates(t)

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	NotFoundHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}

func TestNotFoundHandler_NoTemplates(t *testing.T) {
	// Reset templates to nil
	errorTemplates = nil

	req := httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	rec := httptest.NewRecorder()

	NotFoundHandler(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected fallback error message")
	}
	if body != "Error: Page Not Found" {
		t.Errorf("Expected fallback message, got: %s", body)
	}
}

func TestInternalErrorHandler(t *testing.T) {
	// Setup templates
	setupTestTemplates(t)

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()

	InternalErrorHandler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected non-empty response body")
	}
}

func TestInternalErrorHandler_NoTemplates(t *testing.T) {
	// Reset templates to nil
	errorTemplates = nil

	req := httptest.NewRequest(http.MethodGet, "/error", nil)
	rec := httptest.NewRecorder()

	InternalErrorHandler(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected fallback error message")
	}
	if body != "Error: Internal Server Error" {
		t.Errorf("Expected fallback message, got: %s", body)
	}
}

func TestRecoverMiddleware(t *testing.T) {
	// Setup templates for error rendering
	setupTestTemplates(t)

	tests := []struct {
		name          string
		handler       http.HandlerFunc
		expectPanic   bool
		expectedCode  int
	}{
		{
			name: "normal request without panic",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("OK"))
			},
			expectPanic:  false,
			expectedCode: http.StatusOK,
		},
		{
			name: "request with panic",
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic("test panic")
			},
			expectPanic:  true,
			expectedCode: http.StatusInternalServerError,
		},
		{
			name: "request with string panic",
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic("something went wrong")
			},
			expectPanic:  true,
			expectedCode: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()

			// Wrap handler with recover middleware
			middleware := RecoverMiddleware(tt.handler)
			middleware.ServeHTTP(rec, req)

			if rec.Code != tt.expectedCode {
				t.Errorf("Expected status %d, got %d", tt.expectedCode, rec.Code)
			}

			if tt.expectPanic {
				body := rec.Body.String()
				if body == "" {
					t.Error("Expected error page content after panic")
				}
			}
		})
	}
}

func TestRenderError_InvalidTemplate(t *testing.T) {
	// Setup base templates but without the specific error page
	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)

	templatesDir := filepath.Join(tempDir, "web", "templates")
	err := os.MkdirAll(templatesDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create templates directory: %v", err)
	}

	layoutContent := `{{define "layout.html"}}<!DOCTYPE html><html><body>{{template "content" .}}</body></html>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "layout.html"), []byte(layoutContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create layout template: %v", err)
	}

	os.Chdir(tempDir)
	LoadErrorTemplates()

	rec := httptest.NewRecorder()
	
	// Try to render a template that doesn't exist
	renderError(rec, "nonexistent.html", map[string]any{
		"Title": "Test Error",
	})

	body := rec.Body.String()
	if body == "" {
		t.Error("Expected error message when template doesn't exist")
	}
}

// setupTestTemplates creates a minimal template structure for testing
func setupTestTemplates(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(oldWd) })

	templatesDir := filepath.Join(tempDir, "web", "templates")
	err := os.MkdirAll(templatesDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create templates directory: %v", err)
	}

	// Create layout template
	layoutContent := `{{define "layout.html"}}<!DOCTYPE html><html><body>{{template "content" .}}</body></html>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "layout.html"), []byte(layoutContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create layout template: %v", err)
	}

	// Create 404 template
	notFoundContent := `{{define "content"}}<h1>404 - {{.Title}}</h1>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "404.html"), []byte(notFoundContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create 404 template: %v", err)
	}

	// Create 500 template
	errorContent := `{{define "content"}}<h1>500 - {{.Title}}</h1>{{end}}`
	err = os.WriteFile(filepath.Join(templatesDir, "500.html"), []byte(errorContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create 500 template: %v", err)
	}

	os.Chdir(tempDir)
	LoadErrorTemplates()
}

package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecurityHeaders(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		expectedHeaders map[string]string
	}{
		{
			name: "regular page - restrictive CSP",
			path: "/dashboard",
			expectedHeaders: map[string]string{
				"X-Frame-Options":           "DENY",
				"X-Content-Type-Options":    "nosniff",
				"X-XSS-Protection":          "1; mode=block",
				"Referrer-Policy":           "strict-origin-when-cross-origin",
				"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
			},
		},
		{
			name: "preview page - allow same origin framing",
			path: "/preview/12345",
			expectedHeaders: map[string]string{
				"X-Frame-Options":           "SAMEORIGIN",
				"X-Content-Type-Options":    "nosniff",
				"X-XSS-Protection":          "1; mode=block",
				"Referrer-Policy":           "strict-origin-when-cross-origin",
				"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
			},
		},
		{
			name: "upload page",
			path: "/upload",
			expectedHeaders: map[string]string{
				"X-Frame-Options":           "DENY",
				"X-Content-Type-Options":    "nosniff",
				"X-XSS-Protection":          "1; mode=block",
				"Referrer-Policy":           "strict-origin-when-cross-origin",
				"Permissions-Policy":        "geolocation=(), microphone=(), camera=()",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test handler that just returns 200 OK
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with security headers middleware
			wrappedHandler := SecurityHeaders(handler)

			// Create request and response recorder
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			// Execute request
			wrappedHandler.ServeHTTP(rec, req)

			// Check all expected headers
			for headerName, expectedValue := range tt.expectedHeaders {
				actualValue := rec.Header().Get(headerName)
				if actualValue != expectedValue {
					t.Errorf("Header %s: expected %q, got %q", headerName, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestSecurityHeaders_CSP(t *testing.T) {
	tests := []struct {
		name            string
		path            string
		shouldContain   []string
		shouldNotContain []string
	}{
		{
			name: "regular page CSP - deny framing",
			path: "/dashboard",
			shouldContain: []string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline'",
				"style-src 'self' 'unsafe-inline'",
				"img-src 'self' data:",
				"font-src 'self'",
				"connect-src 'self'",
				"frame-ancestors 'none'",
			},
			shouldNotContain: []string{
				"frame-ancestors 'self'",
			},
		},
		{
			name: "preview page CSP - allow self framing",
			path: "/preview/abc123",
			shouldContain: []string{
				"default-src 'self'",
				"script-src 'self' 'unsafe-inline'",
				"style-src 'self' 'unsafe-inline'",
				"img-src 'self' data:",
				"font-src 'self'",
				"connect-src 'self'",
				"frame-ancestors 'self'",
			},
			shouldNotContain: []string{
				"frame-ancestors 'none'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := SecurityHeaders(handler)
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(rec, req)

			csp := rec.Header().Get("Content-Security-Policy")
			if csp == "" {
				t.Fatal("Content-Security-Policy header not set")
			}

			// Check for required content
			for _, required := range tt.shouldContain {
				if !strings.Contains(csp, required) {
					t.Errorf("CSP should contain %q, got: %s", required, csp)
				}
			}

			// Check for forbidden content
			for _, forbidden := range tt.shouldNotContain {
				if strings.Contains(csp, forbidden) {
					t.Errorf("CSP should NOT contain %q, got: %s", forbidden, csp)
				}
			}
		})
	}
}

func TestSecurityHeaders_AllHeadersPresent(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := SecurityHeaders(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	// Ensure all security headers are set
	requiredHeaders := []string{
		"X-Frame-Options",
		"X-Content-Type-Options",
		"X-XSS-Protection",
		"Referrer-Policy",
		"Content-Security-Policy",
		"Permissions-Policy",
	}

	for _, headerName := range requiredHeaders {
		if rec.Header().Get(headerName) == "" {
			t.Errorf("Required security header %s was not set", headerName)
		}
	}
}

func TestSecurityHeaders_RequestPassesThrough(t *testing.T) {
	// Ensure the middleware passes the request through to the next handler
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrappedHandler := SecurityHeaders(handler)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if !called {
		t.Error("Next handler was not called")
	}

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	if rec.Body.String() != "OK" {
		t.Errorf("Expected body 'OK', got %q", rec.Body.String())
	}
}

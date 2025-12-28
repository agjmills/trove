package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	csrf "filippo.io/csrf/gorilla"
	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
)

// TestCSRFProtection verifies that filippo.io/csrf/gorilla middleware is working correctly.
// This package uses Fetch Metadata headers (Sec-Fetch-Site) for CSRF protection instead of tokens.
// Non-browser requests (without Sec-Fetch-Site or Origin headers) are allowed through.
func TestCSRFProtection(t *testing.T) {
	t.Run("Cross-origin browser POST is rejected", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		// Simulate a cross-origin browser request
		req := httptest.NewRequest("POST", "http://example.com/test", strings.NewReader("data=test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "cross-site") // Browser header indicating cross-origin
		req.Header.Set("Origin", "http://attacker.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for cross-origin POST, got %d", http.StatusForbidden, w.Code)
		}
	})

	t.Run("Same-origin browser POST is allowed", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		// Simulate a same-origin browser request
		req := httptest.NewRequest("POST", "http://example.com/test", strings.NewReader("data=test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for same-origin POST, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("GET requests work without headers", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for GET request, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("Non-browser POST requests are allowed", func(t *testing.T) {
		// filippo.io/csrf allows requests without Sec-Fetch-Site/Origin headers
		// since CSRF is fundamentally a browser issue
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		// API request without browser headers
		req := httptest.NewRequest("POST", "/test", strings.NewReader("data=test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// No Sec-Fetch-Site or Origin header - simulates non-browser client
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected status %d for non-browser POST, got %d", http.StatusOK, w.Code)
		}
	})
}

// TestCSRFNonBrowserClients provides comprehensive tests for non-browser HTTP client scenarios.
// These tests verify the filippo.io/csrf behavioral differences from gorilla/csrf:
// - No token-based CSRF validation (tokens are deprecated/no-op)
// - Fetch Metadata headers (Sec-Fetch-Site) are used for browser detection
// - Non-browser clients (without Sec-Fetch-Site) are allowed through
// - Origin header alone can trigger cross-site rejection
func TestCSRFNonBrowserClients(t *testing.T) {
	csrfKey := []byte("test-secret-key-32-bytes-long!!")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	t.Run("CLI tool without any browser headers", func(t *testing.T) {
		// Simulates: curl -X POST http://localhost/api/endpoint -d "data=value"
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		// No browser headers at all
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("CLI tool request should be allowed, got status %d", w.Code)
		}
	})

	t.Run("API client with JSON content type", func(t *testing.T) {
		// Simulates programmatic API access with JSON
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader(`{"key":"value"}`))
		req.Header.Set("Content-Type", "application/json")
		// No Sec-Fetch-Site header - this is a non-browser client
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("JSON API client request should be allowed, got status %d", w.Code)
		}
	})

	t.Run("API client with custom User-Agent", func(t *testing.T) {
		// Simulates API client libraries like requests (Python), axios, etc.
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("User-Agent", "MyApp/1.0 python-requests/2.28.0")
		// No browser headers
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Custom User-Agent API client should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Webhook callback without browser headers", func(t *testing.T) {
		// Simulates incoming webhook from external service
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/webhooks/callback", strings.NewReader(`{"event":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Webhook-Signature", "sha256=abc123")
		// No Origin or Sec-Fetch-Site headers
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Webhook callback should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Mobile app API client", func(t *testing.T) {
		// Simulates native mobile app making API requests
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/sync", strings.NewReader(`{"data":"sync"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "TroveApp/2.0 (iOS 17.0)")
		req.Header.Set("Authorization", "Bearer token123") // API token auth
		// No Sec-Fetch-Site header
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Mobile app request should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Server-to-server integration", func(t *testing.T) {
		// Simulates backend service calling our API
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/internal", strings.NewReader(`{"action":"sync"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "internal-service-key")
		// No browser headers - server-to-server
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Server-to-server request should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Request with only Referer header (no Origin)", func(t *testing.T) {
		// Some older clients only send Referer, not Origin
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Referer", "http://example.com/page")
		// No Sec-Fetch-Site or Origin header
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// Without Sec-Fetch-Site, this should be allowed (non-browser client)
		if w.Code != http.StatusOK {
			t.Errorf("Request with only Referer should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Request with mismatched Origin but no Sec-Fetch-Site", func(t *testing.T) {
		// Origin header without Sec-Fetch-Site - ambiguous case
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://example.com/api", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://attacker.com")
		// No Sec-Fetch-Site header
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// filippo.io/csrf checks Origin even without Sec-Fetch-Site for cross-origin detection
		// This should be rejected because Origin doesn't match
		if w.Code != http.StatusForbidden {
			t.Errorf("Request with mismatched Origin should be rejected, got status %d", w.Code)
		}
	})

	t.Run("Request with matching Origin but no Sec-Fetch-Site", func(t *testing.T) {
		// Origin matches, no Sec-Fetch-Site - should be allowed
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://example.com/api", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
		// No Sec-Fetch-Site header
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request with matching Origin should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Browser with Sec-Fetch-Site none (user-initiated navigation)", func(t *testing.T) {
		// Browser navigating via bookmark or typing URL
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "none")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// Sec-Fetch-Site: none means direct navigation, should be allowed
		if w.Code != http.StatusOK {
			t.Errorf("Direct navigation (Sec-Fetch-Site: none) should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Browser with Sec-Fetch-Site same-site is rejected", func(t *testing.T) {
		// IMPORTANT: filippo.io/csrf is stricter than gorilla/csrf
		// It rejects Sec-Fetch-Site: same-site requests (e.g., from subdomains)
		// This is a behavioral difference that may affect cross-subdomain deployments
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/api/endpoint", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-site")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// same-site is REJECTED by filippo.io/csrf (stricter than gorilla/csrf)
		// This means requests from subdomains (e.g., api.example.com to example.com) are blocked
		if w.Code != http.StatusForbidden {
			t.Errorf("Same-site request should be rejected by filippo.io/csrf, got status %d", w.Code)
		}
	})

	t.Run("DELETE request from API client", func(t *testing.T) {
		// DELETE requests should also work for non-browser clients
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("DELETE", "/api/resource/123", nil)
		req.Header.Set("Content-Type", "application/json")
		// No browser headers
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("DELETE from API client should be allowed, got status %d", w.Code)
		}
	})

	t.Run("PUT request from API client", func(t *testing.T) {
		// PUT requests should also work for non-browser clients
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("PUT", "/api/resource/123", strings.NewReader(`{"updated":"data"}`))
		req.Header.Set("Content-Type", "application/json")
		// No browser headers
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("PUT from API client should be allowed, got status %d", w.Code)
		}
	})

	t.Run("PATCH request from API client", func(t *testing.T) {
		// PATCH requests should also work for non-browser clients
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("PATCH", "/api/resource/123", strings.NewReader(`{"field":"value"}`))
		req.Header.Set("Content-Type", "application/json")
		// No browser headers
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("PATCH from API client should be allowed, got status %d", w.Code)
		}
	})
}

// TestCSRFBrowserBehavior verifies expected browser-based CSRF protection scenarios
func TestCSRFBrowserBehavior(t *testing.T) {
	csrfKey := []byte("test-secret-key-32-bytes-long!!")

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	t.Run("Cross-site form submission blocked", func(t *testing.T) {
		// Simulates malicious cross-site form submission
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://victim.com/transfer", strings.NewReader("amount=1000&to=attacker"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Origin", "http://attacker.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Cross-site form submission should be blocked, got status %d", w.Code)
		}
	})

	t.Run("Cross-site XHR blocked", func(t *testing.T) {
		// Simulates cross-origin XHR from malicious site
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://victim.com/api/action", strings.NewReader(`{"action":"malicious"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Origin", "http://attacker.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Cross-site XHR should be blocked, got status %d", w.Code)
		}
	})

	t.Run("Embedded iframe cross-origin blocked", func(t *testing.T) {
		// Simulates form submission from embedded iframe
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://victim.com/settings", strings.NewReader("setting=evil"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		req.Header.Set("Sec-Fetch-Dest", "iframe")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Cross-origin iframe submission should be blocked, got status %d", w.Code)
		}
	})

	t.Run("Same-origin iframe allowed", func(t *testing.T) {
		// Same-origin iframe submissions should work
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://example.com/settings", strings.NewReader("setting=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Fetch-Dest", "iframe")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Same-origin iframe submission should be allowed, got status %d", w.Code)
		}
	})

	t.Run("AJAX from same origin allowed", func(t *testing.T) {
		// Normal AJAX from same origin should work
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://example.com/api/action", strings.NewReader(`{"data":"test"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Fetch-Mode", "cors")
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Same-origin AJAX should be allowed, got status %d", w.Code)
		}
	})

	t.Run("Form submission from same origin allowed", func(t *testing.T) {
		// Normal form submission should work
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "http://example.com/form", strings.NewReader("field=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Sec-Fetch-Mode", "navigate")
		req.Header.Set("Sec-Fetch-Dest", "document")
		req.Header.Set("Origin", "http://example.com")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Same-origin form submission should be allowed, got status %d", w.Code)
		}
	})
}

// TestCSRFTokenBehavior documents that token-based CSRF validation is NOT enforced
// by filippo.io/csrf. The middleware uses Fetch Metadata headers instead of tokens.
// Token() exists only for API compatibility but is not used in this codebase.
func TestCSRFTokenBehavior(t *testing.T) {
	csrfKey := []byte("test-secret-key-32-bytes-long!!")

	t.Run("POST without token succeeds if same-origin", func(t *testing.T) {
		// Key difference from gorilla/csrf: no token validation
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		// POST without any CSRF token
		req := httptest.NewRequest("POST", "/test", strings.NewReader("data=value"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		// Note: NO csrf_token field in body, NO X-CSRF-Token header
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// Should succeed because Sec-Fetch-Site is same-origin
		if w.Code != http.StatusOK {
			t.Errorf("POST without token but with same-origin should succeed, got %d", w.Code)
		}
	})

	t.Run("POST with invalid token succeeds if same-origin", func(t *testing.T) {
		// Token validation is NOT performed by filippo.io/csrf
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		// POST with completely invalid token
		req := httptest.NewRequest("POST", "/test", strings.NewReader("csrf_token=INVALID_TOKEN_12345"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		// Should succeed because token is NOT validated
		if w.Code != http.StatusOK {
			t.Errorf("POST with invalid token but same-origin should succeed, got %d", w.Code)
		}
	})
}

// TestRateLimiting verifies that tollbooth rate limiting is working correctly
func TestRateLimiting(t *testing.T) {
	t.Run("Rate limiting blocks excessive requests", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		// Create rate limiter: allow 5 requests (with burst), then throttle
		lmt := tollbooth.NewLimiter(1.0, &limiter.ExpirableOptions{ // 1 req/sec
			DefaultExpirationTTL: time.Minute,
		})
		lmt.SetMax(5) // Allow burst of 5
		lmt.SetMessage("Too many requests. Please try again later.")

		// Make 7 requests quickly
		successCount := 0
		blockedCount := 0

		for i := 0; i < 7; i++ {
			req := httptest.NewRequest("POST", "/test", strings.NewReader("data=test"))
			req.RemoteAddr = "192.168.1.1:12345" // Same IP for all requests
			w := httptest.NewRecorder()

			tollbooth.LimitHandler(lmt, handler).ServeHTTP(w, req)

			if w.Code == http.StatusTooManyRequests {
				blockedCount++
			} else {
				successCount++
			}
		}

		// Should allow some and block some
		if blockedCount < 1 {
			t.Errorf("Expected at least 1 blocked request, got %d blocked and %d success", blockedCount, successCount)
		}
	})

	t.Run("Rate limiting is per-IP", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		lmt := tollbooth.NewLimiter(10.0, &limiter.ExpirableOptions{ // 10 req/sec, plenty
			DefaultExpirationTTL: time.Minute,
		})
		lmt.SetMax(2) // Allow 2 requests burst per IP

		// Requests from different IPs should be independent
		ips := []string{"10.0.0.1:1234", "10.0.0.2:1234", "10.0.0.3:1234"}

		for _, ip := range ips {
			for i := 0; i < 2; i++ {
				req := httptest.NewRequest("POST", "/test", nil)
				req.RemoteAddr = ip
				w := httptest.NewRecorder()

				tollbooth.LimitHandler(lmt, handler).ServeHTTP(w, req)

				// Should not be rate limited (2 requests per IP, limit is 2)
				if w.Code == http.StatusTooManyRequests {
					t.Errorf("Request %d from IP %s should not be rate limited", i+1, ip)
				}
			}
		}
	})

	t.Run("Rate limiting respects X-Forwarded-For header", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		lmt := tollbooth.NewLimiter(1.0, &limiter.ExpirableOptions{
			DefaultExpirationTTL: time.Minute,
		})
		lmt.SetMax(5) // Allow 5 requests

		proxyIP := "192.168.1.100"

		for i := 0; i < 7; i++ {
			req := httptest.NewRequest("POST", "/test", nil)
			req.Header.Set("X-Forwarded-For", proxyIP)
			req.RemoteAddr = "10.0.0.1:1234" // Different from X-Forwarded-For
			w := httptest.NewRecorder()

			tollbooth.LimitHandler(lmt, handler).ServeHTTP(w, req)

			// After 5 requests, should start rate limiting based on X-Forwarded-For
			if i >= 6 && w.Code != http.StatusTooManyRequests {
				t.Errorf("Request %d should be rate limited based on X-Forwarded-For", i+1)
			}
		}
	})
}

// TestRateLimitMessage verifies custom rate limit message
func TestRateLimitMessage(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	lmt := tollbooth.NewLimiter(1.0, &limiter.ExpirableOptions{
		DefaultExpirationTTL: time.Minute,
	})
	lmt.SetMax(3) // Allow 3 requests
	lmt.SetMessage("Too many requests. Please try again later.")

	// Make enough requests to trigger rate limit
	var lastResponse *httptest.ResponseRecorder
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("POST", "/test", nil)
		req.RemoteAddr = "192.168.2.1:12345"
		lastResponse = httptest.NewRecorder()
		tollbooth.LimitHandler(lmt, handler).ServeHTTP(lastResponse, req)
	}

	// Check that rate limited response has our custom message
	if lastResponse.Code == http.StatusTooManyRequests {
		body, _ := io.ReadAll(lastResponse.Body)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "Too many requests") {
			t.Errorf("Expected rate limit message, got: %s", bodyStr)
		}
	} else {
		t.Errorf("Expected rate limit to be triggered, got status %d", lastResponse.Code)
	}
}

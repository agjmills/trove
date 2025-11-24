package middleware_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/gorilla/csrf"
)

// TestCSRFProtection verifies that gorilla/csrf middleware is working correctly
func TestCSRFProtection(t *testing.T) {
	t.Run("POST without CSRF token is rejected", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(csrfKey, csrf.Secure(false))(handler)

		req := httptest.NewRequest("POST", "/test", strings.NewReader("data=test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d for POST without CSRF token, got %d", http.StatusForbidden, w.Code)
		}
	})

	t.Run("GET requests work without CSRF token", func(t *testing.T) {
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

	t.Run("CSRF with custom field name", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		csrfKey := []byte("test-secret-key-32-bytes-long!!")
		protected := csrf.Protect(
			csrfKey,
			csrf.Secure(false),
			csrf.FieldName("csrf_token"), // Our custom field name
		)(handler)

		// Test that POST without token is still rejected
		req := httptest.NewRequest("POST", "/test", strings.NewReader("data=test"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()

		protected.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
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

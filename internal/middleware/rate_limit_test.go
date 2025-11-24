package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewRateLimiter(t *testing.T) {
	rl := NewRateLimiter(5, 15*time.Minute)

	if rl == nil {
		t.Fatal("NewRateLimiter() returned nil")
	}

	if rl.limit != 5 {
		t.Errorf("limit = %d, want 5", rl.limit)
	}

	if rl.window != 15*time.Minute {
		t.Errorf("window = %v, want 15m", rl.window)
	}

	if rl.requests == nil {
		t.Error("requests map is nil")
	}
}

func TestRateLimiter_AllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(5, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 5 requests (within limit)
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(3, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 3 requests (at limit)
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d status = %d, want %d (should be allowed)", i+1, w.Code, http.StatusOK)
		}
	}

	// 4th request should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Request 4 status = %d, want %d (should be blocked)", w.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewRateLimiter(2, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP 1: Make 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("IP1 request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	// IP 2: Should still be allowed (different IP)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.2:5678"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("IP2 request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	// IP 1: 3rd request should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("IP1 request 3 status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_WindowReset(t *testing.T) {
	rl := NewRateLimiter(2, 100*time.Millisecond)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 2 requests (at limit)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}

	// 3rd request should be blocked
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Request 3 (before reset) status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// After window expires, should be allowed again
	req = httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Request after window reset status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want \"192.168.1.1\"", ip)
	}
}

func TestGetClientIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "192.168.1.1")

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want \"192.168.1.1\" (from X-Forwarded-For)", ip)
	}
}

func TestGetClientIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Real-IP", "192.168.1.1")

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want \"192.168.1.1\" (from X-Real-IP)", ip)
	}
}

func TestGetClientIP_XForwardedForPriority(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "192.168.1.1")
	req.Header.Set("X-Real-IP", "192.168.1.2")

	ip := getClientIP(req)
	// X-Forwarded-For takes priority
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want \"192.168.1.1\" (X-Forwarded-For should take priority)", ip)
	}
}

func TestGetClientIP_NoPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1"

	ip := getClientIP(req)
	if ip != "192.168.1.1" {
		t.Errorf("getClientIP() = %q, want \"192.168.1.1\"", ip)
	}
}

func TestRateLimiter_ConcurrentRequests(t *testing.T) {
	rl := NewRateLimiter(10, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make concurrent requests from same IP
	done := make(chan int, 15)
	for i := 0; i < 15; i++ {
		go func(n int) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "192.168.1.1:1234"
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			done <- w.Code
		}(i)
	}

	// Collect results
	okCount := 0
	blockedCount := 0
	for i := 0; i < 15; i++ {
		code := <-done
		if code == http.StatusOK {
			okCount++
		} else if code == http.StatusTooManyRequests {
			blockedCount++
		}
	}

	// Should have ~10 successful and ~5 blocked (may vary due to race conditions)
	if okCount < 10 || okCount > 11 {
		t.Errorf("okCount = %d, want around 10", okCount)
	}

	if blockedCount < 4 || blockedCount > 6 {
		t.Errorf("blockedCount = %d, want around 5", blockedCount)
	}
}

func TestRateLimiter_ZeroLimit(t *testing.T) {
	rl := NewRateLimiter(0, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request is allowed (count starts at 1, check is count >= limit, so 1 >= 0 is true)
	// Actually the logic allows the first request, then blocks at limit
	// Since limit is 0, after first request count=1, then 1 >= 0, so blocks
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// With limit=0, first request should succeed (count starts at 1)
	if w.Code != http.StatusOK {
		t.Errorf("First request with limit=0 status = %d, want %d", w.Code, http.StatusOK)
	}

	// Second request should be blocked
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.1:1234"
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, req2)

	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request with limit=0 status = %d, want %d", w2.Code, http.StatusTooManyRequests)
	}
}

func TestRateLimiter_HighLimit(t *testing.T) {
	rl := NewRateLimiter(1000, 1*time.Minute)

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Make 100 requests (well within limit)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d status = %d, want %d", i+1, w.Code, http.StatusOK)
		}
	}
}

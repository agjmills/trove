package middleware

import (
	"net/http"
	"sync"
	"time"
)

// RateLimiter tracks request counts per IP address
type RateLimiter struct {
	requests map[string]*requestInfo
	mu       sync.RWMutex
	limit    int           // Maximum requests allowed
	window   time.Duration // Time window for rate limiting
}

type requestInfo struct {
	count     int
	firstSeen time.Time
}

// NewRateLimiter creates a new rate limiter
// limit: maximum number of requests allowed in the time window
// window: time duration for the rate limit window (e.g., 15 minutes)
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*requestInfo),
		limit:    limit,
		window:   window,
	}

	// Start cleanup goroutine to prevent memory leaks
	go rl.cleanup()

	return rl
}

// Middleware returns a middleware handler that rate limits requests
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		rl.mu.Lock()
		defer rl.mu.Unlock()

		now := time.Now()
		info, exists := rl.requests[ip]

		if !exists {
			// First request from this IP
			rl.requests[ip] = &requestInfo{
				count:     1,
				firstSeen: now,
			}
			next.ServeHTTP(w, r)
			return
		}

		// Check if window has expired
		if now.Sub(info.firstSeen) > rl.window {
			// Reset counter for new window
			info.count = 1
			info.firstSeen = now
			next.ServeHTTP(w, r)
			return
		}

		// Check if limit exceeded
		if info.count >= rl.limit {
			http.Error(w, "Too many requests. Please try again later.", http.StatusTooManyRequests)
			return
		}

		// Increment counter and proceed
		info.count++
		next.ServeHTTP(w, r)
	})
}

// cleanup removes expired entries every 10 minutes to prevent memory leaks
func (rl *RateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, info := range rl.requests {
			if now.Sub(info.firstSeen) > rl.window {
				delete(rl.requests, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// getClientIP extracts the client IP address from the request
// Checks X-Forwarded-For and X-Real-IP headers first (for proxies)
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP if multiple are present
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (strip port if present)
	ip := r.RemoteAddr
	// Remove port from IP:port format
	if idx := len(ip) - 1; idx >= 0 {
		for i := idx; i >= 0; i-- {
			if ip[i] == ':' {
				return ip[:i]
			}
		}
	}
	return ip
}

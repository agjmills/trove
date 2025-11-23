package csrf

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	tokenLength   = 32
	cookieName    = "csrf_token"
	formFieldName = "csrf_token"
	tokenTTL      = 24 * time.Hour
)

// Store holds CSRF tokens with expiration
type Store struct {
	tokens map[string]time.Time
	mu     sync.RWMutex
}

var globalStore = &Store{
	tokens: make(map[string]time.Time),
}

// GenerateToken creates a new CSRF token
func GenerateToken() (string, error) {
	b := make([]byte, tokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(b)

	// Store token with expiration
	globalStore.mu.Lock()
	globalStore.tokens[token] = time.Now().Add(tokenTTL)
	globalStore.mu.Unlock()

	// Clean up expired tokens periodically
	go globalStore.cleanup()

	return token, nil
}

// ValidateToken checks if a token is valid
func ValidateToken(token string) bool {
	if token == "" {
		return false
	}

	globalStore.mu.RLock()
	expiry, exists := globalStore.tokens[token]
	globalStore.mu.RUnlock()

	if !exists {
		return false
	}

	// Check if token has expired
	if time.Now().After(expiry) {
		globalStore.mu.Lock()
		delete(globalStore.tokens, token)
		globalStore.mu.Unlock()
		return false
	}

	return true
}

// cleanup removes expired tokens
func (s *Store) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, expiry := range s.tokens {
		if now.After(expiry) {
			delete(s.tokens, token)
		}
	}
}

// Middleware adds CSRF protection to routes
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF check for GET, HEAD, OPTIONS, TRACE
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" || r.Method == "TRACE" {
			next.ServeHTTP(w, r)
			return
		}

		// Get token from form or header
		token := r.FormValue(formFieldName)
		if token == "" {
			token = r.Header.Get("X-CSRF-Token")
		}

		// Validate token
		if !ValidateToken(token) {
			http.Error(w, "CSRF token validation failed", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// SetTokenCookie sets the CSRF token in a cookie
func SetTokenCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: false, // JavaScript needs to read this for AJAX requests
		Secure:   false, // Set to true in production with HTTPS
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(tokenTTL.Seconds()),
	})
}

// GetToken retrieves or generates a CSRF token for the request
func GetToken(w http.ResponseWriter, r *http.Request) (string, error) {
	// Try to get existing token from cookie
	cookie, err := r.Cookie(cookieName)
	if err == nil && ValidateToken(cookie.Value) {
		return cookie.Value, nil
	}

	// Generate new token
	token, err := GenerateToken()
	if err != nil {
		return "", fmt.Errorf("failed to generate CSRF token: %w", err)
	}

	// Set cookie
	SetTokenCookie(w, token)

	return token, nil
}

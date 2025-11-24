package csrf

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGenerateToken(t *testing.T) {
	token1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	if token1 == "" {
		t.Error("GenerateToken() returned empty token")
	}

	// Generate another token
	token2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	// Tokens should be unique
	if token1 == token2 {
		t.Error("GenerateToken() produced identical tokens")
	}

	// Token should be valid immediately after generation
	if !ValidateToken(token1) {
		t.Error("Newly generated token should be valid")
	}
}

func TestValidateToken_Valid(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if !ValidateToken(token) {
		t.Error("ValidateToken() = false, want true for valid token")
	}
}

func TestValidateToken_Empty(t *testing.T) {
	if ValidateToken("") {
		t.Error("ValidateToken() = true, want false for empty token")
	}
}

func TestValidateToken_Invalid(t *testing.T) {
	if ValidateToken("invalid_token_12345") {
		t.Error("ValidateToken() = true, want false for non-existent token")
	}
}

func TestValidateToken_Multiple(t *testing.T) {
	// Generate multiple tokens
	tokens := make([]string, 5)
	for i := 0; i < 5; i++ {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("Failed to generate token %d: %v", i, err)
		}
		tokens[i] = token
	}

	// All should be valid
	for i, token := range tokens {
		if !ValidateToken(token) {
			t.Errorf("Token %d is invalid", i)
		}
	}
}

func TestMiddleware_GET_Allowed(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("GET request status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_HEAD_Allowed(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("HEAD", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("HEAD request status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_OPTIONS_Allowed(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("OPTIONS", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("OPTIONS request status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_POST_WithValidToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	// Test with form value
	form := url.Values{}
	form.Add("csrf_token", token)

	req := httptest.NewRequest("POST", "/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with valid token status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_POST_WithValidTokenInHeader(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("POST with valid header token status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_POST_WithoutToken(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/test", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST without token status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestMiddleware_POST_WithInvalidToken(t *testing.T) {
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	form := url.Values{}
	form.Add("csrf_token", "invalid_token")

	req := httptest.NewRequest("POST", "/test", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("POST with invalid token status = %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestMiddleware_PUT_WithValidToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("PUT", "/test", nil)
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("PUT with valid token status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_DELETE_WithValidToken(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("DELETE", "/test", nil)
	req.Header.Set("X-CSRF-Token", token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("DELETE with valid token status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestStoreCleanup(t *testing.T) {
	// Create a test store
	store := &Store{
		tokens: make(map[string]time.Time),
	}

	// Add some expired tokens
	store.tokens["expired1"] = time.Now().Add(-2 * time.Hour)
	store.tokens["expired2"] = time.Now().Add(-1 * time.Hour)

	// Add some valid tokens
	store.tokens["valid1"] = time.Now().Add(1 * time.Hour)
	store.tokens["valid2"] = time.Now().Add(2 * time.Hour)

	// Run cleanup
	store.cleanup()

	// Check that expired tokens are removed
	if _, exists := store.tokens["expired1"]; exists {
		t.Error("Expired token 'expired1' should be removed")
	}
	if _, exists := store.tokens["expired2"]; exists {
		t.Error("Expired token 'expired2' should be removed")
	}

	// Check that valid tokens remain
	if _, exists := store.tokens["valid1"]; !exists {
		t.Error("Valid token 'valid1' should not be removed")
	}
	if _, exists := store.tokens["valid2"]; !exists {
		t.Error("Valid token 'valid2' should not be removed")
	}
}

func TestGenerateToken_UniqueTokens(t *testing.T) {
	tokens := make(map[string]bool)
	count := 100

	for i := 0; i < count; i++ {
		token, err := GenerateToken()
		if err != nil {
			t.Fatalf("GenerateToken() error at iteration %d: %v", i, err)
		}

		if tokens[token] {
			t.Errorf("Duplicate token generated: %s", token)
		}
		tokens[token] = true
	}

	if len(tokens) != count {
		t.Errorf("Expected %d unique tokens, got %d", count, len(tokens))
	}
}

func TestValidateToken_Concurrent(t *testing.T) {
	token, err := GenerateToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Validate token concurrently from multiple goroutines
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			if !ValidateToken(token) {
				t.Error("Token validation failed in concurrent access")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

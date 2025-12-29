package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agjmills/trove/internal/database/models"
)

func TestRequireAdmin_WithAdminUser(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Admin access granted"))
	})

	adminHandler := RequireAdmin()(handler)

	// Create admin user
	adminUser := &models.User{
		ID:           1,
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: "hashedpassword",
		IsAdmin:      true,
	}

	// Add user to request context
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, adminUser)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "Admin access granted" {
		t.Errorf("Expected 'Admin access granted', got %q", body)
	}
}

func TestRequireAdmin_WithNonAdminUser(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called for non-admin user")
	})

	adminHandler := RequireAdmin()(handler)

	// Create regular (non-admin) user
	regularUser := &models.User{
		ID:           2,
		Username:     "user",
		Email:        "user@example.com",
		PasswordHash: "hashedpassword",
		IsAdmin:      false,
	}

	// Add user to request context
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, regularUser)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "Forbidden - Admin access required\n" {
		t.Errorf("Expected forbidden message, got %q", body)
	}
}

func TestRequireAdmin_WithNoUser(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called without user")
	})

	adminHandler := RequireAdmin()(handler)

	// Request without user in context
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	adminHandler.ServeHTTP(rec, req)

	// Should redirect to login
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

func TestRequireAdmin_WithNilUser(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Handler should not be called with nil user")
	})

	adminHandler := RequireAdmin()(handler)

	// Request with nil user in context
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, (*models.User)(nil))
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	// Should redirect to login
	if rec.Code != http.StatusSeeOther {
		t.Errorf("Expected status 303, got %d", rec.Code)
	}

	location := rec.Header().Get("Location")
	if location != "/login" {
		t.Errorf("Expected redirect to /login, got %s", location)
	}
}

func TestRequireAdmin_DifferentHTTPMethods(t *testing.T) {
	tests := []struct {
		name   string
		method string
	}{
		{"GET", http.MethodGet},
		{"POST", http.MethodPost},
		{"PUT", http.MethodPut},
		{"DELETE", http.MethodDelete},
		{"PATCH", http.MethodPatch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			adminHandler := RequireAdmin()(handler)

			adminUser := &models.User{
				ID:           1,
				Username:     "admin",
				Email:        "admin@example.com",
				PasswordHash: "hashedpassword",
				IsAdmin:      true,
			}

			req := httptest.NewRequest(tt.method, "/admin/action", nil)
			ctx := context.WithValue(req.Context(), UserContextKey, adminUser)
			req = req.WithContext(ctx)

			rec := httptest.NewRecorder()
			adminHandler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", tt.method, rec.Code)
			}
		})
	}
}

func TestRequireAdmin_ChainedMiddleware(t *testing.T) {
	// Test that RequireAdmin works when chained with other middleware
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Success"))
	})

	// Wrap with admin middleware
	adminHandler := RequireAdmin()(handler)

	adminUser := &models.User{
		ID:           1,
		Username:     "admin",
		Email:        "admin@example.com",
		PasswordHash: "hashedpassword",
		IsAdmin:      true,
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	ctx := context.WithValue(req.Context(), UserContextKey, adminUser)
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	adminHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}
}

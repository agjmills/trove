package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database"
	"github.com/agjmills/trove/internal/storage"
)

func TestHealthHandler(t *testing.T) {
	// Setup test database
	cfg := &config.Config{
		DBType: "sqlite",
		DBPath: ":memory:",
		Env:    "test",
	}

	db, err := database.Connect(cfg)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Setup test storage
	storageService, err := storage.NewService(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create storage service: %v", err)
	}

	// Create handler
	handler := NewHealthHandler(db, storageService, "test-version")

	// Create request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Execute
	handler.Health(w, req)

	// Verify response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	// Parse JSON response
	var response HealthResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Check status
	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	// Check version
	if response.Version != "test-version" {
		t.Errorf("Expected version 'test-version', got '%s'", response.Version)
	}

	// Check database check exists
	if dbCheck, ok := response.Checks["database"]; !ok {
		t.Error("Database check missing from response")
	} else if dbCheck.Status != "healthy" {
		t.Errorf("Expected database status 'healthy', got '%s'", dbCheck.Status)
	}

	// Check storage check exists
	if storageCheck, ok := response.Checks["storage"]; !ok {
		t.Error("Storage check missing from response")
	} else if storageCheck.Status != "healthy" {
		t.Errorf("Expected storage status 'healthy', got '%s'", storageCheck.Status)
	}

	// Check uptime exists
	if response.Uptime == "" {
		t.Error("Uptime missing from response")
	}
}

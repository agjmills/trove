package auth

import (
	"testing"
	"time"

	"github.com/agjmills/trove/internal/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNewSessionManager(t *testing.T) {
	// Create in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create sessions table
	sqlDB, _ := db.DB()
	_, err = sqlDB.Exec(`CREATE TABLE sessions (
		token TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expiry TIMESTAMP NOT NULL
	)`)
	if err != nil {
		t.Fatalf("Failed to create sessions table: %v", err)
	}

	tests := []struct {
		name              string
		cfg               *config.Config
		expectedLifetime  time.Duration
		expectError       bool
	}{
		{
			name: "sqlite with valid duration",
			cfg: &config.Config{
				DBType:          "sqlite",
				SessionDuration: "24h",
				Env:             "development",
			},
			expectedLifetime: 24 * time.Hour,
			expectError:      false,
		},
		{
			name: "postgres with valid duration",
			cfg: &config.Config{
				DBType:          "postgres",
				SessionDuration: "168h",
				Env:             "production",
			},
			expectedLifetime: 168 * time.Hour,
			expectError:      false,
		},
		{
			name: "invalid duration falls back to default",
			cfg: &config.Config{
				DBType:          "sqlite",
				SessionDuration: "invalid",
				Env:             "development",
			},
			expectedLifetime: 168 * time.Hour, // Default
			expectError:      false,
		},
		{
			name: "empty duration falls back to default",
			cfg: &config.Config{
				DBType:          "sqlite",
				SessionDuration: "",
				Env:             "development",
			},
			expectedLifetime: 168 * time.Hour, // Default
			expectError:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm, err := NewSessionManager(db, tt.cfg)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if sm == nil {
				t.Fatal("Session manager should not be nil")
			}

			if sm.Lifetime != tt.expectedLifetime {
				t.Errorf("Expected lifetime %v, got %v", tt.expectedLifetime, sm.Lifetime)
			}

			if sm.Cookie.Name != "session_token" {
				t.Errorf("Expected cookie name 'session_token', got %q", sm.Cookie.Name)
			}

			if !sm.Cookie.HttpOnly {
				t.Error("Cookie should be HttpOnly")
			}

			if sm.Cookie.SameSite != 3 {
				t.Errorf("Expected SameSite=3 (Strict), got %d", sm.Cookie.SameSite)
			}

			expectedSecure := tt.cfg.Env == "production"
			if sm.Cookie.Secure != expectedSecure {
				t.Errorf("Expected Secure=%v for env=%s, got %v", 
					expectedSecure, tt.cfg.Env, sm.Cookie.Secure)
			}

			if sm.Store == nil {
				t.Error("Store should not be nil")
			}
		})
	}
}

func TestNewSessionManager_MemoryStoreFallback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	cfg := &config.Config{
		DBType:          "unknown", // Unsupported type
		SessionDuration: "24h",
		Env:             "development",
	}

	sm, err := NewSessionManager(db, cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if sm == nil {
		t.Fatal("Session manager should not be nil")
	}

	// Memory store is the default, Store field should still be set
	if sm.Store == nil {
		t.Error("Store should not be nil even with unknown DB type")
	}
}

func TestNewSessionManager_ParseDurationFormats(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create sessions table
	sqlDB, _ := db.DB()
	_, err = sqlDB.Exec(`CREATE TABLE sessions (
		token TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expiry TIMESTAMP NOT NULL
	)`)
	if err != nil {
		t.Fatalf("Failed to create sessions table: %v", err)
	}

	tests := []struct {
		duration string
		expected time.Duration
	}{
		{"1h", 1 * time.Hour},
		{"24h", 24 * time.Hour},
		{"168h", 168 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"720h", 720 * time.Hour}, // 30 days
	}

	for _, tt := range tests {
		t.Run(tt.duration, func(t *testing.T) {
			cfg := &config.Config{
				DBType:          "sqlite",
				SessionDuration: tt.duration,
				Env:             "development",
			}

			sm, err := NewSessionManager(db, cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if sm.Lifetime != tt.expected {
				t.Errorf("Expected lifetime %v, got %v", tt.expected, sm.Lifetime)
			}
		})
	}
}

func TestNewSessionManager_ProductionVsDevelopment(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create sessions table
	sqlDB, _ := db.DB()
	_, err = sqlDB.Exec(`CREATE TABLE sessions (
		token TEXT PRIMARY KEY,
		data BLOB NOT NULL,
		expiry TIMESTAMP NOT NULL
	)`)
	if err != nil {
		t.Fatalf("Failed to create sessions table: %v", err)
	}

	tests := []struct {
		env            string
		expectedSecure bool
	}{
		{"production", true},
		{"development", false},
		{"staging", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			cfg := &config.Config{
				DBType:          "sqlite",
				SessionDuration: "24h",
				Env:             tt.env,
			}

			sm, err := NewSessionManager(db, cfg)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if sm.Cookie.Secure != tt.expectedSecure {
				t.Errorf("Expected Secure=%v for env=%s, got %v",
					tt.expectedSecure, tt.env, sm.Cookie.Secure)
			}
		})
	}
}

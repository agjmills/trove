package storage

import (
	"testing"

	"github.com/agjmills/trove/internal/config"
)

func TestNewBackendFromConfig_Disk(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "disk",
		StoragePath:    t.TempDir(),
	}

	backend, err := NewBackendFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create disk backend: %v", err)
	}

	if backend == nil {
		t.Fatal("Backend should not be nil")
	}

	// Verify it's a disk backend
	_, ok := backend.(*DiskBackend)
	if !ok {
		t.Error("Backend should be a DiskBackend")
	}
}

func TestNewBackendFromConfig_DiskDefault(t *testing.T) {
	// Empty backend should default to disk
	cfg := &config.Config{
		StorageBackend: "",
		StoragePath:    t.TempDir(),
	}

	backend, err := NewBackendFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create disk backend: %v", err)
	}

	if backend == nil {
		t.Fatal("Backend should not be nil")
	}

	_, ok := backend.(*DiskBackend)
	if !ok {
		t.Error("Empty backend should default to DiskBackend")
	}
}

func TestNewBackendFromConfig_Memory(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "memory",
	}

	backend, err := NewBackendFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create memory backend: %v", err)
	}

	if backend == nil {
		t.Fatal("Backend should not be nil")
	}

	// Verify it's a memory backend
	_, ok := backend.(*MemoryBackend)
	if !ok {
		t.Error("Backend should be a MemoryBackend")
	}
}

func TestNewBackendFromConfig_S3(t *testing.T) {
	cfg := &config.Config{
		StorageBackend:   "s3",
		S3Bucket:         "test-bucket",
		S3UsePathStyle:   true,
	}

	backend, err := NewBackendFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to create S3 backend: %v", err)
	}

	if backend == nil {
		t.Fatal("Backend should not be nil")
	}

	// Verify it's an S3 backend
	_, ok := backend.(*S3Backend)
	if !ok {
		t.Error("Backend should be an S3Backend")
	}
}

func TestNewBackendFromConfig_InvalidBackend(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "invalid",
	}

	backend, err := NewBackendFromConfig(cfg)
	
	if err == nil {
		t.Error("Expected error for invalid backend")
	}

	if backend != nil {
		t.Error("Backend should be nil when error occurs")
	}

	// Check error message
	expectedMsg := "unknown storage backend: invalid"
	if err.Error() != expectedMsg + " (supported: disk, memory, s3)" {
		t.Errorf("Expected error message to contain %q, got %q", expectedMsg, err.Error())
	}
}

func TestNewBackendFromConfig_AllSupportedBackends(t *testing.T) {
	tests := []struct {
		name           string
		backendType    string
		expectError    bool
		expectedType   string
	}{
		{"disk backend", "disk", false, "*storage.DiskBackend"},
		{"empty (default disk)", "", false, "*storage.DiskBackend"},
		{"memory backend", "memory", false, "*storage.MemoryBackend"},
		{"s3 backend", "s3", false, "*storage.S3Backend"},
		{"unknown backend", "ftp", true, ""},
		{"unknown backend 2", "azure", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				StorageBackend: tt.backendType,
				StoragePath:    t.TempDir(),
				S3Bucket:       "test-bucket",
			}

			backend, err := NewBackendFromConfig(cfg)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				if backend != nil {
					t.Error("Backend should be nil when error occurs")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if backend == nil {
					t.Error("Backend should not be nil")
				}
			}
		})
	}
}

func TestNewBackendFromConfig_S3WithoutBucket(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "s3",
		S3Bucket:       "", // Missing bucket
	}

	backend, err := NewBackendFromConfig(cfg)
	
	// S3 backend creation should fail without a bucket
	if err == nil {
		t.Error("Expected error when S3 bucket is not configured")
	}

	// Note: Backend might not be nil even with error, depending on implementation
	_ = backend
}

func TestNewBackendFromConfig_DiskWithInvalidPath(t *testing.T) {
	cfg := &config.Config{
		StorageBackend: "disk",
		StoragePath:    "", // Empty path
	}

	backend, err := NewBackendFromConfig(cfg)
	
	// Disk backend should fail with empty path
	if err == nil {
		t.Error("Expected error when disk path is empty")
	}

	// Note: Backend might not be nil even with error, depending on implementation
	_ = backend
}

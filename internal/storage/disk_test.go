package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Note: "os" is still needed for os.Stat in some tests
// "errors" is still needed for errors.Is checks

func TestNewDiskBackend(t *testing.T) {
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "storage")

	backend, err := NewDiskBackend(storagePath)
	if err != nil {
		t.Fatalf("NewDiskBackend failed: %v", err)
	}
	defer backend.Close()

	if backend == nil {
		t.Fatal("NewDiskBackend returned nil backend")
	}

	// Verify directory was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("Storage directory was not created")
	}
}

func TestNewDiskBackend_CreatesNestedDirectories(t *testing.T) {
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "nested", "deep", "storage")

	backend, err := NewDiskBackend(storagePath)
	if err != nil {
		t.Fatalf("NewDiskBackend failed with nested path: %v", err)
	}
	defer backend.Close()

	if backend == nil {
		t.Fatal("NewDiskBackend returned nil backend")
	}

	// Verify nested directory was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("Nested storage directory was not created")
	}
}

func TestDiskBackend_Save(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	testContent := []byte("Hello, Trove!")
	reader := bytes.NewReader(testContent)

	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "test.txt",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify path has UUID format with extension
	if !strings.HasSuffix(result.Path, ".txt") {
		t.Errorf("Expected path to have .txt extension, got %s", result.Path)
	}

	// Verify size
	expectedSize := int64(len(testContent))
	if result.Size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, result.Size)
	}

	// Verify hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, result.Hash)
	}

	// Verify file exists on disk
	filePath := filepath.Join(tempDir, result.Path)
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("File was not saved to disk")
	}

	// Verify file content
	savedContent, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read saved file: %v", err)
	}

	if !bytes.Equal(savedContent, testContent) {
		t.Errorf("Saved content doesn't match. Expected %s, got %s", testContent, savedContent)
	}
}

func TestDiskBackend_Save_NoExtension(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	testContent := []byte("No extension file")
	reader := bytes.NewReader(testContent)

	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "README",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Should still generate a unique path even without extension
	if result.Path == "" {
		t.Error("Expected non-empty path")
	}

	// Verify file exists via Stat
	_, err = backend.Stat(context.Background(), result.Path)
	if err != nil {
		t.Errorf("File should exist after saving: %v", err)
	}
}

func TestDiskBackend_Save_LargeFile(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	// Create a 1MB file
	testContent := make([]byte, 1024*1024)
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	reader := bytes.NewReader(testContent)
	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "large.bin",
	})
	if err != nil {
		t.Fatalf("Save failed for large file: %v", err)
	}

	if result.Size != int64(len(testContent)) {
		t.Errorf("Expected size %d, got %d", len(testContent), result.Size)
	}

	// Verify hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Hash mismatch for large file")
	}

	// Verify file exists via Stat
	_, err = backend.Stat(context.Background(), result.Path)
	if err != nil {
		t.Errorf("Large file should exist after saving: %v", err)
	}
}

func TestDiskBackend_Delete(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Save a file first
	testContent := []byte("Delete me!")
	reader := bytes.NewReader(testContent)
	result, err := backend.Save(ctx, reader, SaveOptions{
		OriginalFilename: "delete.txt",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify it exists
	_, err = backend.Stat(ctx, result.Path)
	if err != nil {
		t.Fatal("File should exist before deletion")
	}

	// Delete the file
	err = backend.Delete(ctx, result.Path)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify it's gone
	_, err = backend.Stat(ctx, result.Path)
	if !errors.Is(err, ErrNotFound) {
		t.Error("File should not exist after deletion")
	}
}

func TestDiskBackend_Delete_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	// Try to delete a file that doesn't exist - should not error (idempotent)
	err = backend.Delete(context.Background(), "nonexistent.txt")
	if err != nil {
		t.Errorf("Delete should not error for non-existent file, got: %v", err)
	}
}

func TestDiskBackend_Stat(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Test non-existent file
	_, err = backend.Stat(ctx, "nonexistent.txt")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Stat should return ErrNotFound for non-existent file, got: %v", err)
	}

	// Save a file
	testContent := []byte("Stat me!")
	reader := bytes.NewReader(testContent)
	result, err := backend.Save(ctx, reader, SaveOptions{
		OriginalFilename: "stat.txt",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Test existing file
	info, err := backend.Stat(ctx, result.Path)
	if err != nil {
		t.Fatalf("Stat should succeed for existing file: %v", err)
	}

	if info.Path != result.Path {
		t.Errorf("Expected path %s, got %s", result.Path, info.Path)
	}

	if info.Size != result.Size {
		t.Errorf("Expected size %d, got %d", result.Size, info.Size)
	}

	if info.ModTime.IsZero() {
		t.Error("ModTime should not be zero")
	}
}

func TestDiskBackend_Open(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Save a file
	testContent := []byte("Open me!")
	reader := bytes.NewReader(testContent)
	result, err := backend.Save(ctx, reader, SaveOptions{
		OriginalFilename: "open.txt",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Open the file
	rc, err := backend.Open(ctx, result.Path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer rc.Close()

	// Read content
	content, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read opened file: %v", err)
	}

	if !bytes.Equal(content, testContent) {
		t.Errorf("Content mismatch. Expected %s, got %s", testContent, content)
	}
}

func TestDiskBackend_Open_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	// Try to open non-existent file
	_, err = backend.Open(context.Background(), "nonexistent.txt")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Open should return ErrNotFound for non-existent file, got: %v", err)
	}
}

func TestDiskBackend_HealthCheck(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	err = backend.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("HealthCheck should succeed: %v", err)
	}
}

func TestDiskBackend_ValidateAccess(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	err = backend.ValidateAccess(context.Background())
	if err != nil {
		t.Errorf("ValidateAccess should succeed: %v", err)
	}

	// Verify no test files were left behind
	files, _ := os.ReadDir(tempDir)
	for _, f := range files {
		if strings.HasPrefix(f.Name(), ".trove-access-test-") {
			t.Errorf("ValidateAccess left behind test file: %s", f.Name())
		}
	}
}

func TestDiskBackend_InterfaceCompliance(t *testing.T) {
	// This test ensures DiskBackend implements StorageBackend interface
	var _ StorageBackend = (*DiskBackend)(nil)
}

func TestDiskBackend_Save_MultipleConcurrent(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Save multiple files concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(n int) {
			content := []byte(strings.Repeat("x", n*100))
			reader := bytes.NewReader(content)
			_, err := backend.Save(ctx, reader, SaveOptions{
				OriginalFilename: "concurrent.txt",
			})
			if err != nil {
				t.Errorf("Concurrent Save failed: %v", err)
			}
			done <- true
		}(i + 1)
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestDiskBackend_CompleteWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	ctx := context.Background()

	// Complete workflow: save, verify exists, open, read, delete
	originalContent := []byte("Complete workflow test")
	reader := bytes.NewReader(originalContent)

	// 1. Save
	result, err := backend.Save(ctx, reader, SaveOptions{
		OriginalFilename: "workflow.txt",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// 2. Verify exists via Stat
	info, err := backend.Stat(ctx, result.Path)
	if err != nil {
		t.Fatalf("File should exist after saving: %v", err)
	}

	if info.Size != result.Size {
		t.Errorf("Size mismatch: stat=%d, save=%d", info.Size, result.Size)
	}

	// 3. Open and read
	rc, err := backend.Open(ctx, result.Path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	readContent, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !bytes.Equal(readContent, originalContent) {
		t.Error("Read content doesn't match original")
	}

	// 4. Verify hash matches
	hasher := sha256.New()
	hasher.Write(originalContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Error("Hash doesn't match expected")
	}

	// 5. Delete
	err = backend.Delete(ctx, result.Path)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// 6. Verify deleted
	_, err = backend.Stat(ctx, result.Path)
	if !errors.Is(err, ErrNotFound) {
		t.Error("File should not exist after deletion")
	}
}

// Note: Size limits are enforced at the HTTP handler level using http.MaxBytesReader,
// not in the storage layer. This provides proper connection handling when limits are exceeded.

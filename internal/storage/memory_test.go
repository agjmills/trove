package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestNewMemoryBackend(t *testing.T) {
	backend := NewMemoryBackend()
	if backend == nil {
		t.Fatal("NewMemoryBackend returned nil")
	}
	if backend.fs == nil {
		t.Fatal("MemoryBackend fs is nil")
	}
}

func TestMemoryBackend_Save(t *testing.T) {
	backend := NewMemoryBackend()

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

	// Verify file exists via Stat
	_, err = backend.Stat(context.Background(), result.Path)
	if err != nil {
		t.Errorf("File should exist after saving: %v", err)
	}
}

func TestMemoryBackend_Save_NoExtension(t *testing.T) {
	backend := NewMemoryBackend()

	testContent := []byte("No extension file")
	reader := bytes.NewReader(testContent)

	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "README",
	})
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if result.Path == "" {
		t.Error("Expected non-empty path")
	}

	// Verify file exists
	_, err = backend.Stat(context.Background(), result.Path)
	if err != nil {
		t.Errorf("File should exist after saving: %v", err)
	}
}

func TestMemoryBackend_Save_LargeFile(t *testing.T) {
	backend := NewMemoryBackend()

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
}

func TestMemoryBackend_Delete(t *testing.T) {
	backend := NewMemoryBackend()
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

func TestMemoryBackend_Delete_NonExistent(t *testing.T) {
	backend := NewMemoryBackend()

	// Try to delete a file that doesn't exist - should not error (idempotent)
	err := backend.Delete(context.Background(), "nonexistent.txt")
	if err != nil {
		t.Errorf("Delete should not error for non-existent file, got: %v", err)
	}
}

func TestMemoryBackend_Stat(t *testing.T) {
	backend := NewMemoryBackend()
	ctx := context.Background()

	// Test non-existent file
	_, err := backend.Stat(ctx, "nonexistent.txt")
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
}

func TestMemoryBackend_Open(t *testing.T) {
	backend := NewMemoryBackend()
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

func TestMemoryBackend_Open_NonExistent(t *testing.T) {
	backend := NewMemoryBackend()

	// Try to open non-existent file
	_, err := backend.Open(context.Background(), "nonexistent.txt")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Open should return ErrNotFound for non-existent file, got: %v", err)
	}
}

func TestMemoryBackend_HealthCheck(t *testing.T) {
	backend := NewMemoryBackend()

	err := backend.HealthCheck(context.Background())
	if err != nil {
		t.Errorf("HealthCheck should always succeed for memory backend: %v", err)
	}
}

func TestMemoryBackend_ValidateAccess(t *testing.T) {
	backend := NewMemoryBackend()

	err := backend.ValidateAccess(context.Background())
	if err != nil {
		t.Errorf("ValidateAccess should always succeed for memory backend: %v", err)
	}
}

func TestMemoryBackend_InterfaceCompliance(t *testing.T) {
	// This test ensures MemoryBackend implements StorageBackend interface
	var _ StorageBackend = (*MemoryBackend)(nil)
}

func TestMemoryBackend_Save_MultipleConcurrent(t *testing.T) {
	backend := NewMemoryBackend()
	ctx := context.Background()

	// Save multiple files concurrently
	var wg sync.WaitGroup
	errChan := make(chan error, 5)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			content := []byte(strings.Repeat("x", n*100))
			reader := bytes.NewReader(content)
			_, err := backend.Save(ctx, reader, SaveOptions{
				OriginalFilename: "concurrent.txt",
			})
			if err != nil {
				errChan <- err
			}
		}(i + 1)
	}

	// Wait for all goroutines
	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent Save failed: %v", err)
	}

	// Verify 5 files were saved
	count := backend.FileCount()
	if count != 5 {
		t.Errorf("Expected 5 files, got %d", count)
	}
}

func TestMemoryBackend_CompleteWorkflow(t *testing.T) {
	backend := NewMemoryBackend()
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

func TestMemoryBackend_Clear(t *testing.T) {
	backend := NewMemoryBackend()
	ctx := context.Background()

	// Save some files
	for i := 0; i < 3; i++ {
		content := []byte("test content")
		reader := bytes.NewReader(content)
		_, err := backend.Save(ctx, reader, SaveOptions{
			OriginalFilename: "test.txt",
		})
		if err != nil {
			t.Fatalf("Save failed: %v", err)
		}
	}

	// Verify files exist
	if backend.FileCount() != 3 {
		t.Errorf("Expected 3 files, got %d", backend.FileCount())
	}

	// Clear
	backend.Clear()

	// Verify all files are gone
	if backend.FileCount() != 0 {
		t.Errorf("Expected 0 files after Clear, got %d", backend.FileCount())
	}
}

package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewService(t *testing.T) {
	// Create a temporary directory for tests
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "storage")

	service, err := NewService(storagePath)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	if service == nil {
		t.Fatal("NewService returned nil service")
	}

	if service.basePath != storagePath {
		t.Errorf("Expected basePath %s, got %s", storagePath, service.basePath)
	}

	// Verify directory was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("Storage directory was not created")
	}
}

func TestNewService_CreatesNestedDirectories(t *testing.T) {
	tempDir := t.TempDir()
	storagePath := filepath.Join(tempDir, "nested", "deep", "storage")

	service, err := NewService(storagePath)
	if err != nil {
		t.Fatalf("NewService failed with nested path: %v", err)
	}

	if service == nil {
		t.Fatal("NewService returned nil service")
	}

	// Verify nested directory was created
	if _, err := os.Stat(storagePath); os.IsNotExist(err) {
		t.Error("Nested storage directory was not created")
	}
}

func TestSaveFile(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	testContent := []byte("Hello, Trove!")
	reader := bytes.NewReader(testContent)
	originalFilename := "test.txt"

	filename, hash, size, err := service.SaveFile(reader, originalFilename)
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Verify filename has UUID format with extension
	if !strings.HasSuffix(filename, ".txt") {
		t.Errorf("Expected filename to have .txt extension, got %s", filename)
	}

	// Verify size
	expectedSize := int64(len(testContent))
	if size != expectedSize {
		t.Errorf("Expected size %d, got %d", expectedSize, size)
	}

	// Verify hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash)
	}

	// Verify file exists on disk
	filePath := filepath.Join(tempDir, filename)
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

func TestSaveFile_NoExtension(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	testContent := []byte("No extension file")
	reader := bytes.NewReader(testContent)

	filename, _, _, err := service.SaveFile(reader, "README")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Should still generate a unique filename even without extension
	if filename == "" {
		t.Error("Expected non-empty filename")
	}

	// Verify file exists
	if !service.FileExists(filename) {
		t.Error("File should exist after saving")
	}
}

func TestSaveFile_LargeFile(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Create a 1MB file
	testContent := make([]byte, 1024*1024)
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	reader := bytes.NewReader(testContent)
	filename, hash, size, err := service.SaveFile(reader, "large.bin")
	if err != nil {
		t.Fatalf("SaveFile failed for large file: %v", err)
	}

	if size != int64(len(testContent)) {
		t.Errorf("Expected size %d, got %d", len(testContent), size)
	}

	// Verify hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if hash != expectedHash {
		t.Errorf("Hash mismatch for large file")
	}

	// Verify file on disk
	if !service.FileExists(filename) {
		t.Error("Large file should exist after saving")
	}
}

func TestDeleteFile(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Save a file first
	testContent := []byte("Delete me!")
	reader := bytes.NewReader(testContent)
	filename, _, _, err := service.SaveFile(reader, "delete.txt")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Verify it exists
	if !service.FileExists(filename) {
		t.Fatal("File should exist before deletion")
	}

	// Delete the file
	err = service.DeleteFile(filename)
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// Verify it's gone
	if service.FileExists(filename) {
		t.Error("File should not exist after deletion")
	}
}

func TestDeleteFile_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Try to delete a file that doesn't exist - should not error
	err = service.DeleteFile("nonexistent.txt")
	if err != nil {
		t.Errorf("DeleteFile should not error for non-existent file, got: %v", err)
	}
}

func TestGetFilePath(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	filename := "test.txt"
	expectedPath := filepath.Join(tempDir, filename)

	actualPath := service.GetFilePath(filename)
	if actualPath != expectedPath {
		t.Errorf("Expected path %s, got %s", expectedPath, actualPath)
	}
}

func TestFileExists(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Test non-existent file
	if service.FileExists("nonexistent.txt") {
		t.Error("FileExists should return false for non-existent file")
	}

	// Save a file
	testContent := []byte("Exists!")
	reader := bytes.NewReader(testContent)
	filename, _, _, err := service.SaveFile(reader, "exists.txt")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Test existing file
	if !service.FileExists(filename) {
		t.Error("FileExists should return true for existing file")
	}
}

func TestOpenFile(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Save a file
	testContent := []byte("Open me!")
	reader := bytes.NewReader(testContent)
	filename, _, _, err := service.SaveFile(reader, "open.txt")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// Open the file
	file, err := service.OpenFile(filename)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}
	defer file.Close()

	// Read content
	content, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatalf("Failed to read opened file: %v", err)
	}

	if !bytes.Equal(content, testContent) {
		t.Errorf("Content mismatch. Expected %s, got %s", testContent, content)
	}
}

func TestOpenFile_NonExistent(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Try to open non-existent file
	_, err = service.OpenFile("nonexistent.txt")
	if err == nil {
		t.Error("OpenFile should error for non-existent file")
	}
}

func TestCalculateHash(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	testContent := []byte("Hash this!")
	reader := bytes.NewReader(testContent)

	hash, err := service.CalculateHash(reader)
	if err != nil {
		t.Fatalf("CalculateHash failed: %v", err)
	}

	// Verify hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	if hash != expectedHash {
		t.Errorf("Expected hash %s, got %s", expectedHash, hash)
	}
}

func TestCalculateHash_EmptyContent(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	testContent := []byte("")
	reader := bytes.NewReader(testContent)

	hash, err := service.CalculateHash(reader)
	if err != nil {
		t.Fatalf("CalculateHash failed for empty content: %v", err)
	}

	// Verify empty string hash
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))

	if hash != expectedHash {
		t.Errorf("Expected hash %s for empty content, got %s", expectedHash, hash)
	}
}

func TestStorageBackend_InterfaceCompliance(t *testing.T) {
	// This test ensures Service implements StorageBackend interface
	var _ StorageBackend = (*Service)(nil)
}

func TestSaveFile_MultipleConcurrent(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Save multiple files concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func(n int) {
			content := []byte(strings.Repeat("x", n*100))
			reader := bytes.NewReader(content)
			_, _, _, err := service.SaveFile(reader, "concurrent.txt")
			if err != nil {
				t.Errorf("Concurrent SaveFile failed: %v", err)
			}
			done <- true
		}(i + 1)
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestSaveAndRetrieveWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	service, err := NewService(tempDir)
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	// Complete workflow: save, verify exists, open, read, delete
	originalContent := []byte("Complete workflow test")
	reader := bytes.NewReader(originalContent)

	// 1. Save
	filename, hash, size, err := service.SaveFile(reader, "workflow.txt")
	if err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	// 2. Verify exists
	if !service.FileExists(filename) {
		t.Fatal("File should exist after saving")
	}

	// 3. Open and read
	file, err := service.OpenFile(filename)
	if err != nil {
		t.Fatalf("OpenFile failed: %v", err)
	}

	readContent := make([]byte, size)
	_, err = file.Read(readContent)
	file.Close()
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	if !bytes.Equal(readContent, originalContent) {
		t.Error("Read content doesn't match original")
	}

	// 4. Verify hash matches
	hashReader := bytes.NewReader(originalContent)
	calculatedHash, err := service.CalculateHash(hashReader)
	if err != nil {
		t.Fatalf("CalculateHash failed: %v", err)
	}

	if calculatedHash != hash {
		t.Error("Calculated hash doesn't match saved hash")
	}

	// 5. Delete
	err = service.DeleteFile(filename)
	if err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	// 6. Verify deleted
	if service.FileExists(filename) {
		t.Error("File should not exist after deletion")
	}
}

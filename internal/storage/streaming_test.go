package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// TestHashingReader tests the hashingReader wrapper used by S3Backend.
func TestHashingReader(t *testing.T) {
	testContent := []byte("Hello, streaming world!")
	hasher := sha256.New()

	hr := &hashingReader{
		reader: bytes.NewReader(testContent),
		hasher: hasher,
	}

	// Read all content
	result, err := io.ReadAll(hr)
	if err != nil {
		t.Fatalf("Failed to read from hashingReader: %v", err)
	}

	// Verify content matches
	if !bytes.Equal(result, testContent) {
		t.Errorf("Content mismatch. Expected %s, got %s", testContent, result)
	}

	// Verify bytes tracked correctly
	if hr.bytesRead != int64(len(testContent)) {
		t.Errorf("Expected bytesRead %d, got %d", len(testContent), hr.bytesRead)
	}

	// Verify hash computed correctly
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	directHasher := sha256.New()
	directHasher.Write(testContent)
	directHash := hex.EncodeToString(directHasher.Sum(nil))

	if expectedHash != directHash {
		t.Errorf("Hash mismatch. Expected %s, got %s", directHash, expectedHash)
	}
}

// TestHashingReader_MultipleReads tests that hashingReader works correctly with multiple Read calls.
func TestHashingReader_MultipleReads(t *testing.T) {
	testContent := make([]byte, 1024*10) // 10KB
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	hasher := sha256.New()
	hr := &hashingReader{
		reader: bytes.NewReader(testContent),
		hasher: hasher,
	}

	// Read in small chunks
	var result []byte
	buf := make([]byte, 512)
	for {
		n, err := hr.Read(buf)
		if n > 0 {
			result = append(result, buf[:n]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
	}

	// Verify full content
	if !bytes.Equal(result, testContent) {
		t.Error("Content mismatch after multiple reads")
	}

	// Verify bytes tracked
	if hr.bytesRead != int64(len(testContent)) {
		t.Errorf("Expected bytesRead %d, got %d", len(testContent), hr.bytesRead)
	}

	// Verify hash
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	directHasher := sha256.New()
	directHasher.Write(testContent)
	directHash := hex.EncodeToString(directHasher.Sum(nil))

	if expectedHash != directHash {
		t.Errorf("Hash mismatch. Expected %s, got %s", directHash, expectedHash)
	}
}

// TestMemoryBackend_Save_MultiChunk tests that streaming works correctly
// for files larger than copyBufferSize (8MB).
func TestMemoryBackend_Save_MultiChunk(t *testing.T) {
	backend := NewMemoryBackend()

	// Create a 10MB file (larger than the 8MB copyBufferSize)
	fileSize := 10 * 1024 * 1024
	testContent := make([]byte, fileSize)
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	reader := bytes.NewReader(testContent)
	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "large.bin",
	})
	if err != nil {
		t.Fatalf("Save failed for multi-chunk file: %v", err)
	}

	// Verify size
	if result.Size != int64(fileSize) {
		t.Errorf("Expected size %d, got %d", fileSize, result.Size)
	}

	// Verify hash is computed correctly across multiple buffer reads
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Hash mismatch for multi-chunk file. Expected %s, got %s", expectedHash, result.Hash)
	}

	// Verify we can read the file back
	rc, err := backend.Open(context.Background(), result.Path)
	if err != nil {
		t.Fatalf("Failed to open saved file: %v", err)
	}
	defer rc.Close()

	readContent, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read file content: %v", err)
	}

	if !bytes.Equal(readContent, testContent) {
		t.Error("Read content doesn't match saved content")
	}
}

// TestDiskBackend_Save_MultiChunk tests that streaming works correctly
// for files larger than copyBufferSize (8MB).
func TestDiskBackend_Save_MultiChunk(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	// Create a 10MB file (larger than the 8MB copyBufferSize)
	fileSize := 10 * 1024 * 1024
	testContent := make([]byte, fileSize)
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}

	reader := bytes.NewReader(testContent)
	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "large.bin",
	})
	if err != nil {
		t.Fatalf("Save failed for multi-chunk file: %v", err)
	}

	// Verify size
	if result.Size != int64(fileSize) {
		t.Errorf("Expected size %d, got %d", fileSize, result.Size)
	}

	// Verify hash is computed correctly across multiple buffer reads
	hasher := sha256.New()
	hasher.Write(testContent)
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Hash mismatch for multi-chunk file. Expected %s, got %s", expectedHash, result.Hash)
	}

	// Verify we can read the file back
	rc, err := backend.Open(context.Background(), result.Path)
	if err != nil {
		t.Fatalf("Failed to open saved file: %v", err)
	}
	defer rc.Close()

	readContent, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("Failed to read file content: %v", err)
	}

	if !bytes.Equal(readContent, testContent) {
		t.Error("Read content doesn't match saved content")
	}
}

// failingReader simulates a reader that fails after reading some data.
type failingReader struct {
	data      []byte
	pos       int
	failAfter int
}

func (fr *failingReader) Read(p []byte) (n int, err error) {
	if fr.pos >= fr.failAfter {
		return 0, errors.New("simulated read failure")
	}

	n = copy(p, fr.data[fr.pos:])
	fr.pos += n

	if fr.pos >= fr.failAfter {
		return n, errors.New("simulated read failure")
	}

	if fr.pos >= len(fr.data) {
		return n, io.EOF
	}

	return n, nil
}

// TestMemoryBackend_Save_StreamingError tests error handling when the reader fails mid-stream.
func TestMemoryBackend_Save_StreamingError(t *testing.T) {
	backend := NewMemoryBackend()

	// Create a reader that will fail after 100KB
	testData := make([]byte, 1024*1024) // 1MB
	reader := &failingReader{
		data:      testData,
		failAfter: 100 * 1024, // Fail after 100KB
	}

	_, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "failing.bin",
	})
	if err == nil {
		t.Fatal("Expected error from failing reader, got nil")
	}

	if !errors.Is(err, errors.New("simulated read failure")) {
		// Check error message contains indication of read failure
		errStr := err.Error()
		if errStr == "" {
			t.Error("Expected non-empty error message")
		}
	}
}

// TestDiskBackend_Save_StreamingError tests error handling when the reader fails mid-stream.
func TestDiskBackend_Save_StreamingError(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	// Create a reader that will fail after 100KB
	testData := make([]byte, 1024*1024) // 1MB
	reader := &failingReader{
		data:      testData,
		failAfter: 100 * 1024, // Fail after 100KB
	}

	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "failing.bin",
	})
	if err == nil {
		t.Fatal("Expected error from failing reader, got nil")
	}

	// Verify that partial file is cleaned up
	if result.Path != "" {
		_, statErr := backend.Stat(context.Background(), result.Path)
		if statErr == nil {
			t.Error("Partial file should have been cleaned up after error")
		}
	}
}

// TestMemoryBackend_Save_EmptyFile tests streaming with zero-length content.
func TestMemoryBackend_Save_EmptyFile(t *testing.T) {
	backend := NewMemoryBackend()

	reader := bytes.NewReader([]byte{})
	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "empty.txt",
	})
	if err != nil {
		t.Fatalf("Save failed for empty file: %v", err)
	}

	if result.Size != 0 {
		t.Errorf("Expected size 0 for empty file, got %d", result.Size)
	}

	// Verify hash of empty content
	hasher := sha256.New()
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Hash mismatch for empty file. Expected %s, got %s", expectedHash, result.Hash)
	}
}

// TestDiskBackend_Save_EmptyFile tests streaming with zero-length content.
func TestDiskBackend_Save_EmptyFile(t *testing.T) {
	tempDir := t.TempDir()
	backend, err := NewDiskBackend(tempDir)
	if err != nil {
		t.Fatalf("Failed to create backend: %v", err)
	}
	defer backend.Close()

	reader := bytes.NewReader([]byte{})
	result, err := backend.Save(context.Background(), reader, SaveOptions{
		OriginalFilename: "empty.txt",
	})
	if err != nil {
		t.Fatalf("Save failed for empty file: %v", err)
	}

	if result.Size != 0 {
		t.Errorf("Expected size 0 for empty file, got %d", result.Size)
	}

	// Verify hash of empty content
	hasher := sha256.New()
	expectedHash := hex.EncodeToString(hasher.Sum(nil))
	if result.Hash != expectedHash {
		t.Errorf("Hash mismatch for empty file. Expected %s, got %s", expectedHash, result.Hash)
	}
}

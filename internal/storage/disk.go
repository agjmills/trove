package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// DiskBackend implements StorageBackend using the local filesystem.
// It uses os.Root (Go 1.23+) for sandboxed file operations, preventing path traversal attacks.
type DiskBackend struct {
	root     *os.Root
	basePath string // stored for ValidateAccess logging
}

// NewDiskBackend creates a new disk-based storage backend.
// The basePath directory will be created if it doesn't exist.
// All file operations are sandboxed to this directory using os.Root.
func NewDiskBackend(basePath string) (*DiskBackend, error) {
	// Create directory if needed
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Open as sandboxed root
	root, err := os.OpenRoot(basePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open storage root: %w", err)
	}

	return &DiskBackend{
		root:     root,
		basePath: basePath,
	}, nil
}

// Save stores content and returns the generated path, hash, and size.
// Size limits should be enforced at the HTTP handler level using http.MaxBytesReader.
func (d *DiskBackend) Save(ctx context.Context, r io.Reader, opts SaveOptions) (SaveResult, error) {
	// Generate unique filename
	ext := filepath.Ext(opts.OriginalFilename)
	filename := uuid.New().String() + ext

	// Create file using sandboxed root
	file, err := d.root.Create(filename)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Hash while writing using large buffer for better throughput
	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	buf := make([]byte, copyBufferSize)

	size, err := io.CopyBuffer(writer, r, buf)
	if err != nil {
		d.root.Remove(filename) // Clean up on error
		return SaveResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return SaveResult{
		Path: filename,
		Hash: hex.EncodeToString(hasher.Sum(nil)),
		Size: size,
	}, nil
}

// Open returns a reader for the file at the given path.
func (d *DiskBackend) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	file, err := d.root.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

// Delete removes a file. Returns nil if file doesn't exist (idempotent).
func (d *DiskBackend) Delete(ctx context.Context, path string) error {
	if err := d.root.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// Stat returns file metadata without opening it.
func (d *DiskBackend) Stat(ctx context.Context, path string) (FileInfo, error) {
	info, err := d.root.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileInfo{}, ErrNotFound
		}
		return FileInfo{}, fmt.Errorf("failed to stat file: %w", err)
	}

	return FileInfo{
		Path:    path,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// HealthCheck verifies the backend is reachable (cheap, safe for frequent polling).
func (d *DiskBackend) HealthCheck(ctx context.Context) error {
	// Check that we can stat the root directory
	_, err := d.root.Stat(".")
	if err != nil {
		return fmt.Errorf("storage health check failed: %w", err)
	}
	return nil
}

// ValidateAccess performs a full read/write/delete test.
func (d *DiskBackend) ValidateAccess(ctx context.Context) error {
	testFilename := ".trove-access-test-" + uuid.New().String()
	testContent := []byte("trove-storage-test")

	// Test write
	file, err := d.root.Create(testFilename)
	if err != nil {
		return fmt.Errorf("storage write test failed: %w", err)
	}
	if _, err := file.Write(testContent); err != nil {
		file.Close()
		d.root.Remove(testFilename)
		return fmt.Errorf("storage write test failed: %w", err)
	}
	file.Close()

	// Test read
	readFile, err := d.root.Open(testFilename)
	if err != nil {
		d.root.Remove(testFilename)
		return fmt.Errorf("storage read test failed: %w", err)
	}
	readContent, err := io.ReadAll(readFile)
	readFile.Close()
	if err != nil {
		d.root.Remove(testFilename)
		return fmt.Errorf("storage read test failed: %w", err)
	}
	if !bytes.Equal(readContent, testContent) {
		d.root.Remove(testFilename)
		return fmt.Errorf("storage read test failed: content mismatch")
	}

	// Test delete
	if err := d.root.Remove(testFilename); err != nil {
		return fmt.Errorf("storage delete test failed: %w", err)
	}

	return nil
}

// Close releases resources held by the backend.
func (d *DiskBackend) Close() error {
	return d.root.Close()
}

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/liamg/memoryfs"
)

// MemoryBackend implements StorageBackend using an in-memory filesystem.
// Useful for integration testing without disk I/O.
// Thread-safe for concurrent use.
type MemoryBackend struct {
	fs *memoryfs.FS
	mu sync.RWMutex // Protects fs operations
}

// NewMemoryBackend creates a new in-memory storage backend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		fs: memoryfs.New(),
	}
}

// Save stores content and returns the generated path, hash, and size.
func (m *MemoryBackend) Save(ctx context.Context, r io.Reader, opts SaveOptions) (SaveResult, error) {
	// Generate unique filename
	ext := filepath.Ext(opts.OriginalFilename)
	filename := uuid.New().String() + ext

	// Stream content into buffer while computing hash (avoids reading entire payload upfront)
	// memoryfs.WriteFile requires complete content, so we still need to buffer,
	// but we use io.CopyBuffer with the shared copyBufferSize for consistency
	hasher := sha256.New()
	var buf bytes.Buffer
	writer := io.MultiWriter(&buf, hasher)

	copyBuf := make([]byte, copyBufferSize)
	size, err := io.CopyBuffer(writer, r, copyBuf)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to read content: %w", err)
	}

	// Write to memoryfs
	m.mu.Lock()
	err = m.fs.WriteFile(filename, buf.Bytes(), 0644)
	m.mu.Unlock()
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return SaveResult{
		Path: filename,
		Hash: hex.EncodeToString(hasher.Sum(nil)),
		Size: size,
	}, nil
}

// Open returns a reader for the file at the given path.
func (m *MemoryBackend) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	m.mu.RLock()
	content, err := m.fs.ReadFile(path)
	m.mu.RUnlock()
	if err != nil {
		if isNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return io.NopCloser(bytes.NewReader(content)), nil
}

// Delete removes a file. Returns nil if file doesn't exist (idempotent).
func (m *MemoryBackend) Delete(ctx context.Context, path string) error {
	m.mu.Lock()
	err := m.fs.Remove(path)
	m.mu.Unlock()
	if err != nil && !isNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// Stat returns file metadata without opening it.
func (m *MemoryBackend) Stat(ctx context.Context, path string) (FileInfo, error) {
	m.mu.RLock()
	info, err := m.fs.Stat(path)
	m.mu.RUnlock()
	if err != nil {
		if isNotExist(err) {
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

// HealthCheck verifies the backend is reachable.
// For memory backend, always returns nil (no external dependencies).
func (m *MemoryBackend) HealthCheck(ctx context.Context) error {
	return nil
}

// ValidateAccess performs a full read/write/delete test.
// For memory backend, always returns nil (no permission issues possible).
func (m *MemoryBackend) ValidateAccess(ctx context.Context) error {
	return nil
}

// Clear removes all files from the memory backend.
// Useful for test cleanup.
func (m *MemoryBackend) Clear() {
	m.mu.Lock()
	m.fs = memoryfs.New()
	m.mu.Unlock()
}

// FileCount returns the number of files currently stored.
// Useful for testing.
func (m *MemoryBackend) FileCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	entries, err := m.fs.ReadDir(".")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count
}

// isNotExist checks if an error indicates the file doesn't exist.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	// memoryfs wraps errors, so check the error message
	errStr := err.Error()
	return strings.Contains(errStr, "file does not exist") ||
		strings.Contains(errStr, "no such file")
}

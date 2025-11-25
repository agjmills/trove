package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

// Errors returned by storage backends.
var (
	ErrNotFound = errors.New("storage: file not found")
)

// copyBufferSize is the buffer size used for file copies (8MB aligns with S3 multipart upload parts).
const copyBufferSize = 8 * 1024 * 1024

// StorageBackend defines the contract for file storage operations.
// All implementations must be safe for concurrent use.
// All paths are relative to the backend's configured root/prefix.
//
// Size limits should be enforced at the HTTP handler level using http.MaxBytesReader,
// which properly closes connections when limits are exceeded.
type StorageBackend interface {
	// Save stores content and returns the generated path, hash, and size.
	// The backend generates a unique path (e.g., "{uuid}.bin").
	// The returned path is relative to the backend root and should be stored in DB.
	Save(ctx context.Context, r io.Reader, opts SaveOptions) (SaveResult, error)

	// Open returns a reader for the file at the given path.
	// Caller must call Close() on the returned reader.
	// Returns ErrNotFound if the path does not exist.
	Open(ctx context.Context, path string) (io.ReadCloser, error)

	// Delete removes a file. Returns nil if file doesn't exist (idempotent).
	Delete(ctx context.Context, path string) error

	// Stat returns file metadata without opening it.
	// Returns ErrNotFound if the path does not exist.
	// Use this for existence checks: _, err := backend.Stat(ctx, path)
	Stat(ctx context.Context, path string) (FileInfo, error)

	// HealthCheck verifies the backend is reachable (cheap, safe for frequent polling).
	// For thorough read/write validation, use ValidateAccess at startup.
	HealthCheck(ctx context.Context) error

	// ValidateAccess performs a full read/write/delete test.
	// Call once at startup to fail fast on permission issues. Too expensive for frequent use.
	ValidateAccess(ctx context.Context) error
}

// SaveOptions configures file saving.
type SaveOptions struct {
	OriginalFilename string // Used to extract extension for generated path
	ContentType      string // MIME type (optional, for S3 metadata)
}

// SaveResult contains the result of a save operation.
type SaveResult struct {
	Path string // Generated storage path (e.g., "abc123.bin") - store this in DB
	Hash string // SHA-256 hex-encoded
	Size int64  // Bytes stored
}

// FileInfo contains file metadata.
type FileInfo struct {
	Path        string
	Size        int64
	ModTime     time.Time
	ContentType string
}

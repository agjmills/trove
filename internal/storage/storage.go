package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

// ErrFileTooLarge is returned when an upload exceeds the maximum allowed size
var ErrFileTooLarge = errors.New("file exceeds maximum allowed size")

// copyBufferSize is the buffer size used for file copies (8MB aligns with S3 multipart upload parts)
const copyBufferSize = 8 * 1024 * 1024

type Service struct {
	basePath string
}

// StorageBackend defines the behavior required by the application for storing files.
// This allows swapping implementations (local FS, S3, etc.) while keeping the
// rest of the codebase implementation-agnostic.
type StorageBackend interface {
	SaveFile(reader io.Reader, originalFilename string) (filename string, hash string, size int64, err error)
	SaveFileWithLimit(reader io.Reader, originalFilename string, maxSize int64) (filename string, hash string, size int64, err error)
	DeleteFile(filename string) error
	GetFilePath(filename string) string
	FileExists(filename string) bool
	OpenFile(filename string) (*os.File, error)
	CalculateHash(reader io.Reader) (string, error)
}

func NewService(basePath string) (*Service, error) {
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}
	return &Service{basePath: basePath}, nil
}

// SaveFile saves a file to disk with a unique filename and returns the stored filename and hash
func (s *Service) SaveFile(reader io.Reader, originalFilename string) (filename string, hash string, size int64, err error) {
	// Generate unique filename
	ext := filepath.Ext(originalFilename)
	filename = uuid.New().String() + ext

	filePath := filepath.Join(s.basePath, filename)

	// Create file
	file, err := os.Create(filePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Hash while writing using large buffer for better throughput
	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	buf := make([]byte, copyBufferSize)

	size, err = io.CopyBuffer(writer, reader, buf)
	if err != nil {
		os.Remove(filePath) // Clean up on error
		return "", "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	hash = hex.EncodeToString(hasher.Sum(nil))

	return filename, hash, size, nil
}

// DeleteFile removes a file from disk
func (s *Service) DeleteFile(filename string) error {
	filePath := filepath.Join(s.basePath, filename)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

// GetFilePath returns the full path to a stored file
func (s *Service) GetFilePath(filename string) string {
	return filepath.Join(s.basePath, filename)
}

// FileExists checks if a file exists on disk
func (s *Service) FileExists(filename string) bool {
	filePath := filepath.Join(s.basePath, filename)
	_, err := os.Stat(filePath)
	return err == nil
}

// OpenFile opens a file for reading
func (s *Service) OpenFile(filename string) (*os.File, error) {
	filePath := filepath.Join(s.basePath, filename)
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	return file, nil
}

// CalculateHash calculates the SHA-256 hash of a reader's content
func (s *Service) CalculateHash(reader io.Reader) (string, error) {
	hasher := sha256.New()
	buf := make([]byte, copyBufferSize)
	if _, err := io.CopyBuffer(hasher, reader, buf); err != nil {
		return "", fmt.Errorf("failed to calculate hash: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// limitedReader wraps an io.Reader and tracks bytes read, erroring if limit is exceeded
type limitedReader struct {
	reader    io.Reader
	remaining int64
	exceeded  bool
	done      bool
}

func (lr *limitedReader) Read(p []byte) (n int, err error) {
	if lr.done {
		return 0, io.EOF
	}

	if lr.remaining <= 0 {
		lr.exceeded = true
		return 0, ErrFileTooLarge
	}

	// Limit read to remaining bytes
	if int64(len(p)) > lr.remaining {
		p = p[:lr.remaining]
	}

	n, err = lr.reader.Read(p)
	lr.remaining -= int64(n)

	if lr.remaining <= 0 && err == nil {
		// Check if there's more data (we've hit the limit)
		var probe [1]byte
		probeN, probeErr := lr.reader.Read(probe[:])
		if probeN > 0 || (probeErr != nil && probeErr != io.EOF) {
			lr.exceeded = true
			return n, ErrFileTooLarge
		}
		// No more data - we read exactly the limit, signal EOF
		lr.done = true
		return n, io.EOF
	}

	return n, err
}

// SaveFileWithLimit saves a file to disk with a size limit, returning early if exceeded.
// This enables terminating oversized uploads without writing the entire file.
func (s *Service) SaveFileWithLimit(reader io.Reader, originalFilename string, maxSize int64) (filename string, hash string, size int64, err error) {
	// Generate unique filename
	ext := filepath.Ext(originalFilename)
	filename = uuid.New().String() + ext

	filePath := filepath.Join(s.basePath, filename)

	// Create file
	file, err := os.Create(filePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Wrap reader with size limit
	lr := &limitedReader{reader: reader, remaining: maxSize}

	// Hash while writing using large buffer for better throughput
	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	buf := make([]byte, copyBufferSize)

	size, err = io.CopyBuffer(writer, lr, buf)
	if err != nil {
		os.Remove(filePath) // Clean up on error
		if lr.exceeded {
			return "", "", 0, ErrFileTooLarge
		}
		return "", "", 0, fmt.Errorf("failed to write file: %w", err)
	}

	hash = hex.EncodeToString(hasher.Sum(nil))

	return filename, hash, size, nil
}

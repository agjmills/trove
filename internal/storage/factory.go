package storage

import (
	"fmt"

	"github.com/agjmills/trove/internal/config"
)

// NewBackendFromConfig creates a StorageBackend based on the configuration.
// Supported backends:
//   - "disk": Local filesystem storage (default)
//   - "memory": In-memory storage for testing
//   - "s3": AWS S3 or compatible storage (e.g., rustfs, MinIO)
func NewBackendFromConfig(cfg *config.Config) (StorageBackend, error) {
	switch cfg.StorageBackend {
	case "disk", "":
		return NewDiskBackend(cfg.StoragePath)
	case "memory":
		return NewMemoryBackend(), nil
	case "s3":
		return NewS3Backend(S3Config{
			Endpoint:     cfg.S3Endpoint,
			Region:       cfg.S3Region,
			Bucket:       cfg.S3Bucket,
			AccessKey:    cfg.S3AccessKey,
			SecretKey:    cfg.S3SecretKey,
			UsePathStyle: cfg.S3UsePathStyle,
		})
	default:
		return nil, fmt.Errorf("unknown storage backend: %s (supported: disk, memory, s3)", cfg.StorageBackend)
	}
}

package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// S3Config holds configuration for the S3 storage backend.
type S3Config struct {
	Endpoint     string // Custom endpoint for S3-compatible services (e.g., http://localhost:9000)
	Region       string // AWS region (default: us-east-1)
	Bucket       string // S3 bucket name
	AccessKey    string // AWS access key ID
	SecretKey    string // AWS secret access key
	UsePathStyle bool   // Use path-style addressing (required for most S3-compatible services)
}

// S3Backend implements StorageBackend using AWS S3 or compatible services.
type S3Backend struct {
	client *s3.Client
	bucket string
}

// NewS3Backend creates a new S3 storage backend.
func NewS3Backend(cfg S3Config) (*S3Backend, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required")
	}

	// Build AWS config options
	var opts []func(*config.LoadOptions) error

	opts = append(opts, config.WithRegion(cfg.Region))

	// Use static credentials if provided
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options
	var s3Opts []func(*s3.Options)

	if cfg.Endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	if cfg.UsePathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(awsCfg, s3Opts...)

	return &S3Backend{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

// Save stores content in S3 and returns the generated path, hash, and size.
func (s *S3Backend) Save(ctx context.Context, r io.Reader, opts SaveOptions) (SaveResult, error) {
	// Generate unique filename
	ext := filepath.Ext(opts.OriginalFilename)
	key := uuid.New().String() + ext

	// Read content and compute hash
	// Note: For very large files, consider using multipart upload with streaming hash
	hasher := sha256.New()
	teeReader := io.TeeReader(r, hasher)

	content, err := io.ReadAll(teeReader)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to read content: %w", err)
	}

	// Upload to S3
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	_, err = s.client.PutObject(ctx, input)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to upload to S3: %w", err)
	}

	return SaveResult{
		Path: key,
		Hash: hex.EncodeToString(hasher.Sum(nil)),
		Size: int64(len(content)),
	}, nil
}

// Open returns a reader for the object at the given key.
func (s *S3Backend) Open(ctx context.Context, path string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		// Check for NotFound error
		if isS3NotFoundError(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get object from S3: %w", err)
	}

	return output.Body, nil
}

// Delete removes an object from S3. Returns nil if object doesn't exist (idempotent).
func (s *S3Backend) Delete(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	if err != nil && !isS3NotFoundError(err) {
		return fmt.Errorf("failed to delete object from S3: %w", err)
	}
	return nil
}

// Stat returns object metadata without downloading content.
func (s *S3Backend) Stat(ctx context.Context, path string) (FileInfo, error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(path),
	})
	if err != nil {
		if isS3NotFoundError(err) {
			return FileInfo{}, ErrNotFound
		}
		return FileInfo{}, fmt.Errorf("failed to stat object in S3: %w", err)
	}

	modTime := time.Time{}
	if output.LastModified != nil {
		modTime = *output.LastModified
	}

	size := int64(0)
	if output.ContentLength != nil {
		size = *output.ContentLength
	}

	return FileInfo{
		Path:    path,
		Size:    size,
		ModTime: modTime,
	}, nil
}

// HealthCheck verifies S3 connectivity by listing bucket (limited to 1 object).
func (s *S3Backend) HealthCheck(ctx context.Context) error {
	_, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.bucket),
		MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return fmt.Errorf("S3 health check failed: %w", err)
	}
	return nil
}

// ValidateAccess performs a full read/write/delete test on S3.
func (s *S3Backend) ValidateAccess(ctx context.Context) error {
	testKey := ".trove-access-test-" + uuid.New().String()
	testContent := []byte("access test")

	// Try to write
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(testKey),
		Body:   bytes.NewReader(testContent),
	})
	if err != nil {
		return fmt.Errorf("S3 write access test failed: %w", err)
	}

	// Try to read
	_, err = s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(testKey),
	})
	if err != nil {
		return fmt.Errorf("S3 read access test failed: %w", err)
	}

	// Try to delete
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(testKey),
	})
	if err != nil {
		return fmt.Errorf("S3 delete access test failed: %w", err)
	}

	return nil
}

// isS3NotFoundError checks if the error indicates the object was not found.
func isS3NotFoundError(err error) bool {
	if err == nil {
		return false
	}
	// AWS SDK v2 uses typed errors
	errStr := err.Error()
	return contains(errStr, "NoSuchKey") ||
		contains(errStr, "NotFound") ||
		contains(errStr, "404")
}

// contains is a simple string contains check.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsImpl(s, substr))
}

func containsImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

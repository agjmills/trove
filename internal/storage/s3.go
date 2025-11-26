package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

// S3Config holds configuration for the S3 storage backend.
// All other S3 configuration (endpoint, region, credentials) uses AWS SDK defaults:
//   - Environment variables (AWS_ENDPOINT_URL, AWS_REGION, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)
//   - Shared credentials file (~/.aws/credentials)
//   - Shared config file (~/.aws/config)
//   - IAM roles (EC2/ECS/Lambda)
type S3Config struct {
	Bucket       string // S3 bucket name (required)
	UsePathStyle bool   // Use path-style addressing (required for MinIO/rustfs)
}

// S3Backend implements StorageBackend using AWS S3 or compatible services.
type S3Backend struct {
	client *s3.Client
	bucket string
}

// NewS3Backend creates a new S3 storage backend.
// Uses the AWS SDK default credential chain for all configuration except bucket and path style:
//   - Environment variables (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_REGION, AWS_ENDPOINT_URL)
//   - Shared credentials file (~/.aws/credentials)
//   - Shared config file (~/.aws/config)
//   - IAM roles (EC2/ECS/Lambda)
func NewS3Backend(cfg S3Config) (*S3Backend, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("S3 bucket name is required (set S3_BUCKET)")
	}

	// Load AWS config using SDK defaults (env vars, shared config, IAM roles)
	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options
	var s3Opts []func(*s3.Options)

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

	// Create a hashing reader that computes SHA256 while streaming to S3
	// Note: For very large files, consider using multipart upload with streaming hash
	hasher := sha256.New()
	hashingReader := &hashingReader{
		reader: r,
		hasher: hasher,
	}

	// Upload to S3 using streaming reader
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		Body:   hashingReader,
	}

	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}

	output, err := s.client.PutObject(ctx, input)
	if err != nil {
		return SaveResult{}, fmt.Errorf("failed to upload to S3: %w", err)
	}

	// Get the actual size uploaded (AWS SDK tracks this)
	size := int64(0)
	if output.ETag != nil {
		// The hashingReader tracks bytes read
		size = hashingReader.bytesRead
	}

	return SaveResult{
		Path: key,
		Hash: hex.EncodeToString(hasher.Sum(nil)),
		Size: size,
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
	resp, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(testKey),
	})
	if err != nil {
		return fmt.Errorf("S3 read access test failed: %w", err)
	}
	// Drain and close the response body to allow connection reuse
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return fmt.Errorf("S3 read access test - failed to drain response body: %w", err)
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
	return strings.Contains(errStr, "NoSuchKey") ||
		strings.Contains(errStr, "NotFound") ||
		strings.Contains(errStr, "404")
}

// hashingReader wraps an io.Reader and computes a hash as data is read.
// This allows streaming uploads to S3 without buffering entire files in memory.
type hashingReader struct {
	reader    io.Reader
	hasher    io.Writer
	bytesRead int64
}

func (hr *hashingReader) Read(p []byte) (n int, err error) {
	n, err = hr.reader.Read(p)
	if n > 0 {
		hr.bytesRead += int64(n)
		// Write to hasher (hash.Hash.Write never returns an error)
		hr.hasher.Write(p[:n])
	}
	return n, err
}

package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	Port string
	Host string
	Env  string

	DBType     string
	DBHost     string
	DBPort     string
	DBName     string
	DBUser     string
	DBPassword string
	DBPath     string

	// Storage configuration
	StorageBackend string // "disk", "memory", "s3"
	StoragePath    string // For disk backend
	TempDir        string // Temp directory for uploads (defaults to system temp)
	S3Bucket       string // S3 bucket name (required for s3 backend)
	S3UsePathStyle bool   // Use path-style addressing (required for MinIO/rustfs)

	DefaultUserQuota int64
	MaxUploadSize    int64

	SessionSecret   string
	SessionDuration string
	BcryptCost      int
	CSRFEnabled     bool

	EnableRegistration      bool
	EnableFileDeduplication bool

	// Deleted items configuration
	DeletedRetentionDays      int // Default number of days to retain deleted files (0 = permanent delete immediately)
	DeletedCleanupIntervalMin int // Interval in minutes between deleted items cleanup runs

	// Chunked upload configuration
	UploadChunkSize       int64         // Default chunk size for resumable uploads (e.g., 5MB)
	UploadSessionTimeout  time.Duration // How long upload sessions remain active

	// TrustedProxyCIDRs is a list of CIDR ranges (e.g., "127.0.0.1/32", "10.0.0.0/8")
	// from which X-Forwarded-Proto headers will be trusted for CSRF origin validation.
	// If empty, X-Forwarded-Proto is never trusted and r.TLS is used to detect HTTPS.
	TrustedProxyCIDRs []string

	// CORSAllowedOrigins is a list of allowed origins for CORS requests (e.g., SSE endpoints).
	// If empty, no CORS headers are sent (same-origin only).
	// Origins should include scheme (e.g., "https://example.com").
	CORSAllowedOrigins []string
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:                      getEnv("PORT", "8080"),
		Host:                      getEnv("HOST", "0.0.0.0"),
		Env:                       getEnv("ENV", "development"),
		DBType:                    getEnv("DB_TYPE", "sqlite"),
		DBHost:                    getEnv("DB_HOST", "localhost"),
		DBPort:                    getEnv("DB_PORT", "5432"),
		DBName:                    getEnv("DB_NAME", "trove"),
		DBUser:                    getEnv("DB_USER", "trove"),
		DBPassword:                getEnv("DB_PASSWORD", ""),
		DBPath:                    getEnv("DB_PATH", "./data/trove.db"),
		StorageBackend:            getEnv("STORAGE_BACKEND", "disk"),
		StoragePath:               getEnv("STORAGE_PATH", "./data/files"),
		TempDir:                   getEnv("TEMP_DIR", ""),
		S3Bucket:                  getEnv("S3_BUCKET", ""),
		S3UsePathStyle:            getEnvBool("S3_USE_PATH_STYLE", false),
		DefaultUserQuota:          getEnvSize("DEFAULT_USER_QUOTA", "10G"),
		MaxUploadSize:             getEnvSize("MAX_UPLOAD_SIZE", "500M"),
		SessionSecret:             getEnv("SESSION_SECRET", "change_me_in_production"),
		SessionDuration:           getEnv("SESSION_DURATION", "168h"),
		BcryptCost:                getEnvInt("BCRYPT_COST", 10),
		CSRFEnabled:               getEnvBool("CSRF_ENABLED", true),
		EnableRegistration:        getEnvBool("ENABLE_REGISTRATION", true),
		EnableFileDeduplication:   getEnvBool("ENABLE_FILE_DEDUPLICATION", true),
		DeletedRetentionDays:      getEnvInt("DELETED_RETENTION_DAYS", 30),
		DeletedCleanupIntervalMin: getEnvInt("DELETED_CLEANUP_INTERVAL_MIN", 60),
		UploadChunkSize:           getEnvSize("UPLOAD_CHUNK_SIZE", "5M"),
		UploadSessionTimeout:      getEnvDuration("UPLOAD_SESSION_TIMEOUT", "24h"),
		TrustedProxyCIDRs:         getEnvStringSlice("TRUSTED_PROXY_CIDRS", nil),
		CORSAllowedOrigins:        getEnvStringSlice("CORS_ALLOWED_ORIGINS", nil),
	}

	// Validate deleted items configuration
	if cfg.DeletedRetentionDays < 0 {
		cfg.DeletedRetentionDays = 0
	}
	if cfg.DeletedCleanupIntervalMin < 1 {
		cfg.DeletedCleanupIntervalMin = 1 // Minimum 1 minute
	}

	log.Printf("Config loaded: MaxUploadSize=%d bytes (%.2f MB), DefaultUserQuota=%d bytes (%.2f GB)",
		cfg.MaxUploadSize, float64(cfg.MaxUploadSize)/(1024*1024),
		cfg.DefaultUserQuota, float64(cfg.DefaultUserQuota)/(1024*1024*1024))

	return cfg, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

// getEnvStringSlice parses a comma-separated env var into a string slice.
// Empty entries are filtered out. Returns defaultValue if env var is empty.
func getEnvStringSlice(key string, defaultValue []string) []string {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	if len(result) == 0 {
		return defaultValue
	}
	return result
}

// parseSize converts human-readable sizes (e.g., "10G", "500M", "1K") to bytes
// Supports: B, K/KB, M/MB, G/GB, T/TB (case-insensitive)
func parseSize(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(strings.ToUpper(sizeStr))

	// If it's just a number, treat as bytes
	if val, err := strconv.ParseInt(sizeStr, 10, 64); err == nil {
		return val, nil
	}

	// Parse size with unit
	var multiplier int64 = 1
	var numStr string

	if strings.HasSuffix(sizeStr, "TB") || strings.HasSuffix(sizeStr, "T") {
		multiplier = 1024 * 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "TB"), "T")
	} else if strings.HasSuffix(sizeStr, "GB") || strings.HasSuffix(sizeStr, "G") {
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "GB"), "G")
	} else if strings.HasSuffix(sizeStr, "MB") || strings.HasSuffix(sizeStr, "M") {
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "MB"), "M")
	} else if strings.HasSuffix(sizeStr, "KB") || strings.HasSuffix(sizeStr, "K") {
		multiplier = 1024
		numStr = strings.TrimSuffix(strings.TrimSuffix(sizeStr, "KB"), "K")
	} else if strings.HasSuffix(sizeStr, "B") {
		multiplier = 1
		numStr = strings.TrimSuffix(sizeStr, "B")
	} else {
		return 0, fmt.Errorf("invalid size format: %s (use B, K/KB, M/MB, G/GB, T/TB)", sizeStr)
	}

	// Parse the numeric part (supports decimals like "1.5G")
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %s", sizeStr)
	}

	return int64(val * float64(multiplier)), nil
}

// getEnvSize parses size strings like "10G", "500M" or raw bytes
func getEnvSize(key string, defaultValue string) int64 {
	value := getEnv(key, defaultValue)
	log.Printf("getEnvSize: key=%s, value=%s, default=%s", key, value, defaultValue)
	size, err := parseSize(value)
	if err != nil {
		log.Printf("getEnvSize: parseSize failed for %s: %v, trying default", value, err)
		// If parsing fails, try to get default
		if defaultSize, defaultErr := parseSize(defaultValue); defaultErr == nil {
			return defaultSize
		}
		// Last resort: return 0
		return 0
	}
	log.Printf("getEnvSize: parsed %s to %d bytes", value, size)
	return size
}

// getEnvDuration parses duration strings like "24h", "30m" or raw values
func getEnvDuration(key string, defaultValue string) time.Duration {
	value := getEnv(key, defaultValue)
	duration, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("getEnvDuration: parse failed for %s: %v, trying default", value, err)
		if defaultDuration, defaultErr := time.ParseDuration(defaultValue); defaultErr == nil {
			return defaultDuration
		}
		return 24 * time.Hour // Fallback to 24 hours
	}
	return duration
}

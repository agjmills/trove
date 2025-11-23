package config

import (
	"fmt"
	"os"
	"strconv"

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

	StoragePath      string
	DefaultUserQuota int64
	MaxUploadSize    int64

	SessionSecret   string
	SessionDuration string
	BcryptCost      int
	CSRFEnabled     bool

	EnableRegistration      bool
	EnableFileDeduplication bool
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		Port:                    getEnv("PORT", "8080"),
		Host:                    getEnv("HOST", "0.0.0.0"),
		Env:                     getEnv("ENV", "development"),
		DBType:                  getEnv("DB_TYPE", "sqlite"),
		DBHost:                  getEnv("DB_HOST", "localhost"),
		DBPort:                  getEnv("DB_PORT", "5432"),
		DBName:                  getEnv("DB_NAME", "trove"),
		DBUser:                  getEnv("DB_USER", "trove"),
		DBPassword:              getEnv("DB_PASSWORD", ""),
		DBPath:                  getEnv("DB_PATH", "./data/trove.db"),
		StoragePath:             getEnv("STORAGE_PATH", "./data/files"),
		DefaultUserQuota:        getEnvInt64("DEFAULT_USER_QUOTA", 10737418240),
		MaxUploadSize:           getEnvInt64("MAX_UPLOAD_SIZE", 524288000),
		SessionSecret:           getEnv("SESSION_SECRET", "change_me_in_production"),
		SessionDuration:         getEnv("SESSION_DURATION", "168h"),
		BcryptCost:              getEnvInt("BCRYPT_COST", 10),
		CSRFEnabled:             getEnvBool("CSRF_ENABLED", true),
		EnableRegistration:      getEnvBool("ENABLE_REGISTRATION", true),
		EnableFileDeduplication: getEnvBool("ENABLE_FILE_DEDUPLICATION", true),
	}

	if cfg.SessionSecret == "change_me_in_production" && cfg.Env == "production" {
		return nil, fmt.Errorf("SESSION_SECRET must be set in production")
	}

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

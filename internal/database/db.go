package database

import (
	"fmt"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database/models"
	"github.com/agjmills/trove/internal/logger"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

func Connect(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	switch cfg.DBType {
	case "postgres":
		dsn := fmt.Sprintf(
			"host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
			cfg.DBHost, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBPort,
		)
		dialector = postgres.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(cfg.DBPath)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", cfg.DBType)
	}

	logLevel := gormlogger.Silent
	if cfg.Env == "development" {
		logLevel = gormlogger.Info
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(logLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	logger.Info("database connected", "type", cfg.DBType)
	return db, nil
}

func Migrate(db *gorm.DB) error {
	logger.Info("running database migrations")

	// Handle storage_path column migration for existing data
	if err := migrateStoragePath(db); err != nil {
		return fmt.Errorf("failed to migrate storage_path: %w", err)
	}

	err := db.AutoMigrate(
		&models.User{},
		&models.Folder{},
		&models.File{},
	)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	// Create sessions table for alexedwards/scs
	if err := createSessionsTable(db); err != nil {
		return fmt.Errorf("failed to create sessions table: %w", err)
	}

	logger.Info("database migrations completed successfully")
	return nil
}

func migrateStoragePath(db *gorm.DB) error {
	// Check if files table exists
	var tableCount int64
	err := db.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'files'").Scan(&tableCount).Error
	if err != nil || tableCount == 0 {
		// Table doesn't exist yet, nothing to migrate
		return nil
	}

	// Check if old file_path column exists (indicates need for migration from old schema)
	var filePathExists int64
	err = db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'files' AND column_name = 'file_path'").Scan(&filePathExists).Error
	if err == nil && filePathExists > 0 {
		logger.Info("migrating from old schema: file_path -> storage_path, folder_path -> logical_path")

		// Add new columns if they don't exist
		var storagePathExists int64
		db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'files' AND column_name = 'storage_path'").Scan(&storagePathExists)
		if storagePathExists == 0 {
			if err := db.Exec("ALTER TABLE files ADD COLUMN storage_path VARCHAR(1024)").Error; err != nil {
				return fmt.Errorf("failed to add storage_path column: %w", err)
			}
		}

		var logicalPathExists int64
		db.Raw("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = CURRENT_SCHEMA() AND table_name = 'files' AND column_name = 'logical_path'").Scan(&logicalPathExists)
		if logicalPathExists == 0 {
			if err := db.Exec("ALTER TABLE files ADD COLUMN logical_path VARCHAR(1024)").Error; err != nil {
				return fmt.Errorf("failed to add logical_path column: %w", err)
			}
		}

		// Copy data from old columns to new columns
		if err := db.Exec("UPDATE files SET storage_path = file_path WHERE storage_path IS NULL").Error; err != nil {
			return fmt.Errorf("failed to migrate file_path to storage_path: %w", err)
		}
		if err := db.Exec("UPDATE files SET logical_path = COALESCE(folder_path, '/') WHERE logical_path IS NULL").Error; err != nil {
			return fmt.Errorf("failed to migrate folder_path to logical_path: %w", err)
		}

		// Make columns NOT NULL
		if err := db.Exec("ALTER TABLE files ALTER COLUMN storage_path SET NOT NULL").Error; err != nil {
			return fmt.Errorf("failed to set storage_path NOT NULL: %w", err)
		}
		if err := db.Exec("ALTER TABLE files ALTER COLUMN logical_path SET NOT NULL").Error; err != nil {
			return fmt.Errorf("failed to set logical_path NOT NULL: %w", err)
		}

		// Drop old columns
		if err := db.Exec("ALTER TABLE files DROP COLUMN file_path").Error; err != nil {
			logger.Warn("failed to drop file_path column", "error", err)
		}
		if err := db.Exec("ALTER TABLE files DROP COLUMN folder_path").Error; err != nil {
			logger.Warn("failed to drop folder_path column", "error", err)
		}

		logger.Info("schema migration completed successfully")
	}

	return nil
}

func createSessionsTable(db *gorm.DB) error {
	// Get the database type
	dbType := db.Dialector.Name()

	switch dbType {
	case "postgres":
		// Drop old sessions table if it exists with wrong schema
		if err := db.Exec(`DROP TABLE IF EXISTS sessions CASCADE`).Error; err != nil {
			return err
		}
		// Create new table
		if err := db.Exec(`
			CREATE TABLE IF NOT EXISTS sessions (
				token TEXT PRIMARY KEY,
				data BYTEA NOT NULL,
				expiry TIMESTAMPTZ NOT NULL
			)
		`).Error; err != nil {
			return err
		}
		// Create index
		return db.Exec(`CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expiry)`).Error

	case "sqlite":
		// Drop old sessions table if it exists with wrong schema
		if err := db.Exec(`DROP TABLE IF EXISTS sessions`).Error; err != nil {
			return err
		}
		// Create new table
		if err := db.Exec(`
			CREATE TABLE IF NOT EXISTS sessions (
				token TEXT PRIMARY KEY,
				data BLOB NOT NULL,
				expiry REAL NOT NULL
			)
		`).Error; err != nil {
			return err
		}
		// Create index
		return db.Exec(`CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions(expiry)`).Error

	default:
		return fmt.Errorf("unsupported database type: %s", dbType)
	}
}

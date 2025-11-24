package auth

import (
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/sqlite3store"
	"github.com/alexedwards/scs/v2"
	"github.com/agjmills/trove/internal/config"
	"gorm.io/gorm"
)

// NewSessionManager creates and configures an scs session manager
func NewSessionManager(db *gorm.DB, cfg *config.Config) (*scs.SessionManager, error) {
	// Get underlying *sql.DB from GORM
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// Parse session duration
	lifetime, err := time.ParseDuration(cfg.SessionDuration)
	if err != nil {
		lifetime = 168 * time.Hour // Default: 7 days
	}

	sessionManager := scs.New()
	sessionManager.Lifetime = lifetime
	sessionManager.Cookie.Name = "session_token"
	sessionManager.Cookie.HttpOnly = true
	sessionManager.Cookie.SameSite = 3 // Strict
	sessionManager.Cookie.Secure = cfg.Env == "production" // Only use Secure in production with HTTPS

	// Choose store based on database type
	switch cfg.DBType {
	case "postgres":
		sessionManager.Store = postgresstore.New(sqlDB)
	case "sqlite":
		sessionManager.Store = sqlite3store.New(sqlDB)
	default:
		// Fallback to memory store (not recommended for production)
		// Store is already initialized by scs.New()
	}

	return sessionManager, nil
}

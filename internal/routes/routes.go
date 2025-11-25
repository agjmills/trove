package routes

import (
	"net/http"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/middleware"
	"github.com/agjmills/trove/internal/storage"
	"github.com/alexedwards/scs/v2"
	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/csrf"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

func Setup(r chi.Router, db *gorm.DB, cfg *config.Config, storageService storage.StorageBackend, sessionManager *scs.SessionManager, version string) {
	authHandler := handlers.NewAuthHandler(db, cfg, sessionManager)
	pageHandler := handlers.NewPageHandler(db, cfg)
	fileHandler := handlers.NewFileHandler(db, cfg, storageService)
	healthHandler := handlers.NewHealthHandler(db, storageService, version)

	// Create rate limiter for auth endpoints
	// Allow 5 login/register attempts per 15 minutes per IP
	authRateLimiter := tollbooth.NewLimiter(5.0/15.0, &limiter.ExpirableOptions{
		DefaultExpirationTTL: 15 * time.Minute,
	})
	authRateLimiter.SetMessage("Too many requests. Please try again later.")

	// CSRF protection (only if enabled in config)
	var csrfMiddleware func(http.Handler) http.Handler
	if cfg.CSRFEnabled {
		csrfMiddleware = csrf.Protect(
			[]byte(cfg.SessionSecret), // Use session secret as CSRF key
			csrf.Secure(cfg.Env == "production"),
			csrf.SameSite(csrf.SameSiteStrictMode),
			csrf.FieldName("csrf_token"),      // Form field name
			csrf.RequestHeader("X-CSRF-Token"), // Header name for XHR requests
		)
	} else {
		// No-op middleware if CSRF is disabled
		csrfMiddleware = func(next http.Handler) http.Handler {
			return next
		}
	}

	r.Get("/health", healthHandler.Health)
	r.Handle("/metrics", promhttp.Handler())

	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// 404 handler
	r.NotFound(middleware.NotFoundHandler)

	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.OptionalAuth(db, sessionManager))
		r.Get("/", authHandler.ShowLogin)
		r.Get("/login", authHandler.ShowLogin)
		r.Get("/register", authHandler.ShowRegister)
	})

	// Rate-limited auth endpoints
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(func(next http.Handler) http.Handler {
			return tollbooth.LimitHandler(authRateLimiter, next)
		})
		r.Post("/register", authHandler.Register)
		r.Post("/login", authHandler.Login)
	})

	// Logout endpoint needs session middleware
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Post("/logout", authHandler.Logout)
	})

	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(csrfMiddleware)
		r.Get("/dashboard", pageHandler.ShowDashboard)
		r.Post("/folders/create", fileHandler.CreateFolder)
		r.Get("/download/{id}", fileHandler.Download)
		r.Post("/delete/{id}", fileHandler.Delete)
		r.Post("/folders/delete/{name}", fileHandler.DeleteFolder)
	})

	// Upload endpoint - exempt from Gorilla CSRF middleware
	// Gorilla CSRF calls ParseMultipartForm internally which consumes the request body,
	// breaking our streaming upload. Protection is still provided via:
	// 1. Session-based authentication (RequireAuth middleware)
	// 2. SameSite cookie policy preventing cross-origin requests
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		// No CSRF middleware - streaming uploads handle their own protection
		r.Post("/upload", fileHandler.Upload)
	})
}

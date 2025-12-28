package routes

import (
	"net"
	"net/http"
	"strings"
	"time"

	csrf "filippo.io/csrf/gorilla"
	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/middleware"
	"github.com/agjmills/trove/internal/storage"
	"github.com/alexedwards/scs/v2"
	"github.com/didip/tollbooth/v7"
	"github.com/didip/tollbooth/v7/limiter"
	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gorm.io/gorm"
)

// parseTrustedCIDRs parses a list of CIDR strings into net.IPNet objects.
// Invalid CIDRs are logged and skipped.
func parseTrustedCIDRs(cidrs []string) []*net.IPNet {
	var result []*net.IPNet
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			// Try parsing as a single IP (e.g., "127.0.0.1" without mask)
			ip := net.ParseIP(cidr)
			if ip != nil {
				// Convert single IP to /32 (IPv4) or /128 (IPv6)
				if ip.To4() != nil {
					_, ipNet, _ = net.ParseCIDR(cidr + "/32")
				} else {
					_, ipNet, _ = net.ParseCIDR(cidr + "/128")
				}
				if ipNet != nil {
					result = append(result, ipNet)
					continue
				}
			}
			logger.Warn("invalid trusted proxy CIDR, skipping", "cidr", cidr, "error", err)
			continue
		}
		result = append(result, ipNet)
	}
	return result
}

// isIPInCIDRs checks if the given IP string is contained in any of the CIDR ranges.
func isIPInCIDRs(ipStr string, cidrs []*net.IPNet) bool {
	// Handle host:port format from RemoteAddr
	host, _, err := net.SplitHostPort(ipStr)
	if err != nil {
		// No port, use as-is
		host = ipStr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	for _, cidr := range cidrs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// getClientIP extracts the client IP, preferring X-Real-IP or the leftmost
// X-Forwarded-For entry if from a trusted proxy. For multi-hop proxy chains
// (e.g., CDN → load balancer → app), X-Forwarded-For is parsed to get the
// original client IP.
func getClientIP(r *http.Request, trustedCIDRs []*net.IPNet) string {
	// First check if RemoteAddr is from a trusted proxy
	if len(trustedCIDRs) > 0 && isIPInCIDRs(r.RemoteAddr, trustedCIDRs) {
		// Trust X-Real-IP header if set (single-proxy setup)
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return realIP
		}
		// Trust X-Forwarded-For header (multi-hop proxy chains)
		// Format: X-Forwarded-For: client, proxy1, proxy2
		// We want the leftmost (original client) IP
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				clientIP := strings.TrimSpace(ips[0])
				if clientIP != "" {
					return clientIP
				}
			}
		}
	}
	return r.RemoteAddr
}

// Setup configures HTTP routes and middleware on the provided chi.Router, wiring application handlers,
// health and metrics endpoints, static file serving, authentication flows, CSRF protection (when enabled),
// and rate limiting for authentication endpoints.
//
// CSRF PROTECTION (filippo.io/csrf v0.2.1):
// This middleware uses Fetch Metadata headers (Sec-Fetch-Site, Origin) for CSRF protection
// instead of the traditional double-submit token pattern. Key behavioral differences:
//
// Browser Request Detection:
//   - Requests WITH Sec-Fetch-Site header are validated as browser requests
//   - Cross-site browser requests (Sec-Fetch-Site: cross-site) are BLOCKED
//   - Same-site browser requests (Sec-Fetch-Site: same-site) are BLOCKED (includes subdomains)
//   - Same-origin browser requests (Sec-Fetch-Site: same-origin) are ALLOWED
//
// Non-Browser Client Behavior:
//   - Requests WITHOUT Sec-Fetch-Site or Origin headers are ALLOWED through
//   - This permits CLI tools (curl, wget), API clients, webhooks, and mobile apps
//   - These clients cannot be exploited via CSRF since they don't automatically attach cookies
//   - Authentication still required via session cookie (obtained through login flow)
//
// Security Model:
//   - CSRF attacks require a browser to automatically attach session cookies
//   - Non-browser clients must explicitly manage cookies, preventing unwitting attacks
//   - Session-based auth + SameSite=Lax cookies provide baseline protection
//
// Token-based CSRF validation is NOT performed. The csrf.Token() function exists for API
// compatibility but tokens are not validated on state-changing requests.
//
// API ENDPOINTS EXEMPT FROM CSRF:
// The following endpoints are exempt from CSRF middleware for non-browser client support:
//   - /upload (streaming multipart uploads)
//   - /api/uploads/* (chunked upload JSON API)
//   - /api/files/status (SSE, GET-only)
//
// These endpoints rely on session-based authentication and SameSite cookie policy.
//
// Returns the file handler and deleted handler for graceful shutdown support.
func Setup(r chi.Router, db *gorm.DB, cfg *config.Config, storageService storage.StorageBackend, sessionManager *scs.SessionManager, version string) (*handlers.FileHandler, *handlers.DeletedHandler) {
	authHandler := handlers.NewAuthHandler(db, cfg, sessionManager)
	pageHandler := handlers.NewPageHandler(db, cfg)
	fileHandler := handlers.NewFileHandler(db, cfg, storageService)
	uploadHandler := handlers.NewUploadHandler(db, cfg, storageService)
	healthHandler := handlers.NewHealthHandler(db, storageService, version)
	adminHandler := handlers.NewAdminHandler(db, cfg, storageService)
	deletedHandler := handlers.NewDeletedHandler(db, cfg, storageService)

	// Create rate limiter for auth endpoints
	// Allow 5 login/register attempts per 15 minutes per IP
	authRateLimiter := tollbooth.NewLimiter(5.0/15.0, &limiter.ExpirableOptions{
		DefaultExpirationTTL: 15 * time.Minute,
	})
	authRateLimiter.SetMessage("Too many requests. Please try again later.")

	// CSRF protection (only if enabled in config)
	var csrfMiddleware func(http.Handler) http.Handler
	if cfg.CSRFEnabled {
		// filippo.io/csrf uses Fetch Metadata headers (Sec-Fetch-Site, Origin) for CSRF protection.
		// The authKey is required and must be exactly 32 bytes. It is used internally for
		// cryptographic operations. Keep this key secret and persist it across restarts.
		csrfMiddleware = csrf.Protect(
			[]byte(cfg.SessionSecret),
			csrf.ErrorHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				logger.Warn("csrf validation failed",
					"reason", csrf.FailureReason(r),
					"method", r.Method,
					"path", r.URL.Path,
				)
				http.Error(w, "Forbidden", http.StatusForbidden)
			})),
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

	// Logout endpoint needs session middleware and CSRF protection
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(csrfMiddleware)
		r.Post("/logout", authHandler.Logout)
	})

	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(csrfMiddleware)
		r.Get("/files", pageHandler.ShowFiles)
		r.Get("/files/{id}", fileHandler.ViewFile)
		r.Get("/deleted", deletedHandler.ShowDeleted)
		r.Post("/deleted/empty", deletedHandler.EmptyDeleted)
		r.Post("/deleted/files/{id}/restore", deletedHandler.RestoreFile)
		r.Post("/deleted/files/{id}/delete", deletedHandler.PermanentlyDeleteFile)
		r.Post("/deleted/folders/{id}/restore", deletedHandler.RestoreFolder)
		r.Post("/deleted/folders/{id}/delete", deletedHandler.PermanentlyDeleteFolder)
		r.Get("/settings", authHandler.ShowSettings)
		r.Post("/folders/create", fileHandler.CreateFolder)
		r.Post("/folders/rename", fileHandler.RenameFolder)
		r.Post("/folders/move", fileHandler.MoveFolder)
		r.Get("/download/{id}", fileHandler.Download)
		r.Get("/preview/{id}", fileHandler.Preview)
		r.Post("/delete/{id}", fileHandler.Delete)
		r.Post("/rename/{id}", fileHandler.RenameFile)
		r.Post("/move/{id}", fileHandler.MoveFile)
		r.Post("/folders/delete/{name}", fileHandler.DeleteFolder)
		r.Post("/files/{id}/dismiss", fileHandler.DismissFailedUpload)
	})

	// SSE endpoint for file upload status - no CSRF needed (GET request, read-only)
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Get("/api/files/status", fileHandler.StatusStream)
	})

	// Change password endpoint - rate limited like login/register
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(func(next http.Handler) http.Handler {
			return tollbooth.LimitHandler(authRateLimiter, next)
		})
		r.Use(csrfMiddleware)
		r.Post("/settings/change-password", authHandler.ChangePassword)
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

	// Chunked upload API endpoints - JSON API for resumable uploads
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		// No CSRF middleware - JSON API with session authentication
		r.Post("/api/uploads/init", uploadHandler.InitUpload)
		r.Post("/api/uploads/{id}/chunk", uploadHandler.UploadChunk)
		r.Post("/api/uploads/{id}/complete", uploadHandler.CompleteUpload)
		r.Delete("/api/uploads/{id}", uploadHandler.CancelUpload)
		r.Get("/api/uploads/{id}/status", uploadHandler.GetUploadStatus)
	})

	// Admin routes - require admin privileges
	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(auth.RequireAdmin())
		r.Use(csrfMiddleware)
		r.Get("/admin", adminHandler.ShowDashboard)
		r.Get("/admin/users", adminHandler.ShowUsers)
		r.Post("/admin/users/create", adminHandler.CreateUser)
		r.Post("/admin/users/{id}/toggle-admin", adminHandler.ToggleAdmin)
		r.Post("/admin/users/{id}/quota", adminHandler.UpdateUserQuota)
		r.Post("/admin/users/{id}/delete", adminHandler.DeleteUser)
		r.Post("/admin/users/{id}/reset-password", adminHandler.ResetUserPassword)
		r.Post("/admin/deleted/empty-all", deletedHandler.AdminEmptyAllDeleted)
	})

	return fileHandler, deletedHandler
}

package routes

import (
	"net"
	"net/http"
	"time"

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
	"github.com/gorilla/csrf"
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

// getClientIP extracts the client IP, preferring X-Real-IP if from a trusted proxy.
func getClientIP(r *http.Request, trustedCIDRs []*net.IPNet) string {
	// First check if RemoteAddr is from a trusted proxy
	if len(trustedCIDRs) > 0 && isIPInCIDRs(r.RemoteAddr, trustedCIDRs) {
		// Trust X-Real-IP header if set
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			return realIP
		}
	}
	return r.RemoteAddr
}

// plaintextCSRFMiddleware returns middleware that marks requests as plaintext HTTP
// for gorilla/csrf origin validation. In development, all requests are marked plaintext.
// In production, X-Forwarded-Proto is only trusted when the request comes from a
// configured trusted proxy CIDR; otherwise r.TLS is used to detect HTTPS.
func plaintextCSRFMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	// Pre-parse trusted CIDRs once at middleware creation time
	trustedCIDRs := parseTrustedCIDRs(cfg.TrustedProxyCIDRs)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.Env != "production" {
				// Development: skip origin checks (plaintext HTTP)
				r = csrf.PlaintextHTTPRequest(r)
			} else {
				// Production: determine if request is over HTTPS
				isHTTPS := false

				// Only trust X-Forwarded-Proto if request is from a trusted proxy
				if len(trustedCIDRs) > 0 && isIPInCIDRs(r.RemoteAddr, trustedCIDRs) {
					proto := r.Header.Get("X-Forwarded-Proto")
					isHTTPS = proto == "https"
				} else {
					// No trusted proxy or request not from trusted IP: use r.TLS
					isHTTPS = r.TLS != nil
				}

				if !isHTTPS {
					r = csrf.PlaintextHTTPRequest(r)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Setup configures HTTP routes and middleware on the provided chi.Router, wiring application handlers,
// health and metrics endpoints, static file serving, authentication flows, CSRF protection (when enabled),
// and rate limiting for authentication endpoints.
//
// When CSRF is enabled, the middleware is initialized with the session secret and its Secure flag is
// determined from cfg.Env; when disabled, a no-op CSRF middleware is used. Authentication endpoints are
// rate-limited to 5 attempts per 15 minutes per IP. The multipart upload endpoint is intentionally exempt
// from the Gorilla CSRF middleware to allow streaming uploads while remaining protected by session-based
// authentication and SameSite cookie policy.
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
		// In production (HTTPS), enforce strict origin validation
		// In development (HTTP), skip origin checks - the CSRF token itself still protects
		// Note: gorilla/csrf automatically skips origin validation for HTTP when Secure=false
		isSecure := cfg.Env == "production"

		csrfMiddleware = csrf.Protect(
			[]byte(cfg.SessionSecret),           // Use session secret as CSRF key
			csrf.Secure(isSecure),               // Only require HTTPS in production
			csrf.SameSite(csrf.SameSiteLaxMode), // Allow same-site POST requests
			csrf.FieldName("csrf_token"),        // Form field name
			csrf.RequestHeader("X-CSRF-Token"),  // Header name for XHR requests
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
		r.Use(plaintextCSRFMiddleware(cfg))
		r.Use(csrfMiddleware)
		r.Post("/logout", authHandler.Logout)
	})

	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(plaintextCSRFMiddleware(cfg))
		r.Use(csrfMiddleware)
		r.Get("/files", pageHandler.ShowFiles)
		r.Get("/settings", authHandler.ShowSettings)
		r.Post("/settings/change-password", authHandler.ChangePassword)
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

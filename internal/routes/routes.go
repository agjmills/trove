package routes

import (
	"net/http"
	"time"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/csrf"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/middleware"
	"github.com/agjmills/trove/internal/storage"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

func Setup(r chi.Router, db *gorm.DB, cfg *config.Config, storageService storage.StorageBackend, sessionManager *scs.SessionManager) {
	authHandler := handlers.NewAuthHandler(db, cfg, sessionManager)
	pageHandler := handlers.NewPageHandler(db)
	fileHandler := handlers.NewFileHandler(db, cfg, storageService)

	// Create rate limiters for auth endpoints
	// Allow 5 login/register attempts per 15 minutes per IP
	authRateLimiter := middleware.NewRateLimiter(5, 15*time.Minute)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

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
		r.Use(authRateLimiter.Middleware)
		r.Post("/register", authHandler.Register)
		r.Post("/login", authHandler.Login)
	})

	r.Post("/logout", authHandler.Logout)

	r.Group(func(r chi.Router) {
		r.Use(sessionManager.LoadAndSave)
		r.Use(auth.RequireAuth(db, sessionManager))
		r.Use(csrf.Middleware)
		r.Get("/dashboard", pageHandler.ShowDashboard)
		r.Post("/upload", fileHandler.Upload)
		r.Post("/folders/create", fileHandler.CreateFolder)
		r.Get("/download/{id}", fileHandler.Download)
		r.Post("/delete/{id}", fileHandler.Delete)
		r.Post("/folders/delete/{name}", fileHandler.DeleteFolder)
	})
}

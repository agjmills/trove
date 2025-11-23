package routes

import (
	"net/http"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

func Setup(r chi.Router, db *gorm.DB, cfg *config.Config) {
	authHandler := handlers.NewAuthHandler(db, cfg)
	pageHandler := handlers.NewPageHandler(db)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	r.Group(func(r chi.Router) {
		r.Use(auth.OptionalAuth(db))
		r.Get("/", authHandler.ShowLogin)
		r.Get("/login", authHandler.ShowLogin)
		r.Get("/register", authHandler.ShowRegister)
	})

	r.Post("/register", authHandler.Register)
	r.Post("/login", authHandler.Login)
	r.Post("/logout", authHandler.Logout)

	r.Group(func(r chi.Router) {
		r.Use(auth.RequireAuth(db))
		r.Get("/dashboard", pageHandler.ShowDashboard)
	})
}

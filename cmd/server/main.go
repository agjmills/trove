package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/routes"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	if err := handlers.LoadTemplates(); err != nil {
		log.Fatalf("Failed to load templates: %v", err)
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	routes.Setup(r, db, cfg)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	log.Printf("Starting Trove server on %s (environment: %s)", addr, cfg.Env)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

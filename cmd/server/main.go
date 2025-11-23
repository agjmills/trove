package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database"
	"github.com/agjmills/trove/internal/handlers"
	internalMiddleware "github.com/agjmills/trove/internal/middleware"
	"github.com/agjmills/trove/internal/routes"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded: MaxUploadSize=%d bytes (%.2f MB), DefaultUserQuota=%d bytes (%.2f GB)",
		cfg.MaxUploadSize, float64(cfg.MaxUploadSize)/(1024*1024),
		cfg.DefaultUserQuota, float64(cfg.DefaultUserQuota)/(1024*1024*1024))

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

	if err := internalMiddleware.LoadErrorTemplates(); err != nil {
		log.Fatalf("Failed to load error templates: %v", err)
	}

	storageService, err := storage.NewService(cfg.StoragePath)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(internalMiddleware.RecoverMiddleware)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	routes.Setup(r, db, cfg, storageService)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	log.Printf("Starting Trove server on %s (environment: %s)", addr, cfg.Env)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

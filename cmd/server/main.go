package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/agjmills/trove/internal/auth"
	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database"
	"github.com/agjmills/trove/internal/handlers"
	"github.com/agjmills/trove/internal/logger"
	internalMiddleware "github.com/agjmills/trove/internal/middleware"
	"github.com/agjmills/trove/internal/routes"
	"github.com/agjmills/trove/internal/storage"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Initialize structured logger
	logger.Init(cfg.Env)

	logger.Info("configuration loaded",
		"max_upload_mb", float64(cfg.MaxUploadSize)/(1024*1024),
		"default_quota_gb", float64(cfg.DefaultUserQuota)/(1024*1024*1024),
		"env", cfg.Env,
	)

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

	sessionManager, err := auth.NewSessionManager(db, cfg)
	if err != nil {
		log.Fatalf("Failed to initialize session manager: %v", err)
	}

	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(internalMiddleware.LoggingMiddleware)
	r.Use(internalMiddleware.RecoverMiddleware)
	r.Use(internalMiddleware.SecurityHeaders)

	versionInfo := fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
	routes.Setup(r, db, cfg, storageService, sessionManager, versionInfo)

	addr := fmt.Sprintf("%s:%s", cfg.Host, cfg.Port)
	logger.Info("starting trove server",
		"address", addr,
		"environment", cfg.Env,
		"version", versionInfo,
	)

	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}

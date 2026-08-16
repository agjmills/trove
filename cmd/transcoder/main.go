package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gorm.io/gorm"

	"github.com/agjmills/trove/internal/config"
	"github.com/agjmills/trove/internal/database"
	"github.com/agjmills/trove/internal/logger"
	"github.com/agjmills/trove/internal/storage"
	"github.com/agjmills/trove/internal/transcode"
)

// The transcoder worker polls the transcode_jobs table, claims jobs one at a
// time, and converts video uploads into web-optimized H.264/AAC MP4 variants
// (max 720p, faststart). Originals are retained for downloads.
//
// Usage:
//
//	trove-transcoder            # run the worker (one job at a time)
//	trove-transcoder -backfill  # enqueue jobs for existing videos, then exit
//	trove-transcoder -once      # process the queue until empty, then exit
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	backfill := flag.Bool("backfill", false, "enqueue transcode jobs for all existing videos, then exit")
	once := flag.Bool("once", false, "process the queue until empty, then exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	logger.Init(cfg.Env)

	// Retry DB connect/migrate a few times: the transcoder container may start
	// before the database is ready, and migrations can briefly race with the
	// app container on first deploy.
	var db *gorm.DB
	var dbErr error
	for attempt := 1; attempt <= 5; attempt++ {
		db, dbErr = database.Connect(cfg)
		if dbErr == nil {
			dbErr = database.Migrate(db)
		}
		if dbErr == nil {
			break
		}
		logger.Warn("database not ready, retrying", "attempt", attempt, "error", dbErr)
		time.Sleep(5 * time.Second)
	}
	if dbErr != nil {
		log.Fatalf("Failed to connect to database: %v", dbErr)
	}

	storageService, err := storage.NewBackendFromConfig(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize storage backend: %v", err)
	}

	if err := storageService.ValidateAccess(context.Background()); err != nil {
		log.Fatalf("Storage backend failed access validation: %v", err)
	}

	if *backfill {
		count, err := transcode.Backfill(db)
		if err != nil {
			log.Fatalf("Backfill failed: %v", err)
		}
		logger.Info("backfill complete", "jobs_enqueued", count)
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	worker := transcode.NewWorker(db, cfg, storageService)

	logger.Info("transcoder worker starting",
		"version", fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date),
		"workers", cfg.TranscodeWorkers,
		"poll_interval", cfg.TranscodePollInterval,
		"max_height", cfg.TranscodeMaxHeight,
		"preset", cfg.TranscodePreset,
	)

	if err := worker.Run(ctx, *once); err != nil {
		logger.Error("transcoder worker stopped with error", "error", err)
		os.Exit(1)
	}
	logger.Info("transcoder worker stopped")
}

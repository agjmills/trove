package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/agjmills/trove/internal/storage"
	"gorm.io/gorm"
)

// HealthHandler handles health check requests
type HealthHandler struct {
	db             *gorm.DB
	storageService storage.StorageBackend
	version        string
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(db *gorm.DB, storageService storage.StorageBackend, version string) *HealthHandler {
	return &HealthHandler{
		db:             db,
		storageService: storageService,
		version:        version,
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Status  string           `json:"status"`
	Version string           `json:"version"`
	Checks  map[string]Check `json:"checks"`
	Uptime  string           `json:"uptime,omitempty"`
}

// Check represents an individual health check
type Check struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Latency string `json:"latency,omitempty"`
}

var startTime = time.Now()

// Health performs comprehensive health checks
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	checks := make(map[string]Check)
	overallStatus := "healthy"

	// Database check
	dbCheck := h.checkDatabase()
	checks["database"] = dbCheck
	if dbCheck.Status != "healthy" {
		overallStatus = "unhealthy"
	}

	// Storage check
	storageCheck := h.checkStorage()
	checks["storage"] = storageCheck
	if storageCheck.Status != "healthy" {
		overallStatus = "unhealthy"
	}

	response := HealthResponse{
		Status:  overallStatus,
		Version: h.version,
		Checks:  checks,
		Uptime:  time.Since(startTime).Round(time.Second).String(),
	}

	w.Header().Set("Content-Type", "application/json")

	if overallStatus != "healthy" {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	json.NewEncoder(w).Encode(response)
}

// checkDatabase verifies database connectivity
func (h *HealthHandler) checkDatabase() Check {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	sqlDB, err := h.db.DB()
	if err != nil {
		return Check{
			Status:  "unhealthy",
			Message: "failed to get database connection: " + err.Error(),
			Latency: time.Since(start).String(),
		}
	}

	if err := sqlDB.PingContext(ctx); err != nil {
		return Check{
			Status:  "unhealthy",
			Message: "database ping failed: " + err.Error(),
			Latency: time.Since(start).String(),
		}
	}

	return Check{
		Status:  "healthy",
		Latency: time.Since(start).String(),
	}
}

// checkStorage verifies storage backend is accessible
func (h *HealthHandler) checkStorage() Check {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := h.storageService.HealthCheck(ctx); err != nil {
		return Check{
			Status:  "unhealthy",
			Message: "storage health check failed: " + err.Error(),
			Latency: time.Since(start).String(),
		}
	}

	return Check{
		Status:  "healthy",
		Latency: time.Since(start).String(),
	}
}

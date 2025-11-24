package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTP metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trove_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "trove_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path", "status"},
	)

	HTTPRequestsInFlight = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "trove_http_requests_in_flight",
			Help: "Current number of HTTP requests being served",
		},
	)

	// Storage metrics
	StorageUsageBytes = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "trove_storage_usage_bytes",
			Help: "Current storage usage in bytes per user",
		},
		[]string{"user_id"},
	)

	FilesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trove_files_total",
			Help: "Total number of files uploaded",
		},
		[]string{"user_id"},
	)

	FilesDeleted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trove_files_deleted_total",
			Help: "Total number of files deleted",
		},
		[]string{"user_id"},
	)

	// Authentication metrics
	LoginAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trove_login_attempts_total",
			Help: "Total number of login attempts",
		},
		[]string{"status"},
	)

	RegisterAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trove_register_attempts_total",
			Help: "Total number of registration attempts",
		},
		[]string{"status"},
	)

	// Database metrics
	DatabaseConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "trove_database_connections_active",
			Help: "Current number of active database connections",
		},
	)

	DatabaseConnectionsIdle = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "trove_database_connections_idle",
			Help: "Current number of idle database connections",
		},
	)

	DatabaseQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "trove_database_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation"},
	)
)

// RecordHTTPRequest records metrics for an HTTP request
func RecordHTTPRequest(method, path string, status int, duration time.Duration) {
	statusStr := httpStatusToString(status)
	HTTPRequestsTotal.WithLabelValues(method, path, statusStr).Inc()
	HTTPRequestDuration.WithLabelValues(method, path, statusStr).Observe(duration.Seconds())
}

// StatusToString converts HTTP status code to string
func httpStatusToString(code int) string {
	if code >= 200 && code < 300 {
		return "2xx"
	} else if code >= 300 && code < 400 {
		return "3xx"
	} else if code >= 400 && code < 500 {
		return "4xx"
	} else if code >= 500 {
		return "5xx"
	}
	return "unknown"
}

// RecordStorageUsage updates storage usage metrics
func RecordStorageUsage(userID string, bytes int64) {
	StorageUsageBytes.WithLabelValues(userID).Set(float64(bytes))
}

// RecordFileUpload increments file upload counter
func RecordFileUpload(userID string) {
	FilesTotal.WithLabelValues(userID).Inc()
}

// RecordFileDelete increments file delete counter
func RecordFileDelete(userID string) {
	FilesDeleted.WithLabelValues(userID).Inc()
}

// RecordLogin increments login attempt counter
func RecordLogin(success bool) {
	status := "failure"
	if success {
		status = "success"
	}
	LoginAttempts.WithLabelValues(status).Inc()
}

// RecordRegistration increments registration attempt counter
func RecordRegistration(success bool) {
	status := "failure"
	if success {
		status = "success"
	}
	RegisterAttempts.WithLabelValues(status).Inc()
}

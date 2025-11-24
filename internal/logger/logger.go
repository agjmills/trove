package logger

import (
	"log/slog"
	"os"
)

// Logger is the global structured logger
var Logger *slog.Logger

// Init initializes the global logger based on environment
func Init(env string) {
	var handler slog.Handler

	if env == "production" {
		// JSON format for production (machine-readable)
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	} else {
		// Text format for development (human-readable)
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelDebug,
		})
	}

	Logger = slog.New(handler)
	slog.SetDefault(Logger)
}

// With returns a logger with additional key-value pairs
func With(args ...any) *slog.Logger {
	if Logger == nil {
		Init("development")
	}
	return Logger.With(args...)
}

// Info logs an info message
func Info(msg string, args ...any) {
	if Logger == nil {
		Init("development")
	}
	Logger.Info(msg, args...)
}

// Debug logs a debug message
func Debug(msg string, args ...any) {
	if Logger == nil {
		Init("development")
	}
	Logger.Debug(msg, args...)
}

// Warn logs a warning message
func Warn(msg string, args ...any) {
	if Logger == nil {
		Init("development")
	}
	Logger.Warn(msg, args...)
}

// Error logs an error message
func Error(msg string, args ...any) {
	if Logger == nil {
		Init("development")
	}
	Logger.Error(msg, args...)
}

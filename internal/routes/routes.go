package routes

import (
	"net/http"

	"github.com/agjmills/trove/internal/config"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

func Setup(r chi.Router, db *gorm.DB, cfg *config.Config) {
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
	<title>Trove</title>
	<meta charset="utf-8">
	<meta name="viewport" content="width=device-width, initial-scale=1">
</head>
<body>
	<h1>Trove</h1>
	<p>File storage is running.</p>
</body>
</html>`))
	})
}

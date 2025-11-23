package middleware

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
)

var errorTemplates *template.Template

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// LoadErrorTemplates loads error page templates
func LoadErrorTemplates() error {
	funcMap := template.FuncMap{
		"formatBytes": formatBytes,
		"add": func(a, b int) int {
			return a + b
		},
		"mul": func(a, b int64) int64 {
			return a * b
		},
		"div": func(a, b int64) int64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}
	var err error
	errorTemplates, err = template.New("").Funcs(funcMap).ParseGlob(filepath.Join("web", "templates", "*.html"))
	return err
}

// NotFoundHandler renders a custom 404 page
func NotFoundHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	renderError(w, "404.html", map[string]any{
		"Title": "Page Not Found",
	})
}

// InternalErrorHandler renders a custom 500 page
func InternalErrorHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusInternalServerError)
	renderError(w, "500.html", map[string]any{
		"Title": "Internal Server Error",
	})
}

func renderError(w http.ResponseWriter, page string, data map[string]any) {
	if errorTemplates == nil {
		// Fallback if templates aren't loaded
		fmt.Fprintf(w, "Error: %s", data["Title"])
		return
	}

	tmpl, err := errorTemplates.Clone()
	if err != nil {
		fmt.Fprintf(w, "Error rendering template: %v", err)
		return
	}

	_, err = tmpl.ParseFiles(filepath.Join("web", "templates", page))
	if err != nil {
		fmt.Fprintf(w, "Error parsing template: %v", err)
		return
	}

	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		fmt.Fprintf(w, "Error executing template: %v", err)
	}
}

// RecoverMiddleware catches panics and renders 500 pages
func RecoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				// Log the error
				fmt.Printf("Panic recovered: %v\n", err)
				InternalErrorHandler(w, r)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

package middleware

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/agjmills/trove/internal/templateutil"
)

var errorTemplates *template.Template

// LoadErrorTemplates loads and parses error page HTML templates and registers helper functions for use by those templates.
//
// It registers the following template helpers: `formatBytes`, `storagePercentage` (returns 0 if quota is 0; otherwise computes used/quota as a percentage capped at 100), `add`, `mul`, and `div` (returns 0 when dividing by zero).
// Parsed templates are loaded from the web/templates/*.html glob and stored in the package-level variable `errorTemplates`.
// Returns any error encountered while parsing the templates.
func LoadErrorTemplates() error {
	var err error
	errorTemplates, err = template.New("").Funcs(templateutil.FuncMap()).ParseGlob(filepath.Join("web", "templates", "*.html"))
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

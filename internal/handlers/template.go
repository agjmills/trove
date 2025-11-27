package handlers

import (
	"html/template"
	"net/http"
	"path/filepath"

	"github.com/agjmills/trove/internal/templateutil"
)

var templates *template.Template

// LoadTemplates parses HTML templates from the web/templates directory and registers
// helper functions for use by those templates.
//
// It builds a template.FuncMap with the following helpers:
//   - formatBytes: formats a byte count into human-readable units.
//   - storagePercentage(used, quota int64) int: returns 0 if quota == 0, otherwise computes
//     (used*100)/quota and clamps the result to a maximum of 100.
//   - add(a, b int) int, mul(a, b int64) int64, div(a, b int64) int64: basic arithmetic
//     helpers (div returns 0 when the divisor is 0).
//
// The parsed templates are assigned to the package-level `templates` variable.
// Returns any error encountered while parsing the template files.
func LoadTemplates() error {
	var err error
	templates, err = template.New("").Funcs(templateutil.FuncMap()).ParseGlob(filepath.Join("web", "templates", "*.html"))
	if err != nil {
		return err
	}
	// Parse partials if they exist
	partials, _ := filepath.Glob(filepath.Join("web", "templates", "partials", "*.html"))
	if len(partials) > 0 {
		templates, err = templates.ParseFiles(partials...)
	}
	return err
}

func render(w http.ResponseWriter, page string, data map[string]any) error {
	tmpl, err := templates.Clone()
	if err != nil {
		return err
	}

	_, err = tmpl.ParseFiles(filepath.Join("web", "templates", page))
	if err != nil {
		return err
	}

	return tmpl.ExecuteTemplate(w, "layout.html", data)
}

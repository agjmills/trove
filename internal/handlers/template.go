package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
)

var templates *template.Template

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

// LoadTemplates parses HTML templates from the web/templates directory and registers
// helper functions for use by those templates.
//
// It builds a template.FuncMap with the following helpers:
// - formatBytes: formats a byte count into human-readable units.
// - storagePercentage(used, quota int64) int: returns 0 if quota == 0, otherwise computes
//   (used*100)/quota and clamps the result to a maximum of 100.
// - add(a, b int) int, mul(a, b int64) int64, div(a, b int64) int64: basic arithmetic
//   helpers (div returns 0 when the divisor is 0).
//
// The parsed templates are assigned to the package-level `templates` variable.
// Returns any error encountered while parsing the template files.
func LoadTemplates() error {
	funcMap := template.FuncMap{
		"formatBytes": formatBytes,
		"storagePercentage": func(used, quota int64) int {
			if quota == 0 {
				return 0
			}
			percentage := (used * 100) / quota
			if percentage > 100 {
				return 100
			}
			return int(percentage)
		},
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
	templates, err = template.New("").Funcs(funcMap).ParseGlob(filepath.Join("web", "templates", "*.html"))
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
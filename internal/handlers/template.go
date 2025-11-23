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

func LoadTemplates() error {
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

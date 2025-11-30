// Package templateutil provides shared template helper functions for use across
// different template loading contexts (handlers, middleware, etc.).
package templateutil

import (
	"encoding/json"
	"fmt"
	"html/template"
)

// FormatBytes formats a byte count into human-readable units (B, KB, MB, etc.).
func FormatBytes(bytes int64) string {
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

// StoragePercentage calculates the percentage of storage used.
// Returns 0 if quota is 0; otherwise computes (used*100)/quota capped at 100.
func StoragePercentage(used, quota int64) int {
	if quota == 0 {
		return 0
	}
	percentage := (used * 100) / quota
	if percentage > 100 {
		return 100
	}
	return int(percentage)
}

// Add returns the sum of two integers.
func Add(a, b int) int {
	return a + b
}

// Mul returns the product of two int64 values.
func Mul(a, b int64) int64 {
	return a * b
}

// Div returns the quotient of two int64 values, or 0 if the divisor is 0.
func Div(a, b int64) int64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// DivFloat returns the quotient of two uint64 values as a float64.
// Returns 0 if the divisor is 0.
func DivFloat(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// MulFloat multiplies a float64 by an integer.
func MulFloat(a float64, b int) float64 {
	return a * float64(b)
}

// SanitizeID converts a string into a safe HTML element ID.
// It replaces spaces and non-alphanumeric characters (except hyphen and underscore)
// with underscores, and prefixes with "id-" to ensure the ID starts with a letter.
func SanitizeID(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return "id-" + string(result)
}

// JsStr encodes a string as a JSON string for safe use in JavaScript contexts.
// This properly escapes special characters like quotes, backslashes, and newlines.
// The result is already quoted, so it can be used directly as a JavaScript string literal.
func JsStr(s string) template.JS {
	b, err := json.Marshal(s)
	if err != nil {
		// Fallback to empty string on error
		return template.JS(`""`)
	}
	return template.JS(b)
}

// FuncMap returns a template.FuncMap with all the standard template helpers.
func FuncMap() template.FuncMap {
	return template.FuncMap{
		"formatBytes":       FormatBytes,
		"storagePercentage": StoragePercentage,
		"add":               Add,
		"mul":               Mul,
		"div":               Div,
		"divFloat":          DivFloat,
		"mulFloat":          MulFloat,
		"sanitizeID":        SanitizeID,
		"jsStr":             JsStr,
	}
}

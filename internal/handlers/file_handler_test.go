package handlers

import "testing"

func TestEscapeSQLLike(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no special characters",
			input:    "normal_folder",
			expected: `normal\_folder`,
		},
		{
			name:     "percent sign",
			input:    "folder%name",
			expected: `folder\%name`,
		},
		{
			name:     "underscore",
			input:    "folder_name",
			expected: `folder\_name`,
		},
		{
			name:     "backslash",
			input:    `folder\name`,
			expected: `folder\\name`,
		},
		{
			name:     "all special characters",
			input:    `test\%_folder`,
			expected: `test\\\%\_folder`,
		},
		{
			name:     "multiple percent signs",
			input:    "%%data%%",
			expected: `\%\%data\%\%`,
		},
		{
			name:     "multiple underscores",
			input:    "___test___",
			expected: `\_\_\_test\_\_\_`,
		},
		{
			name:     "multiple backslashes",
			input:    `\\\`,
			expected: `\\\\\\`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "mixed special characters",
			input:    `path\with%under_score`,
			expected: `path\\with\%under\_score`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeSQLLike(tt.input)
			if result != tt.expected {
				t.Errorf("escapeSQLLike(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
func TestIsCodeFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected bool
	}{
		{
			name:     "go file",
			filename: "main.go",
			expected: true,
		},
		{
			name:     "python file",
			filename: "script.py",
			expected: true,
		},
		{
			name:     "javascript file",
			filename: "app.js",
			expected: true,
		},
		{
			name:     "typescript file",
			filename: "component.tsx",
			expected: true,
		},
		{
			name:     "markdown file",
			filename: "README.md",
			expected: true,
		},
		{
			name:     "json file",
			filename: "config.json",
			expected: true,
		},
		{
			name:     "yaml file",
			filename: "docker-compose.yml",
			expected: true,
		},
		{
			name:     "dockerfile",
			filename: "Dockerfile",
			expected: true,
		},
		{
			name:     "makefile",
			filename: "Makefile",
			expected: true,
		},
		{
			name:     "readme no extension",
			filename: "README",
			expected: true,
		},
		{
			name:     "shell script",
			filename: "build.sh",
			expected: true,
		},
		{
			name:     "image file",
			filename: "photo.jpg",
			expected: false,
		},
		{
			name:     "pdf file",
			filename: "document.pdf",
			expected: false,
		},
		{
			name:     "video file",
			filename: "movie.mp4",
			expected: false,
		},
		{
			name:     "audio file",
			filename: "song.mp3",
			expected: false,
		},
		{
			name:     "binary file",
			filename: "program.exe",
			expected: false,
		},
		{
			name:     "case insensitive",
			filename: "SCRIPT.PY",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isCodeFile(tt.filename)
			if result != tt.expected {
				t.Errorf("isCodeFile(%q) = %v, expected %v", tt.filename, result, tt.expected)
			}
		})
	}
}
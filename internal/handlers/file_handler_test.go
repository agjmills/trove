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

package templateutil

import "testing"

func TestSanitizeID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple name",
			input:    "documents",
			expected: "id-documents",
		},
		{
			name:     "name with spaces",
			input:    "my folder",
			expected: "id-my_folder",
		},
		{
			name:     "name with special characters",
			input:    "folder@#$%!name",
			expected: "id-folder_____name",
		},
		{
			name:     "name with hyphens and underscores",
			input:    "my-folder_name",
			expected: "id-my-folder_name",
		},
		{
			name:     "name with unicode",
			input:    "文档",
			expected: "id-______",
		},
		{
			name:     "name with numbers",
			input:    "folder123",
			expected: "id-folder123",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "id-",
		},
		{
			name:     "name starting with number",
			input:    "123folder",
			expected: "id-123folder",
		},
		{
			name:     "name with quotes and brackets",
			input:    "folder'name\"[test]",
			expected: "id-folder_name__test_",
		},
		{
			name:     "name with html special chars",
			input:    "folder<script>alert(1)</script>",
			expected: "id-folder_script_alert_1___script_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeID(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeIDUniqueness(t *testing.T) {
	// Test that different inputs produce different outputs
	// (except when they differ only in special characters)
	inputs := []string{
		"folder1",
		"folder2",
		"my-docs",
		"my_docs",
	}

	seen := make(map[string]string)
	for _, input := range inputs {
		result := SanitizeID(input)
		if prev, exists := seen[result]; exists {
			t.Errorf("SanitizeID collision: %q and %q both produce %q", prev, input, result)
		}
		seen[result] = input
	}
}

package templateutil

import (
	"html/template"
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    int64
		expected string
	}{
		{"zero bytes", 0, "0 B"},
		{"small bytes", 500, "500 B"},
		{"exactly 1 KB", 1024, "1.0 KB"},
		{"small KB", 2048, "2.0 KB"},
		{"exactly 1 MB", 1048576, "1.0 MB"},
		{"large MB", 52428800, "50.0 MB"},
		{"exactly 1 GB", 1073741824, "1.0 GB"},
		{"multiple GB", 5368709120, "5.0 GB"},
		{"exactly 1 TB", 1099511627776, "1.0 TB"},
		{"fractional KB", 1536, "1.5 KB"},
		{"fractional MB", 1572864, "1.5 MB"},
		{"large value", 10995116277760, "10.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			if result != tt.expected {
				t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestStoragePercentage(t *testing.T) {
	tests := []struct {
		name     string
		used     int64
		quota    int64
		expected int
	}{
		{"zero usage, zero quota", 0, 0, 0},
		{"zero usage, non-zero quota", 0, 1000, 0},
		{"half used", 500, 1000, 50},
		{"full usage", 1000, 1000, 100},
		{"over quota capped at 100", 1500, 1000, 100},
		{"small percentage", 10, 1000, 1},
		{"99 percent", 990, 1000, 99},
		{"large numbers", 5368709120, 10737418240, 50},
		{"just over half", 501, 1000, 50},
		{"just under full", 999, 1000, 99},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StoragePercentage(tt.used, tt.quota)
			if result != tt.expected {
				t.Errorf("StoragePercentage(%d, %d) = %d, want %d", tt.used, tt.quota, result, tt.expected)
			}
		})
	}
}

func TestAdd(t *testing.T) {
	tests := []struct {
		a, b, expected int
	}{
		{0, 0, 0},
		{1, 1, 2},
		{5, 3, 8},
		{-5, 3, -2},
		{-5, -3, -8},
		{100, 200, 300},
	}

	for _, tt := range tests {
		result := Add(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("Add(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestMul(t *testing.T) {
	tests := []struct {
		a, b, expected int64
	}{
		{0, 0, 0},
		{1, 1, 1},
		{5, 3, 15},
		{-5, 3, -15},
		{-5, -3, 15},
		{100, 200, 20000},
		{1024, 1024, 1048576},
	}

	for _, tt := range tests {
		result := Mul(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("Mul(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestDiv(t *testing.T) {
	tests := []struct {
		name     string
		a, b     int64
		expected int64
	}{
		{"normal division", 10, 2, 5},
		{"division with remainder", 10, 3, 3},
		{"zero numerator", 0, 5, 0},
		{"division by zero", 10, 0, 0},
		{"exact division", 100, 10, 10},
		{"negative division", -10, 2, -5},
		{"both negative", -10, -2, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Div(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Div(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestDivFloat(t *testing.T) {
	tests := []struct {
		name     string
		a, b     uint64
		expected float64
	}{
		{"normal division", 10, 2, 5.0},
		{"division with decimal", 10, 3, 3.3333333333333335},
		{"zero numerator", 0, 5, 0.0},
		{"division by zero", 10, 0, 0.0},
		{"exact division", 100, 10, 10.0},
		{"fractional result", 1, 2, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DivFloat(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("DivFloat(%d, %d) = %f, want %f", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestMulFloat(t *testing.T) {
	tests := []struct {
		a        float64
		b        int
		expected float64
	}{
		{2.5, 2, 5.0},
		{0.0, 10, 0.0},
		{1.5, 3, 4.5},
		{-2.5, 2, -5.0},
		{10.5, 0, 0.0},
		{3.14, 10, 31.4},
	}

	for _, tt := range tests {
		result := MulFloat(tt.a, tt.b)
		// Use a small epsilon for floating point comparison
		epsilon := 0.0001
		if diff := result - tt.expected; diff < -epsilon || diff > epsilon {
			t.Errorf("MulFloat(%f, %d) = %f, want %f", tt.a, tt.b, result, tt.expected)
		}
	}
}

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

func TestJsStr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple string",
			input:    "hello",
			expected: `"hello"`,
		},
		{
			name:     "string with quotes",
			input:    `say "hello"`,
			expected: `"say \"hello\""`,
		},
		{
			name:     "string with backslash",
			input:    `path\to\file`,
			expected: `"path\\to\\file"`,
		},
		{
			name:     "string with newline",
			input:    "line1\nline2",
			expected: `"line1\nline2"`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: `""`,
		},
		{
			name:     "folder path",
			input:    "/documents/my folder",
			expected: `"/documents/my folder"`,
		},
		{
			name:     "XSS attempt",
			input:    `</script><script>alert(1)</script>`,
			expected: `"\u003c/script\u003e\u003cscript\u003ealert(1)\u003c/script\u003e"`,
		},
		{
			name:     "unicode characters",
			input:    "文档",
			expected: `"文档"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := string(JsStr(tt.input))
			if result != tt.expected {
				t.Errorf("JsStr(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFuncMap(t *testing.T) {
	funcMap := FuncMap()

	// Check that all expected functions are present
	expectedFuncs := []string{
		"formatBytes",
		"storagePercentage",
		"add",
		"mul",
		"div",
		"divFloat",
		"mulFloat",
		"sanitizeID",
		"jsStr",
	}

	for _, funcName := range expectedFuncs {
		if _, exists := funcMap[funcName]; !exists {
			t.Errorf("FuncMap missing expected function: %s", funcName)
		}
	}

	// Verify the count matches
	if len(funcMap) != len(expectedFuncs) {
		t.Errorf("FuncMap has %d functions, expected %d", len(funcMap), len(expectedFuncs))
	}
}

func TestFuncMapIntegration(t *testing.T) {
	// Test that FuncMap can be used with html/template
	tmpl, err := template.New("test").Funcs(FuncMap()).Parse(`
		{{formatBytes 1024}}
		{{storagePercentage 50 100}}
		{{add 5 3}}
		{{mul 4 5}}
		{{div 10 2}}
		{{divFloat 10 4}}
		{{mulFloat 2.5 4}}
		{{sanitizeID "my folder"}}
		{{jsStr "test string"}}
	`)

	if err != nil {
		t.Fatalf("Failed to parse template with FuncMap: %v", err)
	}

	if tmpl == nil {
		t.Fatal("Template should not be nil")
	}
}


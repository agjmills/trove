package config

import (
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		// Bytes
		{name: "plain bytes", input: "1024", want: 1024, wantErr: false},
		{name: "bytes with B", input: "512B", want: 512, wantErr: false},
		{name: "zero bytes", input: "0", want: 0, wantErr: false},

		// Kilobytes
		{name: "kilobytes K", input: "10K", want: 10 * 1024, wantErr: false},
		{name: "kilobytes KB", input: "10KB", want: 10 * 1024, wantErr: false},
		{name: "lowercase k", input: "5k", want: 5 * 1024, wantErr: false},
		{name: "lowercase kb", input: "5kb", want: 5 * 1024, wantErr: false},
		{name: "decimal kilobytes", input: "1.5K", want: int64(1.5 * 1024), wantErr: false},

		// Megabytes
		{name: "megabytes M", input: "500M", want: 500 * 1024 * 1024, wantErr: false},
		{name: "megabytes MB", input: "500MB", want: 500 * 1024 * 1024, wantErr: false},
		{name: "lowercase m", input: "100m", want: 100 * 1024 * 1024, wantErr: false},
		{name: "lowercase mb", input: "100mb", want: 100 * 1024 * 1024, wantErr: false},
		{name: "decimal megabytes", input: "2.5M", want: int64(2.5 * 1024 * 1024), wantErr: false},

		// Gigabytes
		{name: "gigabytes G", input: "10G", want: 10 * 1024 * 1024 * 1024, wantErr: false},
		{name: "gigabytes GB", input: "10GB", want: 10 * 1024 * 1024 * 1024, wantErr: false},
		{name: "lowercase g", input: "5g", want: 5 * 1024 * 1024 * 1024, wantErr: false},
		{name: "lowercase gb", input: "5gb", want: 5 * 1024 * 1024 * 1024, wantErr: false},
		{name: "decimal gigabytes", input: "1.5G", want: int64(1.5 * 1024 * 1024 * 1024), wantErr: false},

		// Terabytes
		{name: "terabytes T", input: "2T", want: 2 * 1024 * 1024 * 1024 * 1024, wantErr: false},
		{name: "terabytes TB", input: "2TB", want: 2 * 1024 * 1024 * 1024 * 1024, wantErr: false},
		{name: "lowercase t", input: "1t", want: 1 * 1024 * 1024 * 1024 * 1024, wantErr: false},
		{name: "lowercase tb", input: "1tb", want: 1 * 1024 * 1024 * 1024 * 1024, wantErr: false},
		{name: "decimal terabytes", input: "0.5T", want: int64(0.5 * 1024 * 1024 * 1024 * 1024), wantErr: false},

		// Whitespace handling
		{name: "with leading space", input: " 10M", want: 10 * 1024 * 1024, wantErr: false},
		{name: "with trailing space", input: "10M ", want: 10 * 1024 * 1024, wantErr: false},
		{name: "with both spaces", input: " 10M ", want: 10 * 1024 * 1024, wantErr: false},

		// Error cases
		{name: "empty string", input: "", want: 0, wantErr: true},
		{name: "invalid unit", input: "10X", want: 0, wantErr: true},
		{name: "invalid number", input: "abcM", want: 0, wantErr: true},
		{name: "only unit", input: "M", want: 0, wantErr: true},
		{name: "multiple decimal points", input: "1.5.5M", want: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSizeCommonValues(t *testing.T) {
	// Test common configuration values from the plan
	tests := []struct {
		input string
		want  int64
	}{
		{"10G", 10 * 1024 * 1024 * 1024}, // DEFAULT_USER_QUOTA
		{"500M", 500 * 1024 * 1024},      // MAX_UPLOAD_SIZE
		{"1GB", 1 * 1024 * 1024 * 1024},  // Common quota
		{"100MB", 100 * 1024 * 1024},     // Common upload size
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseSize(tt.input)
			if err != nil {
				t.Errorf("parseSize(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got != tt.want {
				t.Errorf("parseSize(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetEnvWithDefaults(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue string
		want         string
	}{
		{
			name:         "returns default when env not set",
			key:          "NONEXISTENT_KEY_12345",
			defaultValue: "default_value",
			want:         "default_value",
		},
		{
			name:         "returns empty default",
			key:          "NONEXISTENT_KEY_67890",
			defaultValue: "",
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnv(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnv(%q, %q) = %q, want %q", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue int
		want         int
	}{
		{
			name:         "returns default when env not set",
			key:          "NONEXISTENT_INT_KEY_12345",
			defaultValue: 42,
			want:         42,
		},
		{
			name:         "returns zero default",
			key:          "NONEXISTENT_INT_KEY_67890",
			defaultValue: 0,
			want:         0,
		},
		{
			name:         "returns negative default",
			key:          "NONEXISTENT_INT_KEY_99999",
			defaultValue: -1,
			want:         -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnvInt(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvInt(%q, %d) = %d, want %d", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	tests := []struct {
		name         string
		key          string
		defaultValue bool
		want         bool
	}{
		{
			name:         "returns default true when env not set",
			key:          "NONEXISTENT_BOOL_KEY_12345",
			defaultValue: true,
			want:         true,
		},
		{
			name:         "returns default false when env not set",
			key:          "NONEXISTENT_BOOL_KEY_67890",
			defaultValue: false,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getEnvBool(tt.key, tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvBool(%q, %v) = %v, want %v", tt.key, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// This test verifies the default configuration values load correctly
	// when no environment variables are set
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if cfg == nil {
		t.Fatal("Load() returned nil config")
	}

	// Verify some key defaults
	if cfg.Port != "8080" {
		t.Errorf("Expected default Port=8080, got %s", cfg.Port)
	}

	if cfg.Host != "0.0.0.0" {
		t.Errorf("Expected default Host=0.0.0.0, got %s", cfg.Host)
	}

	if cfg.DBType != "sqlite" {
		t.Errorf("Expected default DBType=sqlite, got %s", cfg.DBType)
	}

	if cfg.DefaultUserQuota != 10*1024*1024*1024 {
		t.Errorf("Expected DefaultUserQuota=10G, got %d", cfg.DefaultUserQuota)
	}

	if cfg.MaxUploadSize != 500*1024*1024 {
		t.Errorf("Expected MaxUploadSize=500M, got %d", cfg.MaxUploadSize)
	}

	if cfg.BcryptCost != 10 {
		t.Errorf("Expected BcryptCost=10, got %d", cfg.BcryptCost)
	}

	if !cfg.CSRFEnabled {
		t.Error("Expected CSRFEnabled=true by default")
	}

	if !cfg.EnableRegistration {
		t.Error("Expected EnableRegistration=true by default")
	}

	if !cfg.EnableFileDeduplication {
		t.Error("Expected EnableFileDeduplication=true by default")
	}
}

func TestParseSizeEdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "very large number", input: "9999999T", wantErr: false},
		{name: "very small decimal", input: "0.0001M", wantErr: false},
		{name: "many decimal places", input: "1.123456789G", wantErr: false},
		{name: "zero with unit", input: "0M", wantErr: false},
		{name: "space in middle", input: "10 M", wantErr: true},
		{name: "mixed case", input: "10Mb", wantErr: false},
		{name: "just B", input: "B", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseSize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseSize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

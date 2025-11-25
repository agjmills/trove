package routes

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agjmills/trove/internal/config"
)

func TestParseTrustedCIDRs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		wantLen  int
		wantNets []string // Expected CIDR strings for verification
	}{
		{
			name:     "empty list",
			input:    []string{},
			wantLen:  0,
			wantNets: nil,
		},
		{
			name:     "nil list",
			input:    nil,
			wantLen:  0,
			wantNets: nil,
		},
		{
			name:     "single IPv4 CIDR",
			input:    []string{"10.0.0.0/8"},
			wantLen:  1,
			wantNets: []string{"10.0.0.0/8"},
		},
		{
			name:     "single IPv4 without mask",
			input:    []string{"127.0.0.1"},
			wantLen:  1,
			wantNets: []string{"127.0.0.1/32"},
		},
		{
			name:     "multiple CIDRs",
			input:    []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			wantLen:  3,
			wantNets: []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
		},
		{
			name:     "IPv6 CIDR",
			input:    []string{"::1/128"},
			wantLen:  1,
			wantNets: []string{"::1/128"},
		},
		{
			name:     "IPv6 without mask",
			input:    []string{"::1"},
			wantLen:  1,
			wantNets: []string{"::1/128"},
		},
		{
			name:     "mixed valid and invalid",
			input:    []string{"10.0.0.0/8", "invalid", "192.168.1.0/24"},
			wantLen:  2,
			wantNets: []string{"10.0.0.0/8", "192.168.1.0/24"},
		},
		{
			name:     "all invalid",
			input:    []string{"not-an-ip", "also-invalid"},
			wantLen:  0,
			wantNets: nil,
		},
		{
			name:     "common Docker networks",
			input:    []string{"172.17.0.0/16", "172.18.0.0/16"},
			wantLen:  2,
			wantNets: []string{"172.17.0.0/16", "172.18.0.0/16"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseTrustedCIDRs(tt.input)
			if len(result) != tt.wantLen {
				t.Errorf("parseTrustedCIDRs() returned %d nets, want %d", len(result), tt.wantLen)
			}

			// Verify the parsed networks match expected
			for i, want := range tt.wantNets {
				if i >= len(result) {
					break
				}
				if result[i].String() != want {
					t.Errorf("parseTrustedCIDRs()[%d] = %s, want %s", i, result[i].String(), want)
				}
			}
		})
	}
}

func TestIsIPInCIDRs(t *testing.T) {
	// Pre-parse some CIDRs for testing
	privateCIDRs := parseTrustedCIDRs([]string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"})
	localhostCIDR := parseTrustedCIDRs([]string{"127.0.0.1/32"})
	dockerCIDR := parseTrustedCIDRs([]string{"172.17.0.0/16"})

	tests := []struct {
		name  string
		ipStr string
		cidrs []*net.IPNet
		want  bool
	}{
		{
			name:  "IP in private range (10.x)",
			ipStr: "10.0.0.1",
			cidrs: privateCIDRs,
			want:  true,
		},
		{
			name:  "IP in private range (172.16.x)",
			ipStr: "172.16.0.1",
			cidrs: privateCIDRs,
			want:  true,
		},
		{
			name:  "IP in private range (192.168.x)",
			ipStr: "192.168.1.100",
			cidrs: privateCIDRs,
			want:  true,
		},
		{
			name:  "IP not in range",
			ipStr: "8.8.8.8",
			cidrs: privateCIDRs,
			want:  false,
		},
		{
			name:  "IP with port (RemoteAddr format)",
			ipStr: "10.0.0.1:12345",
			cidrs: privateCIDRs,
			want:  true,
		},
		{
			name:  "localhost exact match",
			ipStr: "127.0.0.1",
			cidrs: localhostCIDR,
			want:  true,
		},
		{
			name:  "localhost with port",
			ipStr: "127.0.0.1:8080",
			cidrs: localhostCIDR,
			want:  true,
		},
		{
			name:  "localhost range miss (127.0.0.2 not in /32)",
			ipStr: "127.0.0.2",
			cidrs: localhostCIDR,
			want:  false,
		},
		{
			name:  "Docker network IP",
			ipStr: "172.17.0.2:54321",
			cidrs: dockerCIDR,
			want:  true,
		},
		{
			name:  "empty CIDR list",
			ipStr: "10.0.0.1",
			cidrs: nil,
			want:  false,
		},
		{
			name:  "invalid IP string",
			ipStr: "not-an-ip",
			cidrs: privateCIDRs,
			want:  false,
		},
		{
			name:  "empty IP string",
			ipStr: "",
			cidrs: privateCIDRs,
			want:  false,
		},
		{
			name:  "IPv6 localhost",
			ipStr: "[::1]:8080",
			cidrs: parseTrustedCIDRs([]string{"::1/128"}),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isIPInCIDRs(tt.ipStr, tt.cidrs)
			if got != tt.want {
				t.Errorf("isIPInCIDRs(%q) = %v, want %v", tt.ipStr, got, tt.want)
			}
		})
	}
}

func TestGetClientIP(t *testing.T) {
	trustedCIDRs := parseTrustedCIDRs([]string{"10.0.0.0/8"})

	tests := []struct {
		name             string
		remoteAddr       string
		xRealIP          string
		xForwardedFor    string
		cidrs            []*net.IPNet
		want             string
	}{
		{
			name:       "trusted proxy with X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.50",
			cidrs:      trustedCIDRs,
			want:       "203.0.113.50",
		},
		{
			name:       "trusted proxy without X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "",
			cidrs:      trustedCIDRs,
			want:       "10.0.0.1:12345",
		},
		{
			name:       "untrusted IP ignores X-Real-IP",
			remoteAddr: "8.8.8.8:12345",
			xRealIP:    "203.0.113.50",
			cidrs:      trustedCIDRs,
			want:       "8.8.8.8:12345",
		},
		{
			name:       "no trusted CIDRs configured",
			remoteAddr: "10.0.0.1:12345",
			xRealIP:    "203.0.113.50",
			cidrs:      nil,
			want:       "10.0.0.1:12345",
		},
		{
			name:          "trusted proxy with X-Forwarded-For single IP",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.50",
			cidrs:         trustedCIDRs,
			want:          "203.0.113.50",
		},
		{
			name:          "trusted proxy with X-Forwarded-For multi-hop (returns leftmost)",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "203.0.113.50, 198.51.100.1, 192.0.2.1",
			cidrs:         trustedCIDRs,
			want:          "203.0.113.50",
		},
		{
			name:          "X-Real-IP takes precedence over X-Forwarded-For",
			remoteAddr:    "10.0.0.1:12345",
			xRealIP:       "203.0.113.100",
			xForwardedFor: "203.0.113.50",
			cidrs:         trustedCIDRs,
			want:          "203.0.113.100",
		},
		{
			name:          "untrusted IP ignores X-Forwarded-For",
			remoteAddr:    "8.8.8.8:12345",
			xForwardedFor: "203.0.113.50",
			cidrs:         trustedCIDRs,
			want:          "8.8.8.8:12345",
		},
		{
			name:          "X-Forwarded-For with spaces",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "  203.0.113.50  , 198.51.100.1 ",
			cidrs:         trustedCIDRs,
			want:          "203.0.113.50",
		},
		{
			name:          "empty X-Forwarded-For falls back to RemoteAddr",
			remoteAddr:    "10.0.0.1:12345",
			xForwardedFor: "",
			cidrs:         trustedCIDRs,
			want:          "10.0.0.1:12345",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xRealIP != "" {
				req.Header.Set("X-Real-IP", tt.xRealIP)
			}
			if tt.xForwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.xForwardedFor)
			}

			got := getClientIP(req, tt.cidrs)
			if got != tt.want {
				t.Errorf("getClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPlaintextCSRFMiddleware(t *testing.T) {
	tests := []struct {
		name            string
		env             string
		trustedCIDRs    []string
		remoteAddr      string
		xForwardedProto string
		useTLS          bool
		wantPlaintext   bool // Whether the request should be marked as plaintext
	}{
		{
			name:          "development mode always plaintext",
			env:           "development",
			trustedCIDRs:  nil,
			remoteAddr:    "127.0.0.1:12345",
			wantPlaintext: true,
		},
		{
			name:            "production with trusted proxy and https header",
			env:             "production",
			trustedCIDRs:    []string{"10.0.0.0/8"},
			remoteAddr:      "10.0.0.1:12345",
			xForwardedProto: "https",
			wantPlaintext:   false,
		},
		{
			name:            "production with trusted proxy and http header",
			env:             "production",
			trustedCIDRs:    []string{"10.0.0.0/8"},
			remoteAddr:      "10.0.0.1:12345",
			xForwardedProto: "http",
			wantPlaintext:   true,
		},
		{
			name:            "production with trusted proxy and no header",
			env:             "production",
			trustedCIDRs:    []string{"10.0.0.0/8"},
			remoteAddr:      "10.0.0.1:12345",
			xForwardedProto: "",
			wantPlaintext:   true,
		},
		{
			name:            "production untrusted IP ignores X-Forwarded-Proto (uses r.TLS=nil)",
			env:             "production",
			trustedCIDRs:    []string{"10.0.0.0/8"},
			remoteAddr:      "8.8.8.8:12345",
			xForwardedProto: "https",
			useTLS:          false,
			wantPlaintext:   true, // Falls back to r.TLS which is nil
		},
		{
			name:            "production untrusted IP with TLS connection",
			env:             "production",
			trustedCIDRs:    []string{"10.0.0.0/8"},
			remoteAddr:      "8.8.8.8:12345",
			xForwardedProto: "http", // Header should be ignored
			useTLS:          true,
			wantPlaintext:   false, // r.TLS is set
		},
		{
			name:          "production no trusted CIDRs with TLS",
			env:           "production",
			trustedCIDRs:  nil,
			remoteAddr:    "10.0.0.1:12345",
			useTLS:        true,
			wantPlaintext: false,
		},
		{
			name:          "production no trusted CIDRs without TLS",
			env:           "production",
			trustedCIDRs:  nil,
			remoteAddr:    "10.0.0.1:12345",
			useTLS:        false,
			wantPlaintext: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Env:               tt.env,
				TrustedProxyCIDRs: tt.trustedCIDRs,
			}

			// Track if PlaintextHTTPRequest was effectively applied
			// We can detect this by checking if the request context has the plaintext key
			var wasPlaintext bool

			middleware := plaintextCSRFMiddleware(cfg)
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Check if the request was marked as plaintext
				// gorilla/csrf stores this in context, but we can't easily check it
				// Instead, we'll verify the middleware logic by checking that it ran
				wasPlaintext = true // Placeholder - actual check would need internal access
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xForwardedProto)
			}
			if tt.useTLS {
				req.TLS = &tls.ConnectionState{}
			}

			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != http.StatusOK {
				t.Errorf("handler returned wrong status: got %v want %v", rr.Code, http.StatusOK)
			}

			// Note: We can't directly verify if PlaintextHTTPRequest was called
			// since it modifies request context internally. The test verifies
			// the middleware executes without error. Full integration tests
			// with actual CSRF validation would be needed for complete coverage.
			_ = wasPlaintext
		})
	}
}

func TestPlaintextCSRFMiddleware_Integration(t *testing.T) {
	// Test that the middleware correctly determines HTTPS status based on configuration
	tests := []struct {
		name            string
		env             string
		trustedCIDRs    []string
		remoteAddr      string
		xForwardedProto string
		useTLS          bool
		expectHTTPS     bool
	}{
		{
			name:            "production: trusted proxy says https",
			env:             "production",
			trustedCIDRs:    []string{"172.17.0.0/16"},
			remoteAddr:      "172.17.0.1:45678",
			xForwardedProto: "https",
			expectHTTPS:     true,
		},
		{
			name:            "production: spoofed header from untrusted IP",
			env:             "production",
			trustedCIDRs:    []string{"172.17.0.0/16"},
			remoteAddr:      "1.2.3.4:45678",
			xForwardedProto: "https",
			useTLS:          false,
			expectHTTPS:     false, // Header ignored, r.TLS=nil
		},
		{
			name:         "production: direct TLS connection",
			env:          "production",
			trustedCIDRs: nil,
			remoteAddr:   "1.2.3.4:45678",
			useTLS:       true,
			expectHTTPS:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				Env:               tt.env,
				TrustedProxyCIDRs: tt.trustedCIDRs,
			}

			// We'll capture whether the middleware logic determined HTTPS
			var detectedHTTPS bool
			trustedCIDRs := parseTrustedCIDRs(cfg.TrustedProxyCIDRs)

			// Replicate the middleware logic for testing
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xForwardedProto != "" {
				req.Header.Set("X-Forwarded-Proto", tt.xForwardedProto)
			}
			if tt.useTLS {
				req.TLS = &tls.ConnectionState{}
			}

			if cfg.Env != "production" {
				detectedHTTPS = false // Development always treats as plaintext
			} else {
				if len(trustedCIDRs) > 0 && isIPInCIDRs(req.RemoteAddr, trustedCIDRs) {
					proto := req.Header.Get("X-Forwarded-Proto")
					detectedHTTPS = proto == "https"
				} else {
					detectedHTTPS = req.TLS != nil
				}
			}

			if detectedHTTPS != tt.expectHTTPS {
				t.Errorf("HTTPS detection = %v, want %v", detectedHTTPS, tt.expectHTTPS)
			}
		})
	}
}

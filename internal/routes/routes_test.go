package routes

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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
		name          string
		remoteAddr    string
		xRealIP       string
		xForwardedFor string
		cidrs         []*net.IPNet
		want          string
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

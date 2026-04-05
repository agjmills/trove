package oidc

import (
	"testing"

	"github.com/agjmills/trove/internal/config"
)

func providerWithClaims(usernameClaim, emailClaim, adminClaim, adminValue string) *Provider {
	return &Provider{
		cfg: &config.Config{
			OIDCUsernameClaim: usernameClaim,
			OIDCEmailClaim:    emailClaim,
			OIDCAdminClaim:    adminClaim,
			OIDCAdminValue:    adminValue,
		},
	}
}

func TestExtractClaims_BasicFields(t *testing.T) {
	p := providerWithClaims("preferred_username", "email", "", "")
	raw := map[string]any{
		"sub":                "user-123",
		"email":              "alice@example.com",
		"preferred_username": "alice",
	}
	c := p.ExtractClaims(raw)

	if c.Subject != "user-123" {
		t.Errorf("Subject: want user-123, got %q", c.Subject)
	}
	if c.Email != "alice@example.com" {
		t.Errorf("Email: want alice@example.com, got %q", c.Email)
	}
	if c.Username != "alice" {
		t.Errorf("Username: want alice, got %q", c.Username)
	}
	if c.IsAdmin {
		t.Error("IsAdmin: expected false when no admin claim configured")
	}
}

func TestExtractClaims_MissingOptionalFields(t *testing.T) {
	p := providerWithClaims("preferred_username", "email", "", "")
	raw := map[string]any{
		"sub":   "user-456",
		"email": "bob@example.com",
		// no preferred_username
	}
	c := p.ExtractClaims(raw)

	if c.Subject != "user-456" {
		t.Errorf("Subject: want user-456, got %q", c.Subject)
	}
	if c.Username != "" {
		t.Errorf("Username: want empty string when claim absent, got %q", c.Username)
	}
}

func TestExtractClaims_CustomClaimNames(t *testing.T) {
	p := providerWithClaims("name", "mail", "", "")
	raw := map[string]any{
		"sub":  "u1",
		"mail": "charlie@example.com",
		"name": "charlie",
	}
	c := p.ExtractClaims(raw)

	if c.Email != "charlie@example.com" {
		t.Errorf("Email: want charlie@example.com, got %q", c.Email)
	}
	if c.Username != "charlie" {
		t.Errorf("Username: want charlie, got %q", c.Username)
	}
}

func TestExtractClaims_AdminStringClaim(t *testing.T) {
	p := providerWithClaims("preferred_username", "email", "role", "admin")
	raw := map[string]any{
		"sub":   "u1",
		"email": "admin@example.com",
		"role":  "admin",
	}
	if !p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected true for matching string claim")
	}

	raw["role"] = "viewer"
	if p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected false for non-matching string claim")
	}
}

func TestExtractClaims_AdminGroupArrayClaim(t *testing.T) {
	// Authentik / Keycloak groups style
	p := providerWithClaims("preferred_username", "email", "groups", "trove-admins")
	raw := map[string]any{
		"sub":    "u1",
		"email":  "user@example.com",
		"groups": []any{"developers", "trove-admins", "ops"},
	}
	if !p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected true when admin group present in array")
	}

	raw["groups"] = []any{"developers", "ops"}
	if p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected false when admin group absent from array")
	}
}

func TestExtractClaims_AdminBoolClaim(t *testing.T) {
	// Keycloak custom boolean mapper
	p := providerWithClaims("preferred_username", "email", "is_trove_admin", "true")
	raw := map[string]any{
		"sub":            "u1",
		"email":          "user@example.com",
		"is_trove_admin": true,
	}
	if !p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected true for bool true claim")
	}

	raw["is_trove_admin"] = false
	if p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected false for bool false claim")
	}
}

func TestExtractClaims_AdminClaimNotPresent(t *testing.T) {
	p := providerWithClaims("preferred_username", "email", "groups", "trove-admins")
	raw := map[string]any{
		"sub":   "u1",
		"email": "user@example.com",
		// no groups claim at all
	}
	if p.ExtractClaims(raw).IsAdmin {
		t.Error("IsAdmin: expected false when admin claim not present in token")
	}
}

func TestMatchesAdminValue_EmptyArray(t *testing.T) {
	if matchesAdminValue([]any{}, "admin") {
		t.Error("expected false for empty array")
	}
}

func TestMatchesAdminValue_NonStringArrayElements(t *testing.T) {
	// Array with non-string elements should not panic or match
	if matchesAdminValue([]any{42, nil, true}, "admin") {
		t.Error("expected false for array of non-string elements")
	}
}

func TestMatchesAdminValue_UnknownType(t *testing.T) {
	type customType struct{ name string }
	if matchesAdminValue(customType{"admin"}, "admin") {
		t.Error("expected false for unknown type")
	}
}

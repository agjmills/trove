package oidc

import (
	"context"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/agjmills/trove/internal/config"
)

// Provider wraps go-oidc and oauth2 config. Initialised once at startup via New.
type Provider struct {
	OAuth2Config oauth2.Config
	Verifier     *gooidc.IDTokenVerifier
	cfg          *config.Config
}

// New performs OIDC discovery against the issuer and returns an initialised Provider.
// It makes a network request; call once at startup and fail fast on error.
func New(ctx context.Context, cfg *config.Config) (*Provider, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.OIDCIssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery failed for issuer %q: %w", cfg.OIDCIssuerURL, err)
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.OIDCScopes,
	}

	verifier := provider.Verifier(&gooidc.Config{ClientID: cfg.OIDCClientID})

	return &Provider{
		OAuth2Config: oauth2Cfg,
		Verifier:     verifier,
		cfg:          cfg,
	}, nil
}

// Claims holds normalised identity information extracted from an ID token.
type Claims struct {
	Subject  string
	Email    string
	Username string
	IsAdmin  bool
}

// ExtractClaims maps raw ID token claims to a normalised Claims struct using
// the configured claim names and admin value.
func (p *Provider) ExtractClaims(raw map[string]interface{}) Claims {
	get := func(key string) string {
		if v, ok := raw[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	c := Claims{
		Subject:  get("sub"),
		Email:    get(p.cfg.OIDCEmailClaim),
		Username: get(p.cfg.OIDCUsernameClaim),
	}

	if p.cfg.OIDCAdminClaim != "" && p.cfg.OIDCAdminValue != "" {
		if v, ok := raw[p.cfg.OIDCAdminClaim]; ok {
			c.IsAdmin = matchesAdminValue(v, p.cfg.OIDCAdminValue)
		}
	}

	return c
}

// matchesAdminValue handles the three common claim shapes:
//   - string  (Authelia scalar role)
//   - []interface{} (Authentik/Keycloak groups array)
//   - bool (Keycloak custom boolean mapper)
func matchesAdminValue(claimValue interface{}, adminValue string) bool {
	switch v := claimValue.(type) {
	case string:
		return v == adminValue
	case []interface{}:
		for _, item := range v {
			if s, ok := item.(string); ok && s == adminValue {
				return true
			}
		}
	case bool:
		return v && adminValue == "true"
	}
	return false
}

package service

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"subtrackr/internal/models"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const OIDCHTTPTimeout = 10 * time.Second

// OIDCService performs provider discovery and protocol-level OIDC operations.
type OIDCService struct {
	settings   *SettingsService
	httpClient *http.Client
}

// OIDCIdentity is the verified identity returned by an OIDC provider.
type OIDCIdentity struct {
	Subject string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

func NewOIDCService(settings *SettingsService, httpClient *http.Client) *OIDCService {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	boundedClient := *httpClient
	if boundedClient.Timeout <= 0 || boundedClient.Timeout > OIDCHTTPTimeout {
		boundedClient.Timeout = OIDCHTTPTimeout
	}
	return &OIDCService{settings: settings, httpClient: &boundedClient}
}

// AuthorizationURL discovers the configured provider and builds a secure authorization request.
func (s *OIDCService) AuthorizationURL(ctx context.Context, redirectURI, state, nonce, codeVerifier string) (string, error) {
	config, provider, err := s.provider(ctx)
	if err != nil {
		return "", err
	}

	oauthConfig := oauth2Config(config, provider, redirectURI)
	return oauthConfig.AuthCodeURL(
		state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	), nil
}

// ExchangeAndVerify exchanges a code and verifies the signed ID token and nonce.
func (s *OIDCService) ExchangeAndVerify(ctx context.Context, redirectURI, code, codeVerifier, expectedNonce string) (*OIDCIdentity, error) {
	if code == "" || codeVerifier == "" || expectedNonce == "" {
		return nil, fmt.Errorf("incomplete OIDC callback")
	}

	ctx = oidc.ClientContext(ctx, s.httpClient)
	config, provider, err := s.provider(ctx)
	if err != nil {
		return nil, err
	}

	oauthConfig := oauth2Config(config, provider, redirectURI)
	token, err := oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(codeVerifier))
	if err != nil {
		return nil, fmt.Errorf("exchange OIDC authorization code: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, fmt.Errorf("OIDC provider response did not include an ID token")
	}

	idToken, err := provider.Verifier(&oidc.Config{ClientID: config.ClientID}).Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify OIDC ID token: %w", err)
	}
	var claims struct {
		OIDCIdentity
		Nonce string `json:"nonce"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("decode OIDC ID token claims: %w", err)
	}
	if claims.Nonce == "" || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		return nil, fmt.Errorf("invalid OIDC nonce")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("OIDC ID token is missing subject")
	}
	return &claims.OIDCIdentity, nil
}

func (s *OIDCService) provider(ctx context.Context) (*models.OIDCConfig, *oidc.Provider, error) {
	config, err := s.settings.GetOIDCConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("OIDC is not configured: %w", err)
	}
	if !config.Enabled || config.IssuerURL == "" || config.ClientID == "" || config.ClientSecret == "" {
		return nil, nil, fmt.Errorf("OIDC configuration is incomplete")
	}

	ctx = oidc.ClientContext(ctx, s.httpClient)
	provider, err := oidc.NewProvider(ctx, config.IssuerURL)
	if err != nil {
		return nil, nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	return config, provider, nil
}

func oauth2Config(config *models.OIDCConfig, provider *oidc.Provider, redirectURI string) oauth2.Config {
	scopes := config.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	return oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: config.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       scopes,
	}
}

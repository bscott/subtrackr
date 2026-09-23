package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"subtrackr/internal/models"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOIDCService_EnforcesBoundedHTTPTimeout(t *testing.T) {
	settings := setupSettingsTestDB(t)

	tests := []struct {
		name     string
		client   *http.Client
		expected time.Duration
	}{
		{name: "default client", client: nil, expected: OIDCHTTPTimeout},
		{name: "client without timeout", client: &http.Client{}, expected: OIDCHTTPTimeout},
		{name: "shorter caller timeout", client: &http.Client{Timeout: 2 * time.Second}, expected: 2 * time.Second},
		{name: "excessive caller timeout", client: &http.Client{Timeout: time.Hour}, expected: OIDCHTTPTimeout},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewOIDCService(settings, tt.client)
			assert.Equal(t, tt.expected, service.httpClient.Timeout)
			if tt.client != nil {
				assert.NotSame(t, tt.client, service.httpClient)
			}
		})
	}
}

func TestOIDCService_AuthorizationURLUsesDiscoveryNonceAndPKCE(t *testing.T) {
	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/openid-configuration", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                provider.URL,
			"authorization_endpoint":                provider.URL + "/authorize",
			"token_endpoint":                        provider.URL + "/token",
			"jwks_uri":                              provider.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}))
	}))
	defer provider.Close()

	settings := setupSettingsTestDB(t)
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		DisplayName:  "Pocket ID",
		IssuerURL:    provider.URL,
		ClientID:     "subtrackr-client",
		ClientSecret: "client-secret",
	}))
	service := NewOIDCService(settings, provider.Client())

	verifier := "test-code-verifier-with-sufficient-entropy-123456789"
	authorizationURL, err := service.AuthorizationURL(
		context.Background(),
		"http://subtrackr.test/api/auth/oidc/callback",
		"state-value",
		"nonce-value",
		verifier,
	)
	require.NoError(t, err)

	parsed, err := url.Parse(authorizationURL)
	require.NoError(t, err)
	assert.Equal(t, provider.URL+"/authorize", parsed.Scheme+"://"+parsed.Host+parsed.Path)
	query := parsed.Query()
	assert.Equal(t, "code", query.Get("response_type"))
	assert.Equal(t, "subtrackr-client", query.Get("client_id"))
	assert.Equal(t, "http://subtrackr.test/api/auth/oidc/callback", query.Get("redirect_uri"))
	assert.Equal(t, "state-value", query.Get("state"))
	assert.Equal(t, "nonce-value", query.Get("nonce"))
	assert.Equal(t, "S256", query.Get("code_challenge_method"))
	challenge := sha256.Sum256([]byte(verifier))
	assert.Equal(t, base64.RawURLEncoding.EncodeToString(challenge[:]), query.Get("code_challenge"))
	assert.Contains(t, strings.Fields(query.Get("scope")), "openid")
}

func TestOIDCService_ExchangeVerifiesIDTokenAndNonce(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
	)
	require.NoError(t, err)

	var provider *httptest.Server
	provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"issuer":                                provider.URL,
				"authorization_endpoint":                provider.URL + "/authorize",
				"token_endpoint":                        provider.URL + "/token",
				"jwks_uri":                              provider.URL + "/keys",
				"id_token_signing_alg_values_supported": []string{"RS256"},
			}))
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"keys": []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}},
			}))
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "authorization-code", r.Form.Get("code"))
			assert.Equal(t, "test-code-verifier", r.Form.Get("code_verifier"))
			claims, marshalErr := json.Marshal(map[string]interface{}{
				"iss":   provider.URL,
				"sub":   "user-123",
				"aud":   "subtrackr-client",
				"exp":   time.Now().Add(time.Hour).Unix(),
				"iat":   time.Now().Add(-time.Minute).Unix(),
				"nonce": "expected-nonce",
				"email": "user@example.com",
				"name":  "Example User",
			})
			require.NoError(t, marshalErr)
			signed, signErr := signer.Sign(claims)
			require.NoError(t, signErr)
			rawIDToken, serializeErr := signed.CompactSerialize()
			require.NoError(t, serializeErr)
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     rawIDToken,
			}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()

	settings := setupSettingsTestDB(t)
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		IssuerURL:    provider.URL,
		ClientID:     "subtrackr-client",
		ClientSecret: "client-secret",
	}))
	service := NewOIDCService(settings, provider.Client())

	_, err = service.ExchangeAndVerify(
		context.Background(),
		"http://subtrackr.test/api/auth/oidc/callback",
		"authorization-code",
		"test-code-verifier",
		"wrong-nonce",
	)
	require.ErrorContains(t, err, "invalid OIDC nonce")

	identity, err := service.ExchangeAndVerify(
		context.Background(),
		"http://subtrackr.test/api/auth/oidc/callback",
		"authorization-code",
		"test-code-verifier",
		"expected-nonce",
	)
	require.NoError(t, err)
	assert.Equal(t, "user-123", identity.Subject)
	assert.Equal(t, "user@example.com", identity.Email)
	assert.Equal(t, "Example User", identity.Name)
}

func TestOIDCService_DiscoveryFailuresAreBoundedAndReported(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		timeout time.Duration
	}{
		{
			name: "provider error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			},
		},
		{
			name: "malformed discovery document",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"issuer":`))
			},
		},
		{
			name: "timeout",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			},
			timeout: 20 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := httptest.NewServer(tt.handler)
			defer provider.Close()
			settings := setupSettingsTestDB(t)
			require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
				Enabled: true, IssuerURL: provider.URL, ClientID: "client", ClientSecret: "secret",
			}))
			client := provider.Client()
			if tt.timeout > 0 {
				client.Timeout = tt.timeout
			}

			_, err := NewOIDCService(settings, client).AuthorizationURL(
				context.Background(), "http://subtrackr.test/auth/oidc/callback", "state", "nonce", "verifier",
			)
			require.ErrorContains(t, err, "discover OIDC provider")
		})
	}
}

func TestOIDCService_RejectsInvalidTokenResponses(t *testing.T) {
	tests := []struct {
		name            string
		mutateClaims    func(map[string]interface{})
		omitIDToken     bool
		tokenStatus     int
		wrongSigningKey bool
		errorContains   string
	}{
		{name: "token endpoint failure", tokenStatus: http.StatusBadGateway, errorContains: "exchange OIDC authorization code"},
		{name: "missing ID token", omitIDToken: true, errorContains: "did not include an ID token"},
		{name: "wrong issuer", mutateClaims: func(c map[string]interface{}) { c["iss"] = "https://wrong-issuer.example" }, errorContains: "verify OIDC ID token"},
		{name: "wrong audience", mutateClaims: func(c map[string]interface{}) { c["aud"] = "another-client" }, errorContains: "verify OIDC ID token"},
		{name: "expired token", mutateClaims: func(c map[string]interface{}) { c["exp"] = time.Now().Add(-time.Hour).Unix() }, errorContains: "verify OIDC ID token"},
		{name: "invalid signature", wrongSigningKey: true, errorContains: "verify OIDC ID token"},
		{name: "missing subject", mutateClaims: func(c map[string]interface{}) { delete(c, "sub") }, errorContains: "missing subject"},
		{name: "missing nonce", mutateClaims: func(c map[string]interface{}) { delete(c, "nonce") }, errorContains: "invalid OIDC nonce"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			advertisedKey, err := rsa.GenerateKey(rand.Reader, 2048)
			require.NoError(t, err)
			signingKey := advertisedKey
			if tt.wrongSigningKey {
				signingKey, err = rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
			}
			signer, err := jose.NewSigner(
				jose.SigningKey{Algorithm: jose.RS256, Key: signingKey},
				(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"),
			)
			require.NoError(t, err)

			var provider *httptest.Server
			provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"issuer": provider.URL, "authorization_endpoint": provider.URL + "/authorize",
						"token_endpoint": provider.URL + "/token", "jwks_uri": provider.URL + "/keys",
						"id_token_signing_alg_values_supported": []string{"RS256"},
					})
				case "/keys":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"keys": []jose.JSONWebKey{{Key: &advertisedKey.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}},
					})
				case "/token":
					if tt.tokenStatus != 0 {
						http.Error(w, "token failure", tt.tokenStatus)
						return
					}
					response := map[string]interface{}{"access_token": "access", "token_type": "Bearer", "expires_in": 3600}
					if !tt.omitIDToken {
						claims := map[string]interface{}{
							"iss": provider.URL, "sub": "user-123", "aud": "subtrackr-client",
							"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Add(-time.Minute).Unix(),
							"nonce": "expected-nonce",
						}
						if tt.mutateClaims != nil {
							tt.mutateClaims(claims)
						}
						rawClaims, marshalErr := json.Marshal(claims)
						require.NoError(t, marshalErr)
						signed, signErr := signer.Sign(rawClaims)
						require.NoError(t, signErr)
						response["id_token"], err = signed.CompactSerialize()
						require.NoError(t, err)
					}
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(response)
				default:
					http.NotFound(w, r)
				}
			}))
			defer provider.Close()

			settings := setupSettingsTestDB(t)
			require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
				Enabled: true, IssuerURL: provider.URL, ClientID: "subtrackr-client", ClientSecret: "secret",
			}))
			_, err = NewOIDCService(settings, provider.Client()).ExchangeAndVerify(
				context.Background(), "http://subtrackr.test/auth/oidc/callback",
				"authorization-code", "code-verifier", "expected-nonce",
			)
			require.ErrorContains(t, err, tt.errorContains)
		})
	}
}

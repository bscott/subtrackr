package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOIDCFlow_RoundTripsOnce(t *testing.T) {
	service := NewSessionService("test-session-secret")
	startRequest := httptest.NewRequest("GET", "/api/auth/oidc/login", nil)
	startResponse := httptest.NewRecorder()
	flow := &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
		Redirect:     "/settings",
	}

	require.NoError(t, service.SaveOIDCFlow(startResponse, startRequest, "state-value", flow, false))
	require.NotEmpty(t, startResponse.Result().Cookies())

	callbackRequest := httptest.NewRequest("GET", "/api/auth/oidc/callback?state=state-value", nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()

	consumed, err := service.ConsumeOIDCFlow(callbackResponse, callbackRequest, "state-value", false)
	require.NoError(t, err)
	assert.Equal(t, flow, consumed)

	replayRequest := httptest.NewRequest("GET", "/api/auth/oidc/callback?state=state-value", nil)
	require.Len(t, callbackResponse.Result().Cookies(), 1)
	clearedCookie := callbackResponse.Result().Cookies()[0]
	require.Less(t, clearedCookie.MaxAge, 0)
	require.Empty(t, clearedCookie.Value)
	replayRequest.AddCookie(clearedCookie)
	_, err = service.ConsumeOIDCFlow(httptest.NewRecorder(), replayRequest, "state-value", false)
	assert.Error(t, err)
}

func TestCreateSession_UsesLaxCookieForOIDCRedirectCompatibility(t *testing.T) {
	service := NewSessionService("test-session-secret")
	request := httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil)
	response := httptest.NewRecorder()

	require.NoError(t, service.CreateSession(response, request, false, true))
	require.Len(t, response.Result().Cookies(), 1)
	cookie := response.Result().Cookies()[0]
	assert.Equal(t, SessionName, cookie.Name)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.True(t, cookie.Secure)
}

func TestOIDCFlow_RejectsMismatchedState(t *testing.T) {
	service := NewSessionService("test-session-secret")
	startRequest := httptest.NewRequest("GET", "/api/auth/oidc/login", nil)
	startResponse := httptest.NewRecorder()
	require.NoError(t, service.SaveOIDCFlow(startResponse, startRequest, "expected-state", &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
		Redirect:     "/",
	}, false))

	callbackRequest := httptest.NewRequest("GET", "/api/auth/oidc/callback?state=attacker-state", nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])

	_, err := service.ConsumeOIDCFlow(httptest.NewRecorder(), callbackRequest, "attacker-state", false)
	assert.Error(t, err)
}

func TestOIDCFlow_UsesDedicatedShortLivedEncryptedLaxCookie(t *testing.T) {
	service := NewSessionService("test-session-secret")
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
	response := httptest.NewRecorder()

	require.NoError(t, service.SaveOIDCFlow(response, request, "state-value", &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
		Redirect:     "/settings",
	}, true))

	require.Len(t, response.Result().Cookies(), 1)
	cookie := response.Result().Cookies()[0]
	assert.Equal(t, OIDCSessionName, cookie.Name)
	assert.Equal(t, OIDCCallbackPath, cookie.Path)
	assert.Equal(t, OIDCFlowMaxAge, cookie.MaxAge)
	assert.True(t, cookie.HttpOnly)
	assert.True(t, cookie.Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.NotContains(t, cookie.Value, "state-value")
	assert.NotContains(t, cookie.Value, "nonce-value")
	assert.NotContains(t, cookie.Value, "verifier-value")

	// A store with only the authentication key cannot decode the transaction;
	// the separate encryption key is required.
	authOnlyStore := sessions.NewCookieStore(oidcKey("test-session-secret", "authentication"))
	decodeRequest := httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil)
	decodeRequest.AddCookie(cookie)
	_, err := authOnlyStore.Get(decodeRequest, OIDCSessionName)
	assert.Error(t, err)
}

func TestOIDCFlow_CookieSecureFlagCanBeDisabledForHTTPDevelopment(t *testing.T) {
	service := NewSessionService("test-session-secret")
	response := httptest.NewRecorder()

	require.NoError(t, service.SaveOIDCFlow(response, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil), "state-value", &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
	}, false))

	require.Len(t, response.Result().Cookies(), 1)
	assert.False(t, response.Result().Cookies()[0].Secure)
}

func TestOIDCFlow_ExpiresAndClearsTransactionCookie(t *testing.T) {
	service := NewSessionService("test-session-secret")
	startedAt := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return startedAt }
	startResponse := httptest.NewRecorder()
	require.NoError(t, service.SaveOIDCFlow(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil), "state-value", &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
	}, true))

	service.now = func() time.Time { return startedAt.Add(time.Duration(OIDCFlowMaxAge+1) * time.Second) }
	callbackRequest := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?state=state-value", nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()

	_, err := service.ConsumeOIDCFlow(callbackResponse, callbackRequest, "state-value", true)
	assert.ErrorContains(t, err, "expired")
	require.Len(t, callbackResponse.Result().Cookies(), 1)
	assert.Equal(t, OIDCSessionName, callbackResponse.Result().Cookies()[0].Name)
	assert.Less(t, callbackResponse.Result().Cookies()[0].MaxAge, 0)
}

func TestOIDCFlow_MismatchedStateClearsTransactionCookie(t *testing.T) {
	service := NewSessionService("test-session-secret")
	startResponse := httptest.NewRecorder()
	require.NoError(t, service.SaveOIDCFlow(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil), "expected-state", &OIDCFlow{
		Nonce:        "nonce-value",
		CodeVerifier: "verifier-value",
	}, true))

	callbackRequest := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?state=attacker-state", nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()
	_, err := service.ConsumeOIDCFlow(callbackResponse, callbackRequest, "attacker-state", true)

	assert.Error(t, err)
	require.Len(t, callbackResponse.Result().Cookies(), 1)
	assert.Less(t, callbackResponse.Result().Cookies()[0].MaxAge, 0)
}

func TestOIDCFlow_RejectsMissingCorruptAndIncompleteTransactions(t *testing.T) {
	tests := []struct {
		name   string
		cookie *http.Cookie
	}{
		{name: "missing cookie"},
		{name: "corrupt cookie", cookie: &http.Cookie{Name: OIDCSessionName, Value: "not-a-valid-secure-cookie", Path: OIDCCallbackPath}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewSessionService("test-session-secret")
			request := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?state=state-value", nil)
			if tt.cookie != nil {
				request.AddCookie(tt.cookie)
			}
			response := httptest.NewRecorder()

			_, err := service.ConsumeOIDCFlow(response, request, "state-value", false)
			assert.Error(t, err)
			require.Len(t, response.Result().Cookies(), 1)
			assert.Equal(t, OIDCSessionName, response.Result().Cookies()[0].Name)
			assert.Less(t, response.Result().Cookies()[0].MaxAge, 0)
		})
	}

	t.Run("incomplete transaction", func(t *testing.T) {
		service := NewSessionService("test-session-secret")
		startRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
		startResponse := httptest.NewRecorder()
		session, err := service.oidcStore.Get(startRequest, OIDCSessionName)
		require.NoError(t, err)
		session.Values[oidcStateKey] = "state-value"
		session.Values[oidcIssuedAtKey] = service.now().Unix()
		session.Options = oidcCookieOptions(false, OIDCFlowMaxAge)
		require.NoError(t, session.Save(startRequest, startResponse))

		request := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?state=state-value", nil)
		request.AddCookie(startResponse.Result().Cookies()[0])
		response := httptest.NewRecorder()
		_, err = service.ConsumeOIDCFlow(response, request, "state-value", false)
		assert.ErrorContains(t, err, "incomplete")
		assert.Less(t, response.Result().Cookies()[0].MaxAge, 0)
	})
}

func TestOIDCFlow_RejectsMissingStateAndFutureTimestamp(t *testing.T) {
	service := NewSessionService("test-session-secret")
	startedAt := time.Unix(1_700_000_000, 0)
	service.now = func() time.Time { return startedAt }
	startResponse := httptest.NewRecorder()
	require.NoError(t, service.SaveOIDCFlow(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil), "state-value", &OIDCFlow{
		Nonce: "nonce-value", CodeVerifier: "verifier-value",
	}, false))

	missingStateRequest := httptest.NewRequest(http.MethodGet, OIDCCallbackPath, nil)
	missingStateRequest.AddCookie(startResponse.Result().Cookies()[0])
	_, err := service.ConsumeOIDCFlow(httptest.NewRecorder(), missingStateRequest, "", false)
	assert.ErrorContains(t, err, "invalid OIDC state")

	service = NewSessionService("test-session-secret")
	service.now = func() time.Time { return startedAt.Add(31 * time.Second) }
	futureResponse := httptest.NewRecorder()
	require.NoError(t, service.SaveOIDCFlow(futureResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil), "state-value", &OIDCFlow{
		Nonce: "nonce-value", CodeVerifier: "verifier-value",
	}, false))
	service.now = func() time.Time { return startedAt }
	futureRequest := httptest.NewRequest(http.MethodGet, OIDCCallbackPath+"?state=state-value", nil)
	futureRequest.AddCookie(futureResponse.Result().Cookies()[0])
	_, err = service.ConsumeOIDCFlow(httptest.NewRecorder(), futureRequest, "state-value", false)
	assert.ErrorContains(t, err, "expired")
}

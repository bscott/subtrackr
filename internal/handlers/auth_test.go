package handlers

import (
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"subtrackr/internal/i18n"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeOIDCAuthenticator struct {
	authorizationURL string
	redirectURI      string
	state            string
	nonce            string
	codeVerifier     string
	identity         *service.OIDCIdentity
	exchangeErr      error
	exchangeCalls    int
}

func (f *fakeOIDCAuthenticator) AuthorizationURL(_ context.Context, redirectURI, state, nonce, codeVerifier string) (string, error) {
	f.redirectURI = redirectURI
	f.state = state
	f.nonce = nonce
	f.codeVerifier = codeVerifier
	return f.authorizationURL, nil
}

func (f *fakeOIDCAuthenticator) ExchangeAndVerify(_ context.Context, redirectURI, _, codeVerifier, nonce string) (*service.OIDCIdentity, error) {
	f.exchangeCalls++
	f.redirectURI = redirectURI
	f.codeVerifier = codeVerifier
	f.nonce = nonce
	return f.identity, f.exchangeErr
}

// repoRoot walks up from the test working directory to locate the project root
// (the directory containing go.mod). Tests run with cwd == package dir, so the
// templates and locales need an absolute path.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate go.mod from %s", dir)
		}
		dir = parent
	}
}

func newAuthTestRouter(t *testing.T) (*gin.Engine, *AuthHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	root := repoRoot(t)

	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(root, "web", "locales")))

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))

	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	sessionService := service.NewSessionService("test-secret-key-for-auth-handler-test")

	authHandler := NewAuthHandler(settingsService, sessionService, nil, catalog)

	router := gin.New()
	tFunc := func(lang interface{}, key string) string {
		langStr, _ := lang.(string)
		return catalog.T(langStr, key)
	}
	router.SetFuncMap(template.FuncMap{"t": tFunc})
	// Load only the auth-related templates to avoid pulling in templates that
	// reference other helpers (div/mul/etc.) we don't need here.
	router.LoadHTMLFiles(
		filepath.Join(root, "templates", "login.html"),
		filepath.Join(root, "templates", "login-error.html"),
		filepath.Join(root, "templates", "forgot-password.html"),
		filepath.Join(root, "templates", "reset-password.html"),
	)

	router.GET("/login", authHandler.ShowLoginPage)
	router.POST("/api/auth/login", authHandler.Login)
	router.GET("/forgot-password", authHandler.ShowForgotPasswordPage)
	router.GET("/reset-password", authHandler.ShowResetPasswordPage)

	return router, authHandler
}

func TestLogin_HTTPSBaseURLSetsSecureApplicationSessionCookie(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetupAuth("admin", "password"))
	require.NoError(t, authHandler.settingsService.SetBaseURL("https://subtrackr.example.com"))
	form := url.Values{"username": {"admin"}, "password": {"password"}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	var sessionCookie *http.Cookie
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == service.SessionName {
			sessionCookie = cookie
		}
	}
	require.NotNil(t, sessionCookie)
	assert.True(t, sessionCookie.Secure)
}

func TestOIDCCallback_HTTPSBaseURLSetsSecureApplicationSessionCookie(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBaseURL("https://subtrackr.example.com"))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "subtrackr", ClientSecret: "client-secret",
	}))
	fakeOIDC := &fakeOIDCAuthenticator{
		authorizationURL: "https://id.example.com/authorize",
		identity:         &service.OIDCIdentity{Subject: "user-123"},
	}
	authHandler.oidcService = fakeOIDC
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)
	router.GET("/auth/oidc/callback", authHandler.OIDCCallback)

	startResponse := httptest.NewRecorder()
	router.ServeHTTP(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil))
	require.Len(t, startResponse.Result().Cookies(), 1)
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=code&state="+url.QueryEscape(fakeOIDC.state), nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()
	router.ServeHTTP(callbackResponse, callbackRequest)

	assert.Equal(t, http.StatusFound, callbackResponse.Code)
	var sessionCookie *http.Cookie
	for _, cookie := range callbackResponse.Result().Cookies() {
		if cookie.Name == service.SessionName {
			sessionCookie = cookie
		}
	}
	require.NotNil(t, sessionCookie)
	assert.True(t, sessionCookie.Secure)
}

func TestLogin_RejectsRetainedCredentialsWhenLocalAuthIsDisabled(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetupAuth("admin", "retained-password"))
	require.NoError(t, authHandler.settingsService.SetAuthEnabled(false))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "subtrackr", ClientSecret: "client-secret",
	}))
	form := url.Values{
		"username": {"admin"},
		"password": {"retained-password"},
		"redirect": {"/settings"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusForbidden, response.Code)
	assert.Empty(t, response.Result().Cookies())
}

// TestShowLoginPage_RendersWithoutLangError reproduces issue #113 — before the
// fix, the login template's {{t .Lang "..."}} crashed because the handler did
// not pass Lang in the template context.
func TestShowLoginPage_RendersWithoutLangError(t *testing.T) {
	router, _ := newAuthTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "template:")
	assert.NotContains(t, body, "invalid value")
	// English fallback content from the auth.sign_in_title key.
	assert.Contains(t, body, "Sign")
}

func TestShowLoginPage_OffersOIDCWithoutLocalPasswordForm(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		DisplayName:  "Pocket ID",
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "client-secret",
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login?redirect=%2Fsettings", nil)
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Continue with Pocket ID")
	assert.Contains(t, response.Body.String(), "/auth/oidc/login?redirect=%2Fsettings")
	assert.NotContains(t, response.Body.String(), `name="password"`)
}

func TestShowLoginPage_OffersLocalAndOIDCWhenBothAreEnabled(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBoolSetting("auth_enabled", true))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		DisplayName:  "Authentik",
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "client-secret",
	}))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "Continue with Authentik")
	assert.Contains(t, response.Body.String(), `name="password"`)
}

func TestShowForgotPasswordPage_RendersWithoutLangError(t *testing.T) {
	router, _ := newAuthTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/forgot-password", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "invalid value")
}

func TestShowResetPasswordPage_MissingTokenRendersWithoutLangError(t *testing.T) {
	router, _ := newAuthTestRouter(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	router.ServeHTTP(w, req)

	// Token missing → 400, but template must still render without a Lang error.
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.NotContains(t, w.Body.String(), "invalid value")
}

// TestTFunc_HandlesNonStringLang locks in the defensive coercion in the
// template's t function so missing/nil .Lang values fall back to English
// instead of crashing the render.
func TestTFunc_HandlesNonStringLang(t *testing.T) {
	root := repoRoot(t)
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(root, "web", "locales")))

	tFunc := func(lang interface{}, key string) string {
		langStr, _ := lang.(string)
		return catalog.T(langStr, key)
	}

	tmpl := template.Must(template.New("t").Funcs(template.FuncMap{"t": tFunc}).Parse(`{{t .Lang "auth.sign_in_title"}}`))

	cases := []struct {
		name string
		data map[string]interface{}
	}{
		{name: "missing Lang", data: map[string]interface{}{}},
		{name: "nil Lang", data: map[string]interface{}{"Lang": nil}},
		{name: "empty Lang", data: map[string]interface{}{"Lang": ""}},
		{name: "english Lang", data: map[string]interface{}{"Lang": "en"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			err := tmpl.Execute(&sb, tc.data)
			assert.NoError(t, err)
			assert.NotEmpty(t, sb.String())
		})
	}
}

func TestIsValidRedirectOnlyAcceptsLocalRequestURIs(t *testing.T) {
	tests := []struct {
		name     string
		redirect string
		valid    bool
	}{
		{name: "path", redirect: "/subscriptions", valid: true},
		{name: "path with query", redirect: "/subscriptions?sort=name", valid: true},
		{name: "absolute URL", redirect: "https://evil.example/path", valid: false},
		{name: "scheme relative URL", redirect: "//evil.example/path", valid: false},
		{name: "backslash", redirect: `/\\evil.example`, valid: false},
		{name: "fragment", redirect: "/settings#secret", valid: false},
		{name: "control character", redirect: "/settings\r\nX-Injected: true", valid: false},
		{name: "missing leading slash", redirect: "settings", valid: false},
		{name: "oversized", redirect: "/" + strings.Repeat("x", 2048), valid: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidRedirect(tt.redirect))
		})
	}
}

func TestOIDCLogin_StartsFlowWithStateNonceAndPKCE(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBaseURL("http://subtrackr:8080"))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "client-secret",
	}))
	fakeOIDC := &fakeOIDCAuthenticator{authorizationURL: "https://id.example.com/authorize"}
	authHandler.oidcService = fakeOIDC
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/login?redirect=%2Fsettings", nil)
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, fakeOIDC.authorizationURL, response.Header().Get("Location"))
	assert.NotEmpty(t, fakeOIDC.state)
	assert.NotEmpty(t, fakeOIDC.nonce)
	assert.NotEmpty(t, fakeOIDC.codeVerifier)
	assert.Equal(t, "http://subtrackr:8080/auth/oidc/callback", fakeOIDC.redirectURI)
	assert.NotEmpty(t, response.Result().Cookies())
}

func TestOIDCLogin_UsesTrustedHTTPSBaseURLForRedirectAndSecureTransactionCookie(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBaseURL("https://subtrackr.example.com"))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "subtrackr", ClientSecret: "client-secret",
	}))
	fakeOIDC := &fakeOIDCAuthenticator{authorizationURL: "https://id.example.com/authorize"}
	authHandler.oidcService = fakeOIDC
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil)
	request.Header.Set("X-Forwarded-Host", "attacker.example")
	request.Header.Set("X-Forwarded-Proto", "http")
	router.ServeHTTP(response, request)

	assert.Equal(t, "https://subtrackr.example.com/auth/oidc/callback", fakeOIDC.redirectURI)
	require.Len(t, response.Result().Cookies(), 1)
	cookie := response.Result().Cookies()[0]
	assert.Equal(t, service.OIDCSessionName, cookie.Name)
	assert.Equal(t, service.OIDCCallbackPath, cookie.Path)
	assert.Equal(t, service.OIDCFlowMaxAge, cookie.MaxAge)
	assert.True(t, cookie.Secure)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
}

func TestOIDCCallback_ProviderErrorClearsTransactionCookie(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBaseURL("https://subtrackr.example.com"))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "subtrackr", ClientSecret: "client-secret",
	}))
	fakeOIDC := &fakeOIDCAuthenticator{authorizationURL: "https://id.example.com/authorize"}
	authHandler.oidcService = fakeOIDC
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)
	router.GET("/auth/oidc/callback", authHandler.OIDCCallback)

	startResponse := httptest.NewRecorder()
	router.ServeHTTP(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil))
	require.Len(t, startResponse.Result().Cookies(), 1)

	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?error=access_denied&state="+url.QueryEscape(fakeOIDC.state), nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	callbackResponse := httptest.NewRecorder()
	router.ServeHTTP(callbackResponse, callbackRequest)

	assert.Equal(t, http.StatusFound, callbackResponse.Code)
	assert.Contains(t, callbackResponse.Header().Get("Location"), "/login?error=")
	assert.Equal(t, 0, fakeOIDC.exchangeCalls)
	require.Len(t, callbackResponse.Result().Cookies(), 1)
	clearedCookie := callbackResponse.Result().Cookies()[0]
	assert.Equal(t, service.OIDCSessionName, clearedCookie.Name)
	assert.Less(t, clearedCookie.MaxAge, 0)
	assert.Empty(t, clearedCookie.Value)
	assert.True(t, clearedCookie.Secure)
}

func TestOIDCCallback_ExchangeFailureAndMissingStateClearTransactionCookie(t *testing.T) {
	t.Run("exchange failure", func(t *testing.T) {
		router, authHandler := newAuthTestRouter(t)
		require.NoError(t, authHandler.settingsService.SetBaseURL("http://subtrackr:8080"))
		require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
			Enabled: true, IssuerURL: "https://id.example.com", ClientID: "subtrackr", ClientSecret: "client-secret",
		}))
		fakeOIDC := &fakeOIDCAuthenticator{authorizationURL: "https://id.example.com/authorize", exchangeErr: assert.AnError}
		authHandler.oidcService = fakeOIDC
		router.GET("/auth/oidc/login", authHandler.OIDCLogin)
		router.GET("/auth/oidc/callback", authHandler.OIDCCallback)

		startResponse := httptest.NewRecorder()
		router.ServeHTTP(startResponse, httptest.NewRequest(http.MethodGet, "/auth/oidc/login", nil))
		callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=bad-code&state="+url.QueryEscape(fakeOIDC.state), nil)
		callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
		callbackResponse := httptest.NewRecorder()
		router.ServeHTTP(callbackResponse, callbackRequest)

		assert.Equal(t, 1, fakeOIDC.exchangeCalls)
		assert.Contains(t, callbackResponse.Header().Get("Location"), "/login?error=")
		require.Len(t, callbackResponse.Result().Cookies(), 1)
		assert.Less(t, callbackResponse.Result().Cookies()[0].MaxAge, 0)
	})

	t.Run("missing state and cookie", func(t *testing.T) {
		router, authHandler := newAuthTestRouter(t)
		require.NoError(t, authHandler.settingsService.SetBaseURL("http://subtrackr:8080"))
		router.GET("/auth/oidc/callback", authHandler.OIDCCallback)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=code", nil))

		assert.Contains(t, response.Header().Get("Location"), "/login?error=")
		require.Len(t, response.Result().Cookies(), 1)
		assert.Equal(t, service.OIDCSessionName, response.Result().Cookies()[0].Name)
		assert.Less(t, response.Result().Cookies()[0].MaxAge, 0)
	})
}

func TestOIDCCallback_CreatesSessionAndCannotBeReplayed(t *testing.T) {
	router, authHandler := newAuthTestRouter(t)
	require.NoError(t, authHandler.settingsService.SetBaseURL("http://subtrackr:8080"))
	require.NoError(t, authHandler.settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "client-secret",
	}))
	fakeOIDC := &fakeOIDCAuthenticator{
		authorizationURL: "https://id.example.com/authorize",
		identity:         &service.OIDCIdentity{Subject: "user-123"},
	}
	authHandler.oidcService = fakeOIDC
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)
	router.GET("/auth/oidc/callback", authHandler.OIDCCallback)

	startResponse := httptest.NewRecorder()
	startRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/login?redirect=%2Fsettings", nil)
	router.ServeHTTP(startResponse, startRequest)
	require.Equal(t, http.StatusFound, startResponse.Code)

	callbackResponse := httptest.NewRecorder()
	callbackRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=authorization-code&state="+url.QueryEscape(fakeOIDC.state), nil)
	callbackRequest.AddCookie(startResponse.Result().Cookies()[0])
	router.ServeHTTP(callbackResponse, callbackRequest)

	assert.Equal(t, http.StatusFound, callbackResponse.Code)
	assert.Equal(t, "/settings", callbackResponse.Header().Get("Location"))
	require.NotEmpty(t, callbackResponse.Result().Cookies())
	finalCookie := callbackResponse.Result().Cookies()[len(callbackResponse.Result().Cookies())-1]
	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	authenticatedRequest.AddCookie(finalCookie)
	assert.True(t, authHandler.sessionService.IsAuthenticated(authenticatedRequest))
	assert.Equal(t, 1, fakeOIDC.exchangeCalls)

	replayResponse := httptest.NewRecorder()
	replayRequest := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?code=authorization-code&state="+url.QueryEscape(fakeOIDC.state), nil)
	replayRequest.AddCookie(finalCookie)
	router.ServeHTTP(replayResponse, replayRequest)

	assert.Equal(t, http.StatusFound, replayResponse.Code)
	assert.Contains(t, replayResponse.Header().Get("Location"), "/login?error=")
	assert.Equal(t, 1, fakeOIDC.exchangeCalls)
}

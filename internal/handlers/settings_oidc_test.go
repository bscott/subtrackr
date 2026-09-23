package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func newOIDCSettingsTestRouter(t *testing.T) (*gin.Engine, *service.SettingsService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(repoRoot(t), "web", "locales")))
	handler := NewSettingsHandler(settingsService, catalog)

	router := gin.New()
	router.SetFuncMap(template.FuncMap{"t": func(_ interface{}, key string) string { return key }})
	router.LoadHTMLFiles(filepath.Join(repoRoot(t), "templates", "auth-message.html"))
	router.POST("/api/settings/auth/oidc", handler.SaveOIDCSettings)
	router.POST("/api/settings/base-url", handler.UpdateBaseURL)
	return router, settingsService
}

func TestUpdateBaseURL_AllowsInitialSetupWithoutOIDCConfig(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	form := url.Values{"base_url": {"http://subtrackr.test:8080"}}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/base-url", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "http://subtrackr.test:8080", settings.GetBaseURL())
}

func TestUpdateBaseURL_RejectsHTTPSUpgradeWithHTTPProvider(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	require.NoError(t, settings.SetBaseURL("http://subtrackr.test:8080"))
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "http://pocket-id.test:1411", ClientID: "client", ClientSecret: "secret",
	}))
	form := url.Values{"base_url": {"https://subtrackr.example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/base-url", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "http://subtrackr.test:8080", settings.GetBaseURL())
}

func TestUpdateBaseURL_RejectsInvalidValueWhileOIDCIsEnabled(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	require.NoError(t, settings.SetBaseURL("https://subtrackr.example.com"))
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "client", ClientSecret: "secret",
	}))
	form := url.Values{"base_url": {"not-a-url"}}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/base-url", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "https://subtrackr.example.com", settings.GetBaseURL())
}

func TestSaveOIDCSettings_PersistsEnabledProvider(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	require.NoError(t, settings.SetBaseURL("https://subtrackr.example.com"))
	form := url.Values{
		"enabled":       {"true"},
		"display_name":  {"Pocket ID"},
		"issuer_url":    {"https://id.example.com/"},
		"client_id":     {"subtrackr"},
		"client_secret": {"client-secret"},
		"scopes":        {"profile  openid email profile groups"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/auth/oidc", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	config, err := settings.GetOIDCConfig()
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.Equal(t, "Pocket ID", config.DisplayName)
	assert.Equal(t, "https://id.example.com", config.IssuerURL)
	assert.Equal(t, "subtrackr", config.ClientID)
	assert.Equal(t, "client-secret", config.ClientSecret)
	assert.Equal(t, []string{"openid", "profile", "email", "groups"}, config.Scopes)
	assert.NotContains(t, response.Body.String(), "client-secret")
}

func TestSaveOIDCSettings_RejectsEnableWithoutBaseURL(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	form := url.Values{
		"enabled":       {"true"},
		"display_name":  {"Pocket ID"},
		"issuer_url":    {"https://id.example.com"},
		"client_id":     {"subtrackr"},
		"client_secret": {"super-secret"},
	}
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/settings/auth/oidc", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.False(t, settings.IsOIDCEnabled())
}

func TestSaveOIDCSettings_PreservesReplacesAndDisablesClientSecret(t *testing.T) {
	router, settings := newOIDCSettingsTestRouter(t)
	require.NoError(t, settings.SetBaseURL("https://subtrackr.example.com"))
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, DisplayName: "Old Provider", IssuerURL: "https://old.example.com",
		ClientID: "old-client", ClientSecret: "existing-secret",
	}))

	submit := func(form url.Values) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/api/settings/auth/oidc", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	response := submit(url.Values{
		"enabled": {"true"}, "display_name": {"New Provider"}, "issuer_url": {"https://new.example.com"},
		"client_id": {"new-client"}, "client_secret": {""},
	})
	require.Equal(t, http.StatusOK, response.Code)
	config, err := settings.GetOIDCConfig()
	require.NoError(t, err)
	assert.Equal(t, "existing-secret", config.ClientSecret)
	assert.Equal(t, "New Provider", config.DisplayName)

	response = submit(url.Values{
		"enabled": {"true"}, "display_name": {"New Provider"}, "issuer_url": {"https://new.example.com"},
		"client_id": {"new-client"}, "client_secret": {"replacement-secret"},
	})
	require.Equal(t, http.StatusOK, response.Code)
	config, err = settings.GetOIDCConfig()
	require.NoError(t, err)
	assert.Equal(t, "replacement-secret", config.ClientSecret)

	response = submit(url.Values{
		"display_name": {"New Provider"}, "issuer_url": {"https://new.example.com"},
		"client_id": {"new-client"}, "client_secret": {""},
	})
	require.Equal(t, http.StatusOK, response.Code)
	config, err = settings.GetOIDCConfig()
	require.NoError(t, err)
	assert.False(t, config.Enabled)
	assert.Equal(t, "replacement-secret", config.ClientSecret)
}

func TestSaveOIDCSettings_RejectsInvalidEnabledConfigurationWithoutOverwritingExisting(t *testing.T) {
	tests := []struct {
		name     string
		issuer   string
		clientID string
		secret   string
	}{
		{name: "relative issuer", issuer: "/oidc", clientID: "client", secret: "secret"},
		{name: "unsupported issuer scheme", issuer: "ftp://id.example.com", clientID: "client", secret: "secret"},
		{name: "cleartext issuer for HTTPS application", issuer: "http://id.example.com", clientID: "client", secret: "secret"},
		{name: "issuer userinfo", issuer: "https://user@id.example.com", clientID: "client", secret: "secret"},
		{name: "issuer query", issuer: "https://id.example.com?tenant=one", clientID: "client", secret: "secret"},
		{name: "issuer fragment", issuer: "https://id.example.com/#fragment", clientID: "client", secret: "secret"},
		{name: "missing client ID", issuer: "https://id.example.com", secret: "secret"},
		{name: "missing first client secret", issuer: "https://id.example.com", clientID: "client"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, settings := newOIDCSettingsTestRouter(t)
			require.NoError(t, settings.SetBaseURL("https://subtrackr.example.com"))
			form := url.Values{
				"enabled": {"true"}, "display_name": {"Provider"}, "issuer_url": {tt.issuer},
				"client_id": {tt.clientID}, "client_secret": {tt.secret},
			}
			request := httptest.NewRequest(http.MethodPost, "/api/settings/auth/oidc", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			assert.Equal(t, http.StatusBadRequest, response.Code)
			assert.False(t, settings.IsOIDCEnabled())
		})
	}
}

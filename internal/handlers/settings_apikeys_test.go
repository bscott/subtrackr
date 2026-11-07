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
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newAPIKeysTestRouter(t *testing.T) (*gin.Engine, *service.SettingsService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}, &models.APIKey{}))
	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	handler := NewSettingsHandler(settingsService, i18n.NewCatalog())

	router := gin.New()
	router.SetFuncMap(template.FuncMap{
		"t":       func(_ interface{}, key string) string { return key },
		"fmtTime": func(t time.Time, format string) string { return t.Format(format) },
	})
	router.LoadHTMLFiles(filepath.Join(repoRoot(t), "templates", "api-keys-list.html"))
	router.POST("/api/settings/apikeys", handler.CreateAPIKey)
	router.DELETE("/api/settings/apikeys/:id", handler.DeleteAPIKey)
	return router, settingsService
}

func createAPIKeyRequest(router *gin.Engine, name string) *httptest.ResponseRecorder {
	form := url.Values{"name": {name}}
	req := httptest.NewRequest(http.MethodPost, "/api/settings/apikeys", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// HTMX 4 swaps 4xx/5xx responses into #api-keys-list, so an error response
// must still list the existing keys and show why the request failed.
func TestCreateAPIKey_ErrorKeepsExistingKeys(t *testing.T) {
	router, _ := newAPIKeysTestRouter(t)
	require.Equal(t, http.StatusOK, createAPIKeyRequest(router, "Budget script").Code)

	for _, name := range []string{"", "   "} {
		rec := createAPIKeyRequest(router, name)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		body := rec.Body.String()
		assert.Contains(t, body, "API key name is required")
		assert.Contains(t, body, "Budget script")
		assert.NotContains(t, body, "settings.no_api_keys")
		assert.NotContains(t, body, "sk_", "an error response must not reveal key values")
	}
}

func TestDeleteAPIKey_InvalidIDKeepsExistingKeys(t *testing.T) {
	router, _ := newAPIKeysTestRouter(t)
	require.Equal(t, http.StatusOK, createAPIKeyRequest(router, "Budget script").Code)

	req := httptest.NewRequest(http.MethodDelete, "/api/settings/apikeys/not-a-number", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "Invalid API key ID")
	assert.Contains(t, rec.Body.String(), "Budget script")
}

func TestCreateAPIKey_TrimsName(t *testing.T) {
	router, settingsService := newAPIKeysTestRouter(t)
	require.Equal(t, http.StatusOK, createAPIKeyRequest(router, "  Budget script  ").Code)

	keys, err := settingsService.GetAllAPIKeys()
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "Budget script", keys[0].Name)
}

package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

func TestSettings_PassesOIDCConfigWithoutClientSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settings := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		DisplayName:  "Pocket ID",
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "must-not-render",
	}))
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(repoRoot(t), "web", "locales")))
	handler := NewSubscriptionHandler(nil, settings, nil, nil, nil, nil, nil, nil, nil, nil, catalog)

	router := gin.New()
	router.SetHTMLTemplate(template.Must(template.New("settings.html").Parse(
		`{{.OIDCConfig.DisplayName}}|{{.OIDCConfig.IssuerURL}}|{{.OIDCConfig.ClientID}}|{{.OIDCConfigured}}|{{.OIDCConfig.ClientSecret}}`,
	)))
	router.GET("/settings", handler.Settings)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/settings", nil)

	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, "Pocket ID|https://id.example.com|subtrackr|true|", response.Body.String())
	assert.NotContains(t, response.Body.String(), "must-not-render")
}

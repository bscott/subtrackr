package middleware

import (
	"net/http"
	"net/http/httptest"
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

func TestAuthMiddleware_RequiresLoginWhenOnlyOIDCIsEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settingsService.SaveOIDCConfig(&models.OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://id.example.com",
		ClientID:     "subtrackr",
		ClientSecret: "client-secret",
	}))

	router := gin.New()
	router.Use(AuthMiddleware(settingsService, service.NewSessionService("test-session-secret")))
	router.GET("/", func(c *gin.Context) { c.Status(http.StatusOK) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Accept", "text/html")
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "/login?redirect=%2F", response.Header().Get("Location"))
}

func TestOIDCRoutesArePublicDuringLoginFlow(t *testing.T) {
	assert.True(t, isPublicRoute("/auth/oidc/login"))
	assert.True(t, isPublicRoute("/auth/oidc/callback"))
}

func TestPublicRoutesDoNotUseBroadPrefixMatching(t *testing.T) {
	assert.True(t, isPublicRoute("/login"))
	assert.True(t, isPublicRoute("/static/app.js"))
	assert.False(t, isPublicRoute("/login-anything"))
	assert.False(t, isPublicRoute("/healthz-extra"))
}

func TestAuthMiddleware_PreservesRequestQueryInLoginRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settings := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settings.SaveOIDCConfig(&models.OIDCConfig{
		Enabled: true, IssuerURL: "https://id.example.com", ClientID: "client", ClientSecret: "secret",
	}))
	sessions := service.NewSessionService("test-session-secret-that-is-long-enough")
	router := gin.New()
	router.Use(AuthMiddleware(settings, sessions))
	router.GET("/settings", func(c *gin.Context) { c.Status(http.StatusOK) })

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/settings?tab=oidc", nil)
	request.Header.Set("Accept", "text/html")
	router.ServeHTTP(response, request)

	assert.Equal(t, http.StatusFound, response.Code)
	assert.Equal(t, "/login?redirect=%2Fsettings%3Ftab%3Doidc", response.Header().Get("Location"))
}

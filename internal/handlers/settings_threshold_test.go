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

func TestUpdateNotificationSetting_Threshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	router := gin.New()
	router.POST("/api/settings/notifications/:setting", NewSettingsHandler(settingsService, i18n.NewCatalog()).UpdateNotificationSetting)

	cases := []struct {
		value  string
		status int
	}{
		{"500000", http.StatusOK}, // e.g. VND, above the old 10000 cap
		{"0", http.StatusOK},
		{"-1", http.StatusBadRequest},
		{"+Inf", http.StatusBadRequest},
		{"NaN", http.StatusBadRequest},
		{"abc", http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.value, func(t *testing.T) {
			form := url.Values{"high_cost_threshold": {c.value}}
			req := httptest.NewRequest(http.MethodPost, "/api/settings/notifications/threshold", strings.NewReader(form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.Equal(t, c.status, rec.Code, rec.Body.String())
		})
	}

	assert.Equal(t, 0.0, settingsService.GetFloatSettingWithDefault("high_cost_threshold", 50))
}

func TestSaveNotificationChannel_BlankTokenKeepsStoredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	handler := NewSettingsHandler(settingsService, i18n.NewCatalog())
	router := gin.New()
	router.SetFuncMap(template.FuncMap{})
	router.LoadHTMLFiles(filepath.Join(repoRoot(t), "templates", "smtp-message.html"))
	router.POST("/api/settings/pushover", handler.SavePushoverSettings)
	router.POST("/api/settings/telegram", handler.SaveTelegramSettings)

	post := func(path string, form url.Values) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	t.Run("pushover", func(t *testing.T) {
		// First-time setup still requires the token.
		assert.Equal(t, http.StatusBadRequest, post("/api/settings/pushover", url.Values{"pushover_user_key": {"user-1"}, "pushover_app_token": {""}}))
		_, err := settingsService.GetPushoverConfig()
		assert.Error(t, err)

		assert.Equal(t, http.StatusOK, post("/api/settings/pushover", url.Values{"pushover_user_key": {"user-1"}, "pushover_app_token": {"token-1"}}))
		assert.Equal(t, http.StatusOK, post("/api/settings/pushover", url.Values{"pushover_user_key": {"user-2"}, "pushover_app_token": {""}}))
		config, err := settingsService.GetPushoverConfig()
		require.NoError(t, err)
		assert.Equal(t, "user-2", config.UserKey)
		assert.Equal(t, "token-1", config.AppToken)

		// A non-blank token still replaces the stored one.
		assert.Equal(t, http.StatusOK, post("/api/settings/pushover", url.Values{"pushover_user_key": {"user-2"}, "pushover_app_token": {"token-2"}}))
		config, err = settingsService.GetPushoverConfig()
		require.NoError(t, err)
		assert.Equal(t, "token-2", config.AppToken)
	})

	t.Run("telegram", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, post("/api/settings/telegram", url.Values{"telegram_bot_token": {""}, "telegram_chat_id": {"chat-1"}}))
		_, err := settingsService.GetTelegramConfig()
		assert.Error(t, err)

		assert.Equal(t, http.StatusOK, post("/api/settings/telegram", url.Values{"telegram_bot_token": {"bot-1"}, "telegram_chat_id": {"chat-1"}}))
		assert.Equal(t, http.StatusOK, post("/api/settings/telegram", url.Values{"telegram_bot_token": {""}, "telegram_chat_id": {"chat-2"}}))
		config, err := settingsService.GetTelegramConfig()
		require.NoError(t, err)
		assert.Equal(t, "chat-2", config.ChatID)
		assert.Equal(t, "bot-1", config.BotToken)

		assert.Equal(t, http.StatusOK, post("/api/settings/telegram", url.Values{"telegram_bot_token": {"bot-2"}, "telegram_chat_id": {"chat-2"}}))
		config, err = settingsService.GetTelegramConfig()
		require.NoError(t, err)
		assert.Equal(t, "bot-2", config.BotToken)
	})
}

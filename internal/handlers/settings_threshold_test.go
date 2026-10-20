package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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

package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSubscriptionCurrencyDefaultsAndUnknownEdit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	settingsRepo := repository.NewSettingsRepository(db)
	settings := service.NewSettingsService(settingsRepo)
	require.NoError(t, settings.SetCurrency("SEK"))
	categories := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categories)
	handler := NewSubscriptionHandler(subscriptions, settings, service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo), nil, nil, nil, nil, nil, categories, nil, nil)
	router := gin.New()
	router.SetHTMLTemplate(template.Must(template.New("subscription-form.html").Funcs(template.FuncMap{"t": func(_ interface{}, key string) string { return key }}).ParseFiles("../../templates/subscription-form.html")))
	router.POST("/api/subscriptions", handler.CreateSubscription)
	router.PUT("/api/subscriptions/:id", handler.UpdateSubscription)
	router.GET("/form/subscription/:id", handler.GetSubscriptionForm)

	form := url.Values{"name": {"Local"}, "cost": {"1"}, "schedule": {"Monthly"}, "status": {"Active"}}
	create := httptest.NewRequest(http.MethodPost, "/api/subscriptions", strings.NewReader(form.Encode()))
	create.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, create)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	all, err := subscriptions.GetAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "SEK", all[0].OriginalCurrency)

	updateForm := url.Values{"original_currency": {""}}
	update := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/api/subscriptions/%d", all[0].ID), strings.NewReader(updateForm.Encode()))
	update.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, update)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	saved, err := subscriptions.GetByID(all[0].ID)
	require.NoError(t, err)
	require.Equal(t, "SEK", saved.OriginalCurrency)

	require.NoError(t, db.Model(&models.Subscription{}).Where("id = ?", all[0].ID).Update("original_currency", "ARS").Error)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/form/subscription/%d", all[0].ID), nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `<option value="ARS" data-symbol="ARS" selected>ARS ARS</option>`)
}

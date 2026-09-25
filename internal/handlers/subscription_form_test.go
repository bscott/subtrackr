package handlers

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"testing"

	"subtrackr/internal/database"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGetSubscriptionForm_Integration_UsesPreferredCurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))

	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settingsService.SetCurrency("SEK"))

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptionService := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	handler := NewSubscriptionHandler(
		subscriptionService,
		settingsService,
		service.NewCurrencyService(repository.NewExchangeRateRepository(db)),
		nil,
		nil,
		nil,
		nil,
		nil,
		categoryService,
		nil,
		nil,
	)

	formTemplate := template.Must(template.New("subscription-form.html").Funcs(template.FuncMap{
		"t": func(_ interface{}, key string) string { return key },
	}).ParseFiles("../../templates/subscription-form.html"))
	router := gin.New()
	router.SetHTMLTemplate(formTemplate)
	router.GET("/form/subscription", handler.GetSubscriptionForm)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/form/subscription", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code, "body: %s", recorder.Body.String())
	assert.Contains(t, recorder.Body.String(), `<option value="SEK" data-symbol="kr" selected>kr SEK</option>`)
	assert.NotContains(t, recorder.Body.String(), `<option value="USD" data-symbol="$" selected>$ USD</option>`)
}

package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const wallosFixture = `{
  "success": true,
  "subscriptions": [
    {"Name":"Netflix","Payment Cycle":"Monthly","Next Payment":"2026-01-15","Category":"Entertainment","Payment Method":"Visa","Paid By":"Alice","Price":"$15.99","Notes":"Family","URL":"https://netflix.com","Notifications":"Enabled","Active":"Yes"},
    {"Name":"Adobe","Payment Cycle":"Yearly","Next Payment":"2026-06-01","Category":"Productivity","Price":"€59,99","Cancellation Date":"2026-05-20","Notifications":"Disabled","Active":"No"},
    {"Name":"","Payment Cycle":"Monthly","Price":"$1","Active":"Yes"}
  ]
}`

// newWallosTestHandler wires the subscription + category services needed by
// ImportWallos against an in-memory database.
func newWallosTestHandler(t *testing.T) (*gin.Engine, *service.SubscriptionService, *service.CategoryService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptionService := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := service.NewSettingsService(settingsRepo)
	currencyService := service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo)

	handler := NewSubscriptionHandler(
		subscriptionService,
		settingsService,
		currencyService,
		nil, // emailService
		nil, // pushoverService
		nil, // webhookService
		nil, // telegramService
		nil, // logoService
		categoryService,
		nil, // tagService
		nil, // i18nCatalog
	)

	router := gin.New()
	router.POST("/api/import/wallos", handler.ImportWallos)
	return router, subscriptionService, categoryService
}

func postWallosFile(t *testing.T, router *gin.Engine, field, filename, content, mode string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if mode != "" {
		require.NoError(t, writer.WriteField("mode", mode))
	}
	part, err := writer.CreateFormFile(field, filename)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest(http.MethodPost, "/api/import/wallos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// Integration/Functional — a full multipart upload imports the valid rows,
// creates categories, and reports the skipped row.
func TestImportWallos_Integration_HappyPath(t *testing.T) {
	router, subService, catService := newWallosTestHandler(t)

	rec := postWallosFile(t, router, "wallos_file", "wallos.json", wallosFixture, "merge")

	// One row is skipped (empty name) -> 207 Multi-Status with a warning.
	require.Equal(t, http.StatusMultiStatus, rec.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["imported_count"])

	subs, err := subService.GetAll()
	require.NoError(t, err)
	require.Len(t, subs, 2)

	names := map[string]bool{}
	for _, s := range subs {
		names[s.Name] = true
	}
	assert.True(t, names["Netflix"])
	assert.True(t, names["Adobe"])

	// Categories from the export were created.
	cats, err := catService.GetAll()
	require.NoError(t, err)
	catNames := map[string]bool{}
	for _, c := range cats {
		catNames[c.Name] = true
	}
	assert.True(t, catNames["Entertainment"])
	assert.True(t, catNames["Productivity"])
}

// Security/Frame — a missing file field is rejected with 400.
func TestImportWallos_NoFile(t *testing.T) {
	router, _, _ := newWallosTestHandler(t)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/import/wallos", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Security — a non-Wallos JSON upload is rejected rather than silently importing.
func TestImportWallos_RejectsNonWallosJSON(t *testing.T) {
	router, subService, _ := newWallosTestHandler(t)

	rec := postWallosFile(t, router, "wallos_file", "bad.json", `{"foo":"bar"}`, "merge")
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	subs, err := subService.GetAll()
	require.NoError(t, err)
	assert.Empty(t, subs)
}

// Functional — replace mode clears existing data first.
func TestImportWallos_ReplaceMode(t *testing.T) {
	router, subService, _ := newWallosTestHandler(t)

	// Seed one existing subscription.
	_, err := subService.Create(&models.Subscription{
		Name:     "Existing",
		Cost:     9.99,
		Schedule: "Monthly",
		Status:   "Active",
	})
	require.NoError(t, err)

	valid := `{"success":true,"subscriptions":[{"Name":"OnlyOne","Payment Cycle":"Monthly","Price":"$5","Active":"Yes"}]}`
	rec := postWallosFile(t, router, "wallos_file", "wallos.json", valid, "replace")
	require.Equal(t, http.StatusOK, rec.Code)

	subs, err := subService.GetAll()
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "OnlyOne", subs[0].Name)
}

package handlers

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"subtrackr/internal/i18n"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type calendarTestEvent struct {
	Cost           float64 `json:"cost"`
	CurrencySymbol string  `json:"currency_symbol"`
	Type           string  `json:"type"`
}

func newCalendarCurrencyTestRouter(t *testing.T, withRate bool, noticeDays ...int) *gin.Engine {
	t.Helper()
	t.Setenv("FIXER_API_KEY", "")
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "calendar.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Category{},
		&models.Tag{},
		&models.Subscription{},
		&models.ExchangeRate{},
		&models.Settings{},
	))

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := service.NewSettingsService(settingsRepo)
	require.NoError(t, settingsService.SetCurrency("SEK"))

	exchangeRateRepo := repository.NewExchangeRateRepository(db)
	if withRate {
		saveEURRates(t, exchangeRateRepo, time.Now().Add(-time.Hour))
	}

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptionService := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	renewalDate := time.Now().AddDate(0, 0, 1)
	notice := 0
	if len(noticeDays) > 0 {
		notice = noticeDays[0]
		renewalDate = time.Now().AddDate(0, 0, 30)
	}
	_, err = subscriptionService.Create(&models.Subscription{
		Name:                   "Foreign subscription",
		Cost:                   3,
		OriginalCurrency:       "USD",
		Schedule:               "Monthly",
		Status:                 "Active",
		RenewalDate:            &renewalDate,
		CancellationNoticeDays: notice,
	})
	require.NoError(t, err)

	handler := NewSubscriptionHandler(
		subscriptionService,
		settingsService,
		service.NewCurrencyService(exchangeRateRepo, settingsRepo),
		nil,
		nil,
		nil,
		nil,
		nil,
		categoryService,
		nil,
		i18n.NewCatalog(),
	)

	router := gin.New()
	router.SetHTMLTemplate(template.Must(template.New("calendar.html").Parse(`{{define "calendar.html"}}<script>{{.EventsByDate}}</script>{{end}}`)))
	router.GET("/calendar", handler.Calendar)
	router.GET("/api/export/ical", handler.ExportICal)
	return router
}

func getCalendarTestEvents(t *testing.T, router *gin.Engine) map[string][]calendarTestEvent {
	t.Helper()

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/calendar", nil))
	require.Equal(t, http.StatusOK, recorder.Code)

	var eventsByDate map[string][]calendarTestEvent
	eventsJSON := strings.TrimSuffix(strings.TrimPrefix(recorder.Body.String(), "<script>"), "</script>")
	require.NoError(t, json.Unmarshal([]byte(eventsJSON), &eventsByDate))
	return eventsByDate
}

func getCalendarTestEvent(t *testing.T, router *gin.Engine) calendarTestEvent {
	t.Helper()
	eventsByDate := getCalendarTestEvents(t, router)
	for _, events := range eventsByDate {
		require.Len(t, events, 1)
		return events[0]
	}
	t.Fatal("calendar response did not contain an event")
	return calendarTestEvent{}
}

func TestCalendar_ConvertsForeignCostWhenRateExists(t *testing.T) {
	event := getCalendarTestEvent(t, newCalendarCurrencyTestRouter(t, true))

	assert.InDelta(t, 30, event.Cost, 0.001)
	assert.Equal(t, "kr", event.CurrencySymbol)
}

func TestCalendar_UsesOriginalCurrencyWhenRateIsMissing(t *testing.T) {
	event := getCalendarTestEvent(t, newCalendarCurrencyTestRouter(t, false))

	assert.InDelta(t, 3, event.Cost, 0.001)
	assert.Equal(t, "$", event.CurrencySymbol)
}

func TestCalendar_KeepsCancellationEventsAlongsideConvertedCosts(t *testing.T) {
	router := newCalendarCurrencyTestRouter(t, true, 7)
	eventsByDate := getCalendarTestEvents(t, router)
	require.Len(t, eventsByDate, 2)
	var renewalDate, cancelDate time.Time
	for date, events := range eventsByDate {
		require.Len(t, events, 1)
		assert.InDelta(t, 30, events[0].Cost, 0.001)
		assert.Equal(t, "kr", events[0].CurrencySymbol)
		parsed, err := time.Parse("2006-01-02", date)
		require.NoError(t, err)
		switch events[0].Type {
		case "renewal":
			renewalDate = parsed
		case "cancel_by":
			cancelDate = parsed
		default:
			t.Fatalf("unexpected calendar event type %q", events[0].Type)
		}
	}
	require.False(t, renewalDate.IsZero())
	require.False(t, cancelDate.IsZero())
	assert.Equal(t, renewalDate.AddDate(0, 0, -7), cancelDate)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/export/ical", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, 2, strings.Count(recorder.Body.String(), "BEGIN:VEVENT"))
	assert.Contains(t, recorder.Body.String(), "SUMMARY:Foreign subscription Renewal")
	assert.Contains(t, recorder.Body.String(), "SUMMARY:Cancel Foreign subscription by today")
	assert.Contains(t, recorder.Body.String(), "DTSTART:"+cancelDate.Format("20060102"))
}

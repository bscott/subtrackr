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

// calendarTestNoticeRenewalDate returns a fixed UTC midday renewal date 30 days
// out, so calendar and iCal dates don't depend on the machine's local timezone.
func calendarTestNoticeRenewalDate() time.Time {
	d := time.Now().UTC().AddDate(0, 0, 30)
	return time.Date(d.Year(), d.Month(), d.Day(), 12, 0, 0, 0, time.UTC)
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
		renewalDate = calendarTestNoticeRenewalDate()
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
	wantRenewal := calendarTestNoticeRenewalDate()
	wantCancelBy := wantRenewal.AddDate(0, 0, -7)

	eventsByDate := getCalendarTestEvents(t, router)
	require.Len(t, eventsByDate, 2)
	for date, events := range eventsByDate {
		require.Len(t, events, 1)
		assert.InDelta(t, 30, events[0].Cost, 0.001)
		assert.Equal(t, "kr", events[0].CurrencySymbol)
		switch events[0].Type {
		case "renewal":
			assert.Equal(t, wantRenewal.Format("2006-01-02"), date)
		case "cancel_by":
			assert.Equal(t, wantCancelBy.Format("2006-01-02"), date)
		default:
			t.Fatalf("unexpected calendar event type %q", events[0].Type)
		}
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/export/ical", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	assert.Equal(t, 2, strings.Count(body, "BEGIN:VEVENT"))

	cancelEvent := findICalEvent(t, body, "UID:subtrackr-cancelby-")
	assert.Contains(t, cancelEvent, "\r\nDTSTART:"+wantCancelBy.Format("20060102T150000Z")+"\r\n")
	assert.Contains(t, cancelEvent, "\r\nSUMMARY:Cancel Foreign subscription by today\r\n")

	renewalEvent := findICalEvent(t, body, "UID:subtrackr-1-")
	assert.Contains(t, renewalEvent, "\r\nDTSTART:"+wantRenewal.Format("20060102T150000Z")+"\r\n")
	assert.Contains(t, renewalEvent, "\r\nSUMMARY:Foreign subscription Renewal\r\n")
}

// findICalEvent returns the single VEVENT block whose properties include a line
// starting with uidPrefix.
func findICalEvent(t *testing.T, ical, uidPrefix string) string {
	t.Helper()
	var match string
	for _, block := range strings.Split(ical, "BEGIN:VEVENT")[1:] {
		block = block[:strings.Index(block, "END:VEVENT")]
		if strings.Contains(block, "\r\n"+uidPrefix) {
			require.Empty(t, match, "more than one VEVENT matches %q", uidPrefix)
			match = block
		}
	}
	require.NotEmpty(t, match, "no VEVENT matches %q", uidPrefix)
	return match
}

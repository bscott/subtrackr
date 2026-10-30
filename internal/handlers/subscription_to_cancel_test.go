package handlers

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"html/template"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
)

func newToCancelRouter(t *testing.T) (*gin.Engine, *service.SubscriptionService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Initialize(filepath.Join(t.TempDir(), "to-cancel.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	settingsRepo := repository.NewSettingsRepository(db)
	handler := NewSubscriptionHandler(
		subscriptions,
		service.NewSettingsService(settingsRepo),
		service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo),
		nil, // notifier — only used on high-cost transition
		nil, // logoService — only used when a URL is set
		categoryService,
		nil, // tagService — only used when tags are submitted
		nil, // i18nCatalog
	)
	router := gin.New()
	router.POST("/api/subscriptions", handler.CreateSubscription)
	router.PUT("/api/subscriptions/:id", handler.UpdateSubscription)
	router.GET("/api/subscriptions/:id", handler.GetSubscription)
	router.GET("/api/export/csv", handler.ExportCSV)
	router.GET("/api/backup", handler.BackupData)
	router.POST("/api/restore", handler.RestoreData)
	router.POST("/api/subscriptions/:id/duplicate", handler.DuplicateSubscription)
	return router, subscriptions
}

func postForm(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func createViaForm(t *testing.T, router *gin.Engine, extra string) models.Subscription {
	t.Helper()
	rec := postForm(router, http.MethodPost, "/api/subscriptions", "name=S&cost=5&schedule=Monthly&status=Active&"+extra)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created models.Subscription
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
	return created
}

func TestCreateSubscription_ToCancelFields(t *testing.T) {
	tests := []struct {
		name     string
		form     string
		wantFlag bool
		wantDays int
	}{
		{"omitted is not opted in", "", false, 3},
		{"opted in without days defaults to 3", "to_cancel=true", true, 3},
		{"form's hidden field then checkbox", "to_cancel=false&to_cancel=true&to_cancel_reminder_days=14", true, 14},
		{"unchecked checkbox keeps only the hidden field", "to_cancel=false&to_cancel_reminder_days=5", false, 5},
		{"empty days is the default", "to_cancel=true&to_cancel_reminder_days=", true, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, subs := newToCancelRouter(t)
			created := createViaForm(t, router, tt.form)
			stored, err := subs.GetByID(created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantFlag, stored.ToCancel)
			assert.Equal(t, tt.wantDays, stored.ToCancelReminderDays)
		})
	}
}

func TestCreateSubscription_CancelledClearsToCancel(t *testing.T) {
	router, subs := newToCancelRouter(t)
	rec := postForm(router, http.MethodPost, "/api/subscriptions", "name=S&cost=5&schedule=Monthly&status=Cancelled&to_cancel=true")
	require.Equal(t, http.StatusCreated, rec.Code)
	all, err := subs.GetAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.False(t, all[0].ToCancel)
}

func TestToCancelReminderDays_InvalidValuesAreRejected(t *testing.T) {
	router, subs := newToCancelRouter(t)
	for _, invalid := range []string{"0", "4", "7", "-3", "abc", "3.5"} {
		rec := postForm(router, http.MethodPost, "/api/subscriptions", "name=S&cost=5&schedule=Monthly&status=Active&to_cancel=true&to_cancel_reminder_days="+invalid)
		assert.Equal(t, http.StatusBadRequest, rec.Code, invalid)
		assert.Contains(t, rec.Body.String(), "3, 5, or 14")
	}
	all, err := subs.GetAll()
	require.NoError(t, err)
	assert.Empty(t, all, "a rejected create stores nothing")

	created := createViaForm(t, router, "to_cancel=true&to_cancel_reminder_days=5")
	rec := postForm(router, http.MethodPut, "/api/subscriptions/"+strconv.Itoa(int(created.ID)), "to_cancel_reminder_days=7&notes=changed")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	stored, err := subs.GetByID(created.ID)
	require.NoError(t, err)
	assert.Equal(t, 5, stored.ToCancelReminderDays)
	assert.Empty(t, stored.Notes, "a rejected update changes nothing")
}

func TestToCancelReminderDays_HTMXErrorGoesToTheOpenForm(t *testing.T) {
	router, _ := newToCancelRouter(t)
	router.SetHTMLTemplate(template.Must(template.New("form-errors.html").Parse(`{{.Error}}`)))
	created := createViaForm(t, router, "")
	for method, path := range map[string]string{
		http.MethodPost: "/api/subscriptions",
		http.MethodPut:  "/api/subscriptions/" + strconv.Itoa(int(created.ID)),
	} {
		req := httptest.NewRequest(method, path, strings.NewReader("to_cancel_reminder_days=7"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("HX-Request", "true")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code, method)
		assert.Equal(t, "#form-errors", rec.Header().Get("HX-Retarget"), method)
		assert.Contains(t, rec.Body.String(), "3, 5, or 14", method)
	}
}

func TestUpdateSubscription_ToCancelFields(t *testing.T) {
	router, subs := newToCancelRouter(t)
	created := createViaForm(t, router, "renewal_date="+time.Now().AddDate(0, 0, 2).Format("2006-01-02"))
	path := "/api/subscriptions/" + strconv.Itoa(int(created.ID))
	update := func(body string) *models.Subscription {
		t.Helper()
		require.Equal(t, http.StatusOK, postForm(router, http.MethodPut, path, body).Code)
		stored, err := subs.GetByID(created.ID)
		require.NoError(t, err)
		return stored
	}

	stored := update("to_cancel=false&to_cancel=true&to_cancel_reminder_days=14")
	assert.True(t, stored.ToCancel)
	assert.Equal(t, 14, stored.ToCancelReminderDays)

	stored = update("notes=partial")
	assert.True(t, stored.ToCancel, "omitted fields keep their values")
	assert.Equal(t, 14, stored.ToCancelReminderDays)

	stored = update("to_cancel_reminder_days=")
	assert.Equal(t, 14, stored.ToCancelReminderDays, "an empty value keeps the choice")

	stored = update("to_cancel=false")
	assert.False(t, stored.ToCancel)
	assert.Equal(t, 14, stored.ToCancelReminderDays, "opting out remembers the choice")

	update("to_cancel=true")
	stored = update("status=Cancelled")
	assert.False(t, stored.ToCancel, "Cancelled clears the intent")

	stored = update("status=Active")
	assert.False(t, stored.ToCancel, "reactivating does not restore it")
}

func TestExportCSV_ToCancelColumnsAreTrailing(t *testing.T) {
	router, _ := newToCancelRouter(t)
	createViaForm(t, router, "to_cancel=true&to_cancel_reminder_days=14")
	createViaForm(t, router, "")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/export/csv", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	records, err := csv.NewReader(rec.Body).ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 3)

	header := records[0]
	assert.Equal(t, "Created At", header[len(header)-3], "existing columns keep their positions")
	assert.Equal(t, []string{"To Cancel", "To Cancel Reminder Days"}, header[len(header)-2:])
	values := map[string]bool{}
	for _, record := range records[1:] {
		require.Len(t, record, len(header))
		values[strings.Join(record[len(record)-2:], "/")] = true
	}
	assert.Equal(t, map[string]bool{"true/14": true, "false/3": true}, values)
}

func restoreBackup(t *testing.T, router *gin.Engine, backup []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("backup_file", "backup.json")
	require.NoError(t, err)
	_, err = file.Write(backup)
	require.NoError(t, err)
	require.NoError(t, form.WriteField("mode", "replace"))
	require.NoError(t, form.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/restore", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// A restore must not make every in-window subscription remind again, so the
// delivered state travels with the backup. It is bound to a deadline, so a
// restored entry whose deadline has since moved simply fires for the new one.
func TestBackupRestore_PreservesIntentAndDeliveredState(t *testing.T) {
	router, subs := newToCancelRouter(t)
	renewal := time.Now().AddDate(0, 0, 2)
	created, err := subs.Create(&models.Subscription{Name: "Kept", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal, ToCancel: true, ToCancelReminderDays: 14})
	require.NoError(t, err)
	stored, err := subs.GetByID(created.ID)
	require.NoError(t, err)
	marked, err := subs.MarkToCancelReminderSent(stored, *stored.ToCancelDeadline(), time.Now())
	require.NoError(t, err)
	require.True(t, marked)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/backup", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	backup := rec.Body.Bytes()

	require.Equal(t, http.StatusOK, restoreBackup(t, router, backup).Code)

	all, err := subs.GetAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.True(t, all[0].ToCancel)
	assert.Equal(t, 14, all[0].ToCancelReminderDays)
	require.NotNil(t, all[0].LastToCancelReminderAnchor)
	assert.True(t, all[0].LastToCancelReminderAnchor.Equal(*all[0].ToCancelDeadline()))
	due, err := subs.GetSubscriptionsNeedingToCancelReminders(time.Now())
	require.NoError(t, err)
	assert.Empty(t, due, "a restore does not repeat a delivered reminder")
}

func TestRestore_NormalizesToCancelAndAcceptsOlderBackups(t *testing.T) {
	router, subs := newToCancelRouter(t)
	stamp := time.Now().Format(time.RFC3339)
	backup, err := json.Marshal(map[string]any{
		"version": "test",
		"subscriptions": []map[string]any{
			{"name": "Older backup", "cost": 1, "schedule": "Monthly", "status": "Active"},
			{"name": "Bad days", "cost": 1, "schedule": "Monthly", "status": "Active", "to_cancel": true, "to_cancel_reminder_days": 7},
			{"name": "Cancelled", "cost": 1, "schedule": "Monthly", "status": "Cancelled", "to_cancel": true, "last_to_cancel_reminder_anchor": stamp, "last_to_cancel_reminder_sent": stamp},
		},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, restoreBackup(t, router, backup).Code)

	all, err := subs.GetAll()
	require.NoError(t, err)
	byName := map[string]models.Subscription{}
	for _, sub := range all {
		byName[sub.Name] = sub
	}
	assert.False(t, byName["Older backup"].ToCancel)
	assert.Equal(t, 3, byName["Older backup"].ToCancelReminderDays)
	assert.True(t, byName["Bad days"].ToCancel)
	assert.Equal(t, 3, byName["Bad days"].ToCancelReminderDays)
	assert.False(t, byName["Cancelled"].ToCancel)
	assert.Nil(t, byName["Cancelled"].LastToCancelReminderAnchor)
	assert.Nil(t, byName["Cancelled"].LastToCancelReminderSent)

	malformed := []byte(`{"subscriptions":[{"name":"Bad","cost":1,"schedule":"Monthly","status":"Active","to_cancel":"yes"}]}`)
	assert.Equal(t, http.StatusBadRequest, restoreBackup(t, router, malformed).Code)
	still, err := subs.GetAll()
	require.NoError(t, err)
	assert.Len(t, still, 3, "a rejected backup deletes nothing")
}

func TestDuplicateSubscription_KeepsToCancelIntent(t *testing.T) {
	router, subs := newToCancelRouter(t)
	created := createViaForm(t, router, "to_cancel=true&to_cancel_reminder_days=5")
	rec := postForm(router, http.MethodPost, "/api/subscriptions/"+strconv.Itoa(int(created.ID))+"/duplicate", "")
	require.Equal(t, http.StatusCreated, rec.Code)
	var dup models.Subscription
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &dup))
	stored, err := subs.GetByID(dup.ID)
	require.NoError(t, err)
	assert.True(t, stored.ToCancel)
	assert.Equal(t, 5, stored.ToCancelReminderDays)
}

func TestSubscriptionJSON_ExposesToCancelFields(t *testing.T) {
	router, _ := newToCancelRouter(t)
	created := createViaForm(t, router, "to_cancel=true&to_cancel_reminder_days=14")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/subscriptions/"+strconv.Itoa(int(created.ID)), nil))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, true, body["to_cancel"])
	assert.Equal(t, float64(14), body["to_cancel_reminder_days"])
	assert.Contains(t, body, "last_to_cancel_reminder_sent")
	assert.Contains(t, body, "last_to_cancel_reminder_anchor")
}

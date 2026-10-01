package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

// newSubscriptionUpdateTestRouter wires the minimal set of services needed to exercise
// subscription form checkbox handling against an in-memory database.
func newSubscriptionUpdateTestRouter(t *testing.T) (*gin.Engine, *service.SubscriptionService) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))

	subscriptionRepo := repository.NewSubscriptionRepository(db)
	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptionService := service.NewSubscriptionService(subscriptionRepo, categoryService)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := service.NewSettingsService(settingsRepo)
	currencyService := service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo)

	handler := NewSubscriptionHandler(
		subscriptionService,
		settingsService,
		currencyService,
		nil, // emailService — only used on high-cost transition
		nil, // pushoverService
		nil, // webhookService
		nil, // telegramService
		nil, // logoService — only used when a URL is set
		categoryService,
		nil, // tagService — only used when tags are submitted
		nil, // i18nCatalog
	)

	router := gin.New()
	router.POST("/subscriptions/:id", handler.UpdateSubscription)

	return router, subscriptionService
}

// TestUpdateSubscription_ReminderToggle reproduces issue #119. The edit form
// submits a hidden reminder_enabled=false immediately before the checkbox's
// reminder_enabled=true. Gin's GetPostForm returns the *first* value, so the
// handler must read the last value of the array for the checkbox to take effect.
func TestUpdateSubscription_ReminderToggle(t *testing.T) {
	tests := []struct {
		name     string
		formBody string // raw body, mirroring what the browser submits
		start    bool   // initial ReminderEnabled state
		want     bool   // expected state after the update
	}{
		{
			name:     "enable when checkbox checked (hidden false + true)",
			formBody: "reminder_enabled=false&reminder_enabled=true",
			start:    false,
			want:     true,
		},
		{
			name:     "disable when checkbox unchecked (hidden false only)",
			formBody: "reminder_enabled=false",
			start:    true,
			want:     false,
		},
		{
			name:     "field absent leaves value unchanged",
			formBody: "name=Netflix",
			start:    true,
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, svc := newSubscriptionUpdateTestRouter(t)

			created, err := svc.Create(&models.Subscription{
				Name:            "Netflix",
				Cost:            9.99,
				Schedule:        "Monthly",
				Status:          "Active",
				ReminderEnabled: tc.start,
			})
			require.NoError(t, err)

			req := httptest.NewRequest(
				http.MethodPost,
				"/subscriptions/"+strconv.FormatUint(uint64(created.ID), 10),
				strings.NewReader(tc.formBody),
			)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			updated, err := svc.GetByID(created.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, updated.ReminderEnabled)
		})
	}
}

func testBoolPointer(value bool) *bool {
	return &value
}

func TestUpdateSubscription_AutopayToggle(t *testing.T) {
	tests := []struct {
		name     string
		formBody string
		start    *bool
		want     *bool
	}{
		{
			name:     "unknown remains unknown when checkbox is untouched",
			formBody: "autopay_known=false&autopay=false",
			start:    nil,
			want:     nil,
		},
		{
			name:     "explicit false marks payment as manual",
			formBody: "autopay_known=true&autopay=false",
			start:    testBoolPointer(true),
			want:     testBoolPointer(false),
		},
		{
			name:     "checked value wins over hidden false",
			formBody: "autopay_known=true&autopay=false&autopay=true",
			start:    testBoolPointer(false),
			want:     testBoolPointer(true),
		},
		{
			name:     "absent field leaves value unchanged",
			formBody: "name=Netflix",
			start:    testBoolPointer(true),
			want:     testBoolPointer(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router, svc := newSubscriptionUpdateTestRouter(t)

			created, err := svc.Create(&models.Subscription{
				Name:            "Netflix",
				Cost:            9.99,
				Schedule:        "Monthly",
				Status:          "Active",
				ReminderEnabled: true,
				Autopay:         tc.start,
			})
			require.NoError(t, err)

			req := httptest.NewRequest(
				http.MethodPost,
				"/subscriptions/"+strconv.FormatUint(uint64(created.ID), 10),
				strings.NewReader(tc.formBody),
			)
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

			updated, err := svc.GetByID(created.ID)
			require.NoError(t, err)
			if tc.want == nil {
				assert.Nil(t, updated.Autopay)
			} else {
				require.NotNil(t, updated.Autopay)
				assert.Equal(t, *tc.want, *updated.Autopay)
			}
		})
	}
}

package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Unit tests send only to a local HTTP sink. Live integration tests require
// PUSHOVER_LIVE_TEST=1 as well as explicit credentials.
func capturePushover(t *testing.T, service *PushoverService) <-chan url.Values {
	t.Helper()
	requests := make(chan url.Values, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		requests <- r.PostForm
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":1,"request":"local-test"}`)
	}))
	t.Cleanup(server.Close)
	service.apiURL = server.URL
	return requests
}

func setupPushoverTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(
		&models.Settings{},
		&models.Category{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestPushoverService_SendNotification_NoConfig(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Try to send notification without config
	err := pushoverService.SendNotification("Test", "Test message", 0)
	assert.Error(t, err, "Should return error when Pushover is not configured")
	// Error will be "failed to get Pushover config: record not found" when no config exists
	assert.Contains(t, err.Error(), "Pushover config", "Error should mention Pushover config")
}

func TestPushoverService_SendNotification_EmptyUserKey(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Configure with empty user key (but valid app token)
	config := &models.PushoverConfig{
		UserKey:  "", // Empty user key
		AppToken: "test-app-token",
	}
	settingsService.SavePushoverConfig(config)

	err := pushoverService.SendNotification("Test", "Test message", 0)
	assert.Error(t, err, "Should return error when User Key is empty")
	assert.Contains(t, err.Error(), "not configured", "Error should mention not configured")
}

func TestPushoverService_SendNotification_EmptyAppToken(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Configure with empty app token
	config := &models.PushoverConfig{
		UserKey:  "test-user-key",
		AppToken: "",
	}
	settingsService.SavePushoverConfig(config)

	err := pushoverService.SendNotification("Test", "Test message", 0)
	assert.Error(t, err, "Should return error when App Token is empty")
	assert.Contains(t, err.Error(), "not configured", "Error should mention not configured")
}

func TestPushoverService_SendHighCostAlert_Disabled(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Ensure high cost alerts are disabled
	settingsService.SetBoolSetting("high_cost_alerts", false)

	subscription := &models.Subscription{
		Name:     "Test Subscription",
		Cost:     100.00,
		Schedule: "Monthly",
		Status:   "Active",
		Category: models.Category{Name: "Test"},
	}

	// Should return nil without error when disabled
	err := pushoverService.SendHighCostAlert(subscription)
	assert.NoError(t, err, "Should return nil when high cost alerts are disabled")
}

func TestPushoverService_SendHighCostAlert_EnabledButNoConfig(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Enable high cost alerts but don't configure Pushover
	settingsService.SetBoolSetting("high_cost_alerts", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:     "Test Subscription",
		Cost:     100.00,
		Schedule: "Monthly",
		Status:   "Active",
		Category: models.Category{Name: "Test"},
	}

	// Should return error when Pushover is not configured
	err := pushoverService.SendHighCostAlert(subscription)
	assert.Error(t, err, "Should return error when Pushover is not configured")
}

func TestPushoverService_SendRenewalReminder_Disabled(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Ensure renewal reminders are disabled
	settingsService.SetBoolSetting("renewal_reminders", false)

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
	}

	// Should return nil without error when disabled
	err := pushoverService.SendRenewalReminder(subscription, 3)
	assert.NoError(t, err, "Should return nil when renewal reminders are disabled")
}

func TestPushoverService_SendRenewalReminder_EnabledButNoConfig(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Enable renewal reminders but don't configure Pushover
	settingsService.SetBoolSetting("renewal_reminders", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
	}

	// Should return error when Pushover is not configured
	err := pushoverService.SendRenewalReminder(subscription, 3)
	assert.Error(t, err, "Should return error when Pushover is not configured")
}

func TestPushoverService_SendHighCostAlert_MessageFormat(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)
	requests := capturePushover(t, pushoverService)

	// Configure Pushover with invalid credentials (we're testing message format, not actual sending)
	config := &models.PushoverConfig{
		UserKey:  "test-user-key",
		AppToken: "test-app-token",
	}
	settingsService.SavePushoverConfig(config)
	settingsService.SetBoolSetting("high_cost_alerts", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Netflix",
		Cost:        15.99,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 30)),
		Category:    models.Category{Name: "Entertainment"},
		URL:         "https://netflix.com",
	}

	require.NoError(t, pushoverService.SendHighCostAlert(subscription))
	payload := <-requests
	assert.Equal(t, "test-user-key", payload.Get("user"))
	assert.Equal(t, "test-app-token", payload.Get("token"))
	assert.Equal(t, "1", payload.Get("priority"))
	assert.Contains(t, payload.Get("message"), "Netflix")
	assert.Contains(t, payload.Get("message"), "15.99")
}

func TestPushoverService_SendRenewalReminder_MessageFormat(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)
	requests := capturePushover(t, pushoverService)

	// Configure Pushover with invalid credentials (we're testing message format, not actual sending)
	config := &models.PushoverConfig{
		UserKey:  "test-user-key",
		AppToken: "test-app-token",
	}
	settingsService.SavePushoverConfig(config)
	settingsService.SetBoolSetting("renewal_reminders", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Netflix",
		Cost:        15.99,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Entertainment"},
		URL:         "https://netflix.com",
	}

	require.NoError(t, pushoverService.SendRenewalReminder(subscription, 3))
	payload := <-requests
	assert.Equal(t, "test-user-key", payload.Get("user"))
	assert.Equal(t, "test-app-token", payload.Get("token"))
	assert.Equal(t, "0", payload.Get("priority"))
	assert.Contains(t, payload.Get("message"), "Netflix")
	assert.Contains(t, payload.Get("message"), "15.99")
}

func TestPushoverService_SendRenewalReminder_DaysText(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)
	requests := capturePushover(t, pushoverService)

	// Configure Pushover
	config := &models.PushoverConfig{
		UserKey:  "test-user-key",
		AppToken: "test-app-token",
	}
	settingsService.SavePushoverConfig(config)
	settingsService.SetBoolSetting("renewal_reminders", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 1)),
		Category:    models.Category{Name: "Test"},
	}

	tests := []struct {
		name             string
		daysUntil        int
		expectedDaysText string
	}{
		{
			name:             "Singular day",
			daysUntil:        1,
			expectedDaysText: "day",
		},
		{
			name:             "Plural days",
			daysUntil:        3,
			expectedDaysText: "days",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, pushoverService.SendRenewalReminder(subscription, tt.daysUntil))
			payload := <-requests
			assert.Contains(t, payload.Get("message"), fmt.Sprintf("%d %s", tt.daysUntil, tt.expectedDaysText))
		})
	}
}

// getPushoverTestCredentials retrieves Pushover credentials from environment variables for integration testing
// Returns empty strings if not set (for unit tests)
func getPushoverTestCredentials() (userKey, appToken string) {
	if os.Getenv("PUSHOVER_LIVE_TEST") != "1" {
		return "", ""
	}
	userKey = os.Getenv("PUSHOVER_USER_KEY")
	appToken = os.Getenv("PUSHOVER_APP_TOKEN")
	return userKey, appToken
}

// TestPushoverService_SendNotification_Integration tests sending a real notification if credentials are provided
// This is an optional integration test that only runs if PUSHOVER_USER_KEY and PUSHOVER_APP_TOKEN are set
func TestPushoverService_SendNotification_Integration(t *testing.T) {
	userKey, appToken := getPushoverTestCredentials()
	if userKey == "" || appToken == "" {
		t.Skip("Skipping integration test: PUSHOVER_USER_KEY and PUSHOVER_APP_TOKEN environment variables not set")
	}

	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Configure Pushover with real credentials from environment
	config := &models.PushoverConfig{
		UserKey:  userKey,
		AppToken: appToken,
	}
	err := settingsService.SavePushoverConfig(config)
	assert.NoError(t, err, "Should save Pushover config")

	// Send a test notification
	err = pushoverService.SendNotification("SubTrackr Test", "This is a test notification from SubTrackr integration tests", 0)
	assert.NoError(t, err, "Should successfully send notification with valid credentials")
}

// TestPushoverService_SendHighCostAlert_Integration tests sending a real high cost alert if credentials are provided
func TestPushoverService_SendHighCostAlert_Integration(t *testing.T) {
	userKey, appToken := getPushoverTestCredentials()
	if userKey == "" || appToken == "" {
		t.Skip("Skipping integration test: PUSHOVER_USER_KEY and PUSHOVER_APP_TOKEN environment variables not set")
	}

	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Configure Pushover with real credentials
	config := &models.PushoverConfig{
		UserKey:  userKey,
		AppToken: appToken,
	}
	settingsService.SavePushoverConfig(config)
	settingsService.SetBoolSetting("high_cost_alerts", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Test High Cost Subscription",
		Cost:        100.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 30)),
		Category:    models.Category{Name: "Test"},
		URL:         "https://example.com",
	}

	err := pushoverService.SendHighCostAlert(subscription)
	assert.NoError(t, err, "Should successfully send high cost alert with valid credentials")
}

// TestPushoverService_SendRenewalReminder_Integration tests sending a real renewal reminder if credentials are provided
func TestPushoverService_SendRenewalReminder_Integration(t *testing.T) {
	userKey, appToken := getPushoverTestCredentials()
	if userKey == "" || appToken == "" {
		t.Skip("Skipping integration test: PUSHOVER_USER_KEY and PUSHOVER_APP_TOKEN environment variables not set")
	}

	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

	// Configure Pushover with real credentials
	config := &models.PushoverConfig{
		UserKey:  userKey,
		AppToken: appToken,
	}
	settingsService.SavePushoverConfig(config)
	settingsService.SetBoolSetting("renewal_reminders", true)
	settingsService.SetCurrency("USD")

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        15.99,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
		URL:         "https://example.com",
	}

	err := pushoverService.SendRenewalReminder(subscription, 3)
	assert.NoError(t, err, "Should successfully send renewal reminder with valid credentials")
}

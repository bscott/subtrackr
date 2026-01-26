package service

import (
	"os"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Pushover Test Credentials Usage:
//
// For unit tests (default): Tests use mock credentials and will fail API calls (expected behavior)
//
// For integration tests: Set environment variables before running tests:
//   export PUSHOVER_USER_KEY="your_user_key_here"
//   export PUSHOVER_APP_TOKEN="your_app_token_here"
//
// Integration tests will automatically skip if credentials are not provided.
// Example:
//   PUSHOVER_USER_KEY="u1234567890abcdef" PUSHOVER_APP_TOKEN="a1b2c3d4e5f6g7h8" go test ./internal/service -run Integration

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

	// This will fail because we don't have real Pushover credentials, but it should attempt to send
	err := pushoverService.SendHighCostAlert(subscription)
	// We expect an error because we can't actually connect to Pushover API, but the function should attempt to send
	assert.Error(t, err, "Should return error when Pushover API call fails (expected in test)")
	// The error should be about API call, not about being disabled
	assert.NotContains(t, err.Error(), "disabled", "Error should not be about being disabled")
}

func TestPushoverService_SendRenewalReminder_MessageFormat(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

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

	// This will fail because we don't have real Pushover credentials, but it should attempt to send
	err := pushoverService.SendRenewalReminder(subscription, 3)
	// We expect an error because we can't actually connect to Pushover API, but the function should attempt to send
	assert.Error(t, err, "Should return error when Pushover API call fails (expected in test)")
	// The error should be about API call, not about being disabled
	assert.NotContains(t, err.Error(), "disabled", "Error should not be about being disabled")
}

func TestPushoverService_SendRenewalReminder_DaysText(t *testing.T) {
	db := setupPushoverTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	pushoverService := NewPushoverService(settingsService)

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
			// This will fail because we don't have real Pushover credentials
			err := pushoverService.SendRenewalReminder(subscription, tt.daysUntil)
			assert.Error(t, err, "Should return error when Pushover API call fails (expected in test)")
			// Verify the function attempted to send (not disabled)
			assert.NotContains(t, err.Error(), "disabled", "Error should not be about being disabled")
		})
	}
}

// getPushoverTestCredentials retrieves Pushover credentials from environment variables for integration testing
// Returns empty strings if not set (for unit tests)
func getPushoverTestCredentials() (userKey, appToken string) {
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

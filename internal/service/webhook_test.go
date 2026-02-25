package service

import (
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupWebhookTestDB(t *testing.T) (*SettingsService, *WebhookService) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	err = db.AutoMigrate(&models.Settings{}, &models.Category{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	webhookService := NewWebhookService(settingsService)
	return settingsService, webhookService
}

func TestWebhookService_SendWebhook_NoConfig(t *testing.T) {
	_, ws := setupWebhookTestDB(t)

	payload := &WebhookPayload{
		Event:   "test",
		Title:   "Test",
		Message: "Test message",
	}

	err := ws.SendWebhook(payload)
	assert.Error(t, err, "Should error when webhook is not configured")
	assert.Contains(t, err.Error(), "webhook")
}

func TestWebhookService_SendWebhook_EmptyURL(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	config := &models.WebhookConfig{
		URL: "",
	}
	ss.SaveWebhookConfig(config)

	payload := &WebhookPayload{
		Event:   "test",
		Title:   "Test",
		Message: "Test message",
	}

	err := ws.SendWebhook(payload)
	assert.Error(t, err, "Should error when webhook URL is empty")
	assert.Contains(t, err.Error(), "URL required")
}

func TestWebhookService_SendHighCostAlert_Disabled(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("high_cost_alerts", false)

	sub := &models.Subscription{
		Name:     "Test Sub",
		Cost:     100.00,
		Schedule: "Monthly",
		Category: models.Category{Name: "Test"},
	}

	err := ws.SendHighCostAlert(sub)
	assert.NoError(t, err, "Should return nil when high cost alerts are disabled")
}

func TestWebhookService_SendHighCostAlert_EnabledNoConfig(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("high_cost_alerts", true)
	ss.SetCurrency("USD")

	sub := &models.Subscription{
		Name:     "Test Sub",
		Cost:     100.00,
		Schedule: "Monthly",
		Category: models.Category{Name: "Test"},
	}

	err := ws.SendHighCostAlert(sub)
	assert.Error(t, err, "Should error when webhook is not configured")
}

func TestWebhookService_SendRenewalReminder_Disabled(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("renewal_reminders", false)

	sub := &models.Subscription{
		Name:        "Test Sub",
		Cost:        10.00,
		Schedule:    "Monthly",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
	}

	err := ws.SendRenewalReminder(sub, 3)
	assert.NoError(t, err, "Should return nil when renewal reminders are disabled")
}

func TestWebhookService_SendRenewalReminder_EnabledNoConfig(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("renewal_reminders", true)
	ss.SetCurrency("USD")

	sub := &models.Subscription{
		Name:        "Test Sub",
		Cost:        10.00,
		Schedule:    "Monthly",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
	}

	err := ws.SendRenewalReminder(sub, 3)
	assert.Error(t, err, "Should error when webhook is not configured")
}

func TestWebhookService_SendCancellationReminder_Disabled(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("cancellation_reminders", false)

	sub := &models.Subscription{
		Name:             "Test Sub",
		Cost:             10.00,
		Schedule:         "Monthly",
		CancellationDate: timePtr(time.Now().AddDate(0, 0, 5)),
		Category:         models.Category{Name: "Test"},
	}

	err := ws.SendCancellationReminder(sub, 5)
	assert.NoError(t, err, "Should return nil when cancellation reminders are disabled")
}

func TestWebhookService_SendCancellationReminder_EnabledNoConfig(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("cancellation_reminders", true)
	ss.SetCurrency("USD")

	sub := &models.Subscription{
		Name:             "Test Sub",
		Cost:             10.00,
		Schedule:         "Monthly",
		CancellationDate: timePtr(time.Now().AddDate(0, 0, 5)),
		Category:         models.Category{Name: "Test"},
	}

	err := ws.SendCancellationReminder(sub, 5)
	assert.Error(t, err, "Should error when webhook is not configured")
}

func TestSubscriptionToWebhook(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	err = db.AutoMigrate(&models.Settings{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	settingsService.SetCurrency("USD")

	renewalDate := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	cancellationDate := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	sub := &models.Subscription{
		Name:             "Netflix",
		Cost:             15.99,
		OriginalCurrency: "EUR",
		Schedule:         "Monthly",
		Category:         models.Category{Name: "Entertainment"},
		URL:              "https://netflix.com",
		RenewalDate:      &renewalDate,
		CancellationDate: &cancellationDate,
	}
	sub.ID = 42

	ws := subscriptionToWebhook(sub, settingsService)

	assert.Equal(t, uint(42), ws.ID)
	assert.Equal(t, "Netflix", ws.Name)
	assert.Equal(t, 15.99, ws.Cost)
	assert.Equal(t, "EUR", ws.Currency)
	assert.Equal(t, "€", ws.CurrencySymbol)
	assert.Equal(t, "Monthly", ws.Schedule)
	assert.Equal(t, "Entertainment", ws.Category)
	assert.Equal(t, "https://netflix.com", ws.URL)
	assert.NotEmpty(t, ws.RenewalDate)
	assert.NotEmpty(t, ws.CancellationDate)
}

func TestSubscriptionToWebhook_MinimalFields(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	err = db.AutoMigrate(&models.Settings{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	settingsService.SetCurrency("USD")

	sub := &models.Subscription{
		Name:     "Basic Sub",
		Cost:     5.00,
		Schedule: "Monthly",
	}

	ws := subscriptionToWebhook(sub, settingsService)

	assert.Equal(t, "Basic Sub", ws.Name)
	assert.Equal(t, 5.00, ws.Cost)
	assert.Empty(t, ws.Category, "Category should be empty when not set")
	assert.Empty(t, ws.URL, "URL should be empty when not set")
	assert.Empty(t, ws.RenewalDate, "RenewalDate should be empty when nil")
	assert.Empty(t, ws.CancellationDate, "CancellationDate should be empty when nil")
}

func TestWebhookService_SendRenewalReminder_DaysText(t *testing.T) {
	ss, ws := setupWebhookTestDB(t)

	ss.SetBoolSetting("renewal_reminders", true)
	ss.SetCurrency("USD")

	sub := &models.Subscription{
		Name:        "Test Sub",
		Cost:        10.00,
		Schedule:    "Monthly",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
		Category:    models.Category{Name: "Test"},
	}

	tests := []struct {
		name      string
		daysUntil int
	}{
		{"Singular day", 1},
		{"Plural days", 3},
		{"Zero days", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This will fail at the webhook send level (no config), not at the days text level
			err := ws.SendRenewalReminder(sub, tt.daysUntil)
			assert.Error(t, err, "Should error when webhook is not configured")
			// Verify it got past the enabled check
			assert.NotContains(t, err.Error(), "disabled")
		})
	}
}

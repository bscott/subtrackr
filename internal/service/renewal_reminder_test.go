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

func setupRenewalReminderTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(
		&models.Subscription{},
		&models.Category{},
		&models.Settings{},
	)
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestSubscriptionService_GetSubscriptionsNeedingReminders(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	categoryService := NewCategoryService(categoryRepo)
	subscriptionService := NewSubscriptionService(subscriptionRepo, categoryService)

	now := time.Now()

	tests := []struct {
		name         string
		reminderDays int
		subscriptions []models.Subscription
		expectedCount int
		description   string
	}{
		{
			name:         "Subscription renewing in 3 days with 7 day reminder",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 1",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 3)), // 3 days from now
				},
			},
			expectedCount: 1,
			description:   "Should find subscription renewing within reminder window",
		},
		{
			name:         "Subscription renewing in 10 days with 7 day reminder",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 2",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 10)), // 10 days from now
				},
			},
			expectedCount: 0,
			description:   "Should not find subscription outside reminder window",
		},
		{
			name:         "Subscription renewing today",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 3",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.Add(12 * time.Hour)), // 12 hours from now
				},
			},
			expectedCount: 1,
			description:   "Should find subscription renewing today (within 24 hours)",
		},
		{
			name:         "Multiple subscriptions in reminder window",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 4",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 2)), // 2 days
				},
				{
					Name:        "Test Subscription 5",
					Cost:        20.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 5)), // 5 days
				},
				{
					Name:        "Test Subscription 6",
					Cost:        30.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 10)), // 10 days (outside window)
				},
			},
			expectedCount: 2,
			description:   "Should find only subscriptions within reminder window",
		},
		{
			name:         "Cancelled subscription should be excluded",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 7",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Cancelled",
					RenewalDate: timePtr(now.AddDate(0, 0, 3)), // 3 days
				},
			},
			expectedCount: 0,
			description:   "Should exclude cancelled subscriptions",
		},
		{
			name:         "Subscription without renewal date should be excluded",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 8",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: nil,
				},
			},
			expectedCount: 0,
			description:   "Should exclude subscriptions without renewal date",
		},
		{
			name:         "Zero reminder days should return empty",
			reminderDays: 0,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 9",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, 3)),
				},
			},
			expectedCount: 0,
			description:   "Should return empty when reminder days is 0",
		},
		{
			name:         "Past renewal date should be excluded",
			reminderDays: 7,
			subscriptions: []models.Subscription{
				{
					Name:        "Test Subscription 10",
					Cost:        10.00,
					Schedule:    "Monthly",
					Status:      "Active",
					RenewalDate: timePtr(now.AddDate(0, 0, -1)), // 1 day ago
				},
			},
			expectedCount: 0,
			description:   "Should exclude subscriptions with past renewal dates",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up previous test data
			db.Exec("DELETE FROM subscriptions")

			// Create test subscriptions
			for _, sub := range tt.subscriptions {
				err := db.Create(&sub).Error
				assert.NoError(t, err, "Failed to create test subscription")
			}

			// Get subscriptions needing reminders
			result, err := subscriptionService.GetSubscriptionsNeedingReminders(tt.reminderDays)
			assert.NoError(t, err, "GetSubscriptionsNeedingReminders should not return error")
			assert.Equal(t, tt.expectedCount, len(result), tt.description)

			// Verify days until renewal calculation
			for sub, daysUntil := range result {
				assert.GreaterOrEqual(t, daysUntil, 0, "Days until renewal should be non-negative")
				assert.LessOrEqual(t, daysUntil, tt.reminderDays, "Days until renewal should be within reminder window")
				assert.Equal(t, "Active", sub.Status, "Subscription should be active")
				assert.NotNil(t, sub.RenewalDate, "Subscription should have renewal date")
			}
		})
	}
}

func TestEmailService_SendRenewalReminder_Disabled(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	emailService := NewEmailService(settingsService)

	// Ensure reminders are disabled
	settingsService.SetBoolSetting("renewal_reminders", false)

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
	}

	// Should return nil without error when disabled
	err := emailService.SendRenewalReminder(subscription, 3)
	assert.NoError(t, err, "Should return nil when reminders are disabled")
}

func TestEmailService_SendRenewalReminder_EnabledButNoSMTP(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	emailService := NewEmailService(settingsService)

	// Enable reminders but don't configure SMTP
	settingsService.SetBoolSetting("renewal_reminders", true)

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
	}

	// Should return error when SMTP is not configured
	err := emailService.SendRenewalReminder(subscription, 3)
	assert.Error(t, err, "Should return error when SMTP is not configured")
	assert.Contains(t, err.Error(), "SMTP", "Error should mention SMTP")
}

func TestEmailService_SendRenewalReminder_WithSMTPConfig(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := NewSettingsService(settingsRepo)
	emailService := NewEmailService(settingsService)

	// Enable reminders
	settingsService.SetBoolSetting("renewal_reminders", true)

	// Configure SMTP (using invalid config - we're just testing the logic, not actual email sending)
	smtpConfig := &models.SMTPConfig{
		Host:     "smtp.example.com",
		Port:     587,
		Username: "test@example.com",
		Password: "password",
		From:     "test@example.com",
		FromName: "Test",
		To:       "recipient@example.com",
	}
	settingsService.SaveSMTPConfig(smtpConfig)

	subscription := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: timePtr(time.Now().AddDate(0, 0, 3)),
	}

	// This will fail because we don't have a real SMTP server, but it should get past the enabled check
	err := emailService.SendRenewalReminder(subscription, 3)
	// We expect an error because we can't actually connect to SMTP, but the function should attempt to send
	assert.Error(t, err, "Should return error when SMTP connection fails (expected in test)")
	// The error should be about connection, not about being disabled
	assert.NotContains(t, err.Error(), "disabled", "Error should not be about being disabled")
}

func TestSubscriptionService_GetSubscriptionsNeedingReminders_DaysCalculation(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	categoryService := NewCategoryService(categoryRepo)
	subscriptionService := NewSubscriptionService(subscriptionRepo, categoryService)

	now := time.Now()

	// Create subscription renewing in exactly 5 days
	renewalDate := now.AddDate(0, 0, 5)
	sub := &models.Subscription{
		Name:        "Test Subscription",
		Cost:        10.00,
		Schedule:    "Monthly",
		Status:      "Active",
		RenewalDate: &renewalDate,
	}
	err := db.Create(sub).Error
	assert.NoError(t, err)

	// Get subscriptions needing reminders with 7 day window
	result, err := subscriptionService.GetSubscriptionsNeedingReminders(7)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(result), "Should find one subscription")

	// Check days until renewal
	for foundSub, daysUntil := range result {
		assert.Equal(t, sub.ID, foundSub.ID, "Should be the same subscription")
		// Days should be approximately 5 (allowing for small time differences)
		assert.InDelta(t, 5, daysUntil, 1, "Days until renewal should be approximately 5")
	}
}

func TestSubscriptionService_GetSubscriptionsNeedingReminders_BoundaryCases(t *testing.T) {
	db := setupRenewalReminderTestDB(t)
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	categoryService := NewCategoryService(categoryRepo)
	subscriptionService := NewSubscriptionService(subscriptionRepo, categoryService)

	now := time.Now()

	tests := []struct {
		name         string
		renewalDate  time.Time
		reminderDays int
		shouldFind   bool
		description  string
	}{
		{
			name:         "Exactly at reminder window boundary",
			renewalDate:  now.AddDate(0, 0, 7), // Exactly 7 days
			reminderDays: 7,
			shouldFind:   true,
			description:  "Should find subscription renewing exactly at reminder window boundary",
		},
		{
			name:         "Just outside reminder window",
			renewalDate:  now.AddDate(0, 0, 8), // 8 days (outside 7 day window)
			reminderDays: 7,
			shouldFind:   false,
			description:  "Should not find subscription just outside reminder window",
		},
		{
			name:         "Renewing tomorrow",
			renewalDate:  now.AddDate(0, 0, 1), // 1 day
			reminderDays: 7,
			shouldFind:   true,
			description:  "Should find subscription renewing tomorrow",
		},
		{
			name:         "Renewing in 1 hour (less than 1 day)",
			renewalDate:  now.Add(1 * time.Hour),
			reminderDays: 7,
			shouldFind:   true,
			description:  "Should find subscription renewing in less than 1 day (counts as 0 days)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clean up
			db.Exec("DELETE FROM subscriptions")

			sub := &models.Subscription{
				Name:        "Test Subscription",
				Cost:        10.00,
				Schedule:    "Monthly",
				Status:      "Active",
				RenewalDate: &tt.renewalDate,
			}
			err := db.Create(sub).Error
			assert.NoError(t, err)

			result, err := subscriptionService.GetSubscriptionsNeedingReminders(tt.reminderDays)
			assert.NoError(t, err)

			if tt.shouldFind {
				assert.Equal(t, 1, len(result), tt.description)
			} else {
				assert.Equal(t, 0, len(result), tt.description)
			}
		})
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}


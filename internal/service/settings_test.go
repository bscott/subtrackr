package service

import (
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupSettingsTestDB(t *testing.T) *SettingsService {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	err = db.AutoMigrate(&models.Settings{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}
	settingsRepo := repository.NewSettingsRepository(db)
	return NewSettingsService(settingsRepo)
}

func TestSetDateFormat_Valid(t *testing.T) {
	s := setupSettingsTestDB(t)

	tests := []struct {
		name   string
		format string
	}{
		{"US format", "MM/DD/YYYY"},
		{"European format", "DD/MM/YYYY"},
		{"ISO format", "YYYY-MM-DD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.SetDateFormat(tt.format)
			assert.NoError(t, err)

			result := s.GetDateFormat()
			assert.Equal(t, tt.format, result)
		})
	}
}

func TestSetDateFormat_Invalid(t *testing.T) {
	s := setupSettingsTestDB(t)

	tests := []struct {
		name   string
		format string
	}{
		{"Empty string", ""},
		{"Random string", "foobar"},
		{"Close but wrong", "MM-DD-YYYY"},
		{"Lowercase", "mm/dd/yyyy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.SetDateFormat(tt.format)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid date format")
		})
	}
}

func TestGetDateFormat_Default(t *testing.T) {
	s := setupSettingsTestDB(t)

	format := s.GetDateFormat()
	assert.Equal(t, "MM/DD/YYYY", format, "Default date format should be MM/DD/YYYY")
}

func TestDateFormatToGo(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MM/DD/YYYY", "01/02/2006"},
		{"DD/MM/YYYY", "02/01/2006"},
		{"YYYY-MM-DD", "2006-01-02"},
		{"unknown", "01/02/2006"}, // defaults to US
		{"", "01/02/2006"},        // defaults to US
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, DateFormatToGo(tt.input))
		})
	}
}

func TestDateFormatToGoLong(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"MM/DD/YYYY", "January 2, 2006"},
		{"DD/MM/YYYY", "2 January 2006"},
		{"YYYY-MM-DD", "2006-01-02"},
		{"unknown", "January 2, 2006"}, // defaults to US
		{"", "January 2, 2006"},        // defaults to US
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, DateFormatToGoLong(tt.input))
		})
	}
}

func TestGetGoDateFormat(t *testing.T) {
	s := setupSettingsTestDB(t)

	// Default
	assert.Equal(t, "01/02/2006", s.GetGoDateFormat())

	// Set to European
	s.SetDateFormat("DD/MM/YYYY")
	assert.Equal(t, "02/01/2006", s.GetGoDateFormat())

	// Set to ISO
	s.SetDateFormat("YYYY-MM-DD")
	assert.Equal(t, "2006-01-02", s.GetGoDateFormat())
}

func TestGetGoDateFormatLong(t *testing.T) {
	s := setupSettingsTestDB(t)

	// Default
	assert.Equal(t, "January 2, 2006", s.GetGoDateFormatLong())

	// Set to European
	s.SetDateFormat("DD/MM/YYYY")
	assert.Equal(t, "2 January 2006", s.GetGoDateFormatLong())
}

func TestWebhookConfig_SaveAndRetrieve(t *testing.T) {
	s := setupSettingsTestDB(t)

	config := &models.WebhookConfig{
		URL: "https://example.com/webhook",
		Headers: map[string]string{
			"Authorization": "Bearer test-token",
			"X-Custom":      "value",
		},
	}

	err := s.SaveWebhookConfig(config)
	assert.NoError(t, err)

	retrieved, err := s.GetWebhookConfig()
	assert.NoError(t, err)
	assert.Equal(t, config.URL, retrieved.URL)
	assert.Equal(t, config.Headers, retrieved.Headers)
}

func TestWebhookConfig_NotConfigured(t *testing.T) {
	s := setupSettingsTestDB(t)

	_, err := s.GetWebhookConfig()
	assert.Error(t, err, "Should error when webhook not configured")
}

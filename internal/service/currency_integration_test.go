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

func setupTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(&models.ExchangeRate{})
	if err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	return db
}

func TestCurrencyService_Integration_IsEnabled(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)

	tests := []struct {
		name     string
		apiKey   string
		expected bool
	}{
		{
			name:     "Enabled with API key",
			apiKey:   "test-api-key",
			expected: true,
		},
		{
			name:     "Disabled without API key",
			apiKey:   "",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set or unset the environment variable
			if tt.apiKey != "" {
				os.Setenv("FIXER_API_KEY", tt.apiKey)
			} else {
				os.Unsetenv("FIXER_API_KEY")
			}

			service := NewCurrencyService(repo)
			assert.Equal(t, tt.expected, service.IsEnabled())
		})
	}

	// Clean up
	os.Unsetenv("FIXER_API_KEY")
}

func TestCurrencyService_Integration_ConvertAmount_SameCurrency(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo)

	// Test same currency conversion (should return same amount)
	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "USD")

	assert.NoError(t, err)
	assert.Equal(t, amount, result)
}

func TestCurrencyService_Integration_ConvertAmount_WithCachedRate(t *testing.T) {
	os.Setenv("FIXER_API_KEY", "test-key")
	defer os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo)

	// Create a cached rate
	cachedRate := &models.ExchangeRate{
		BaseCurrency: "USD",
		Currency:     "EUR",
		Rate:         0.85,
		Date:         time.Now(),
	}

	err := repo.SaveRates([]models.ExchangeRate{*cachedRate})
	assert.NoError(t, err)

	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "EUR")

	assert.NoError(t, err)
	assert.Equal(t, 85.0, result)
}

func TestCurrencyService_Integration_ConvertAmount_NoAPIKey(t *testing.T) {
	os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo)

	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "EUR")

	assert.Error(t, err)
	assert.Equal(t, 0.0, result)
	assert.Contains(t, err.Error(), "currency conversion not available")
}

func TestCurrencyService_Integration_ConvertAmount_InvalidAmount(t *testing.T) {
	os.Setenv("FIXER_API_KEY", "test-key")
	defer os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo)

	// Pre-cache a rate to avoid API calls
	cachedRate := models.ExchangeRate{
		BaseCurrency: "USD",
		Currency:     "EUR",
		Rate:         0.85,
		Date:         time.Now(),
	}
	repo.SaveRates([]models.ExchangeRate{cachedRate})

	tests := []struct {
		name     string
		amount   float64
		expected float64
	}{
		{"Negative amount", -100.0, -85.0}, // Negative amounts are converted
		{"Zero amount", 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.ConvertAmount(tt.amount, "USD", "EUR")
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCurrencyService_Integration_SupportedCurrencies(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo)

	// Test that common currencies are supported
	supportedCurrencies := []string{
		"USD", "EUR", "GBP", "CAD", "AUD", "JPY", "INR",
		"CHF", "SEK", "NOK", "DKK", "NZD", "SGD", "HKD",
	}

	for _, currency := range supportedCurrencies {
		t.Run(currency, func(t *testing.T) {
			// Test by attempting same-currency conversion (should always work)
			result, err := service.ConvertAmount(100.0, currency, currency)
			assert.NoError(t, err)
			assert.Equal(t, 100.0, result)
		})
	}
}
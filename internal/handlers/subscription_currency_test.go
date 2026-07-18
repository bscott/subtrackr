package handlers

import (
	"bytes"
	"log"
	"testing"
	"time"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newCurrencyTestHandler(t *testing.T) (*SubscriptionHandler, *repository.ExchangeRateRepository, *service.SettingsService) {
	t.Helper()
	t.Setenv("FIXER_API_KEY", "")

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ExchangeRate{}, &models.Settings{}))

	settingsRepo := repository.NewSettingsRepository(db)
	settingsService := service.NewSettingsService(settingsRepo)
	require.NoError(t, settingsService.SetCurrency("SEK"))

	exchangeRateRepo := repository.NewExchangeRateRepository(db)
	currencyService := service.NewCurrencyService(exchangeRateRepo, settingsRepo)

	return &SubscriptionHandler{
		settingsService: settingsService,
		currencyService: currencyService,
	}, exchangeRateRepo, settingsService
}

func saveEURRates(t *testing.T, repo *repository.ExchangeRateRepository, rateDate time.Time) {
	t.Helper()
	require.NoError(t, repo.SaveRates([]models.ExchangeRate{
		{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate},
		{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate},
	}))
}

func TestEnrichWithCurrencyConversion_UsesFreshCachedCrossRateWithoutAPIKey(t *testing.T) {
	handler, exchangeRateRepo, _ := newCurrencyTestHandler(t)
	rateDate := time.Now().Add(-time.Hour)
	saveEURRates(t, exchangeRateRepo, rateDate)

	result := handler.enrichWithCurrencyConversion([]models.Subscription{{
		Cost:             3,
		Schedule:         "Monthly",
		OriginalCurrency: "USD",
	}})

	require.Len(t, result, 1)
	assert.True(t, result[0].ShowConversion)
	assert.Equal(t, "SEK", result[0].DisplayCurrency)
	assert.Equal(t, "kr", result[0].DisplayCurrencySymbol)
	assert.InDelta(t, 30, result[0].ConvertedCost, 0.001)
	assert.WithinDuration(t, rateDate, result[0].ConversionRateDate, time.Millisecond)
	assert.False(t, result[0].ConversionRateStale)
}

func TestEnrichWithCurrencyConversion_ConvertsZeroCostWithoutInvalidValues(t *testing.T) {
	handler, exchangeRateRepo, _ := newCurrencyTestHandler(t)
	saveEURRates(t, exchangeRateRepo, time.Now().Add(-time.Hour))

	result := handler.enrichWithCurrencyConversion([]models.Subscription{{
		Cost:             0,
		Schedule:         "Monthly",
		ShareCount:       2,
		OriginalCurrency: "USD",
	}})

	require.Len(t, result, 1)
	assert.True(t, result[0].ShowConversion)
	assert.Equal(t, 0.0, result[0].ConvertedCost)
	assert.Equal(t, 0.0, result[0].ConvertedAnnualCost)
	assert.Equal(t, 0.0, result[0].ConvertedMonthlyCost)
	assert.Equal(t, 0.0, result[0].ConvertedShareCost)
}

func TestEnrichWithCurrencyConversion_UsesStaleCachedRateWithTimestamp(t *testing.T) {
	handler, exchangeRateRepo, _ := newCurrencyTestHandler(t)
	rateDate := time.Now().Add(-5 * 24 * time.Hour)
	saveEURRates(t, exchangeRateRepo, rateDate)

	result := handler.enrichWithCurrencyConversion([]models.Subscription{{
		Cost:             3,
		Schedule:         "Monthly",
		OriginalCurrency: "USD",
	}})

	require.Len(t, result, 1)
	assert.True(t, result[0].ShowConversion)
	assert.InDelta(t, 30, result[0].ConvertedCost, 0.001)
	assert.WithinDuration(t, rateDate, result[0].ConversionRateDate, time.Millisecond)
	assert.True(t, result[0].ConversionRateStale)
}

func TestEnrichWithCurrencyConversion_UsesOriginalCurrencyWhenNoRateExists(t *testing.T) {
	handler, _, _ := newCurrencyTestHandler(t)

	result := handler.enrichWithCurrencyConversion([]models.Subscription{{
		Cost:             3,
		Schedule:         "Monthly",
		OriginalCurrency: "USD",
	}})

	require.Len(t, result, 1)
	assert.False(t, result[0].ShowConversion)
	assert.Equal(t, "USD", result[0].DisplayCurrency)
	assert.Equal(t, "$", result[0].DisplayCurrencySymbol)
	assert.Equal(t, 3.0, result[0].ConvertedCost)
	assert.Equal(t, 3.0, result[0].ConvertedMonthlyCost)
	assert.Equal(t, 36.0, result[0].ConvertedAnnualCost)
}

func TestSortSubscriptionsByCost_UsesConvertedValuesWhenRatesExist(t *testing.T) {
	handler, exchangeRateRepo, _ := newCurrencyTestHandler(t)
	saveEURRates(t, exchangeRateRepo, time.Now().Add(-time.Hour))

	subscriptions := handler.enrichWithCurrencyConversion([]models.Subscription{
		{Name: "Foreign", Cost: 3, Schedule: "Monthly", OriginalCurrency: "USD"},
		{Name: "Local", Cost: 20, Schedule: "Monthly", OriginalCurrency: "SEK"},
	})
	sortSubscriptionsByCost(subscriptions, "SEK", "asc")

	assert.Equal(t, []string{"Local", "Foreign"}, []string{subscriptions[0].Name, subscriptions[1].Name})
}

func TestSortSubscriptionsByCost_GroupsMissingRatesAndSortsWithinCurrencies(t *testing.T) {
	handler, _, _ := newCurrencyTestHandler(t)
	subscriptions := handler.enrichWithCurrencyConversion([]models.Subscription{
		{Name: "USD 3", Cost: 3, Schedule: "Monthly", OriginalCurrency: "USD"},
		{Name: "SEK 20", Cost: 20, Schedule: "Monthly", OriginalCurrency: "SEK"},
		{Name: "USD 1", Cost: 1, Schedule: "Monthly", OriginalCurrency: "USD"},
		{Name: "SEK 10", Cost: 10, Schedule: "Monthly", OriginalCurrency: "SEK"},
	})
	sortSubscriptionsByCost(subscriptions, "SEK", "desc")

	assert.Equal(t,
		[]string{"SEK 20", "SEK 10", "USD 3", "USD 1"},
		[]string{subscriptions[0].Name, subscriptions[1].Name, subscriptions[2].Name, subscriptions[3].Name},
	)
}

func TestIsHighCostWithCurrency_DoesNotCompareUnconvertedCurrencies(t *testing.T) {
	handler, _, settingsService := newCurrencyTestHandler(t)
	require.NoError(t, settingsService.SetFloatSetting("high_cost_threshold", 50))

	var logs bytes.Buffer
	originalLogOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(originalLogOutput) })

	isHighCost := handler.isHighCostWithCurrency(&models.Subscription{
		Cost:             100,
		Schedule:         "Monthly",
		OriginalCurrency: "USD",
	})

	assert.False(t, isHighCost)
	assert.Contains(t, logs.String(), "Failed to convert currency for high-cost check (USD to SEK)")
	assert.NotContains(t, logs.String(), "Using direct comparison")
}

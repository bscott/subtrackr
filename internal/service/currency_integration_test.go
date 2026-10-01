package service

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "currency.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(&models.ExchangeRate{}, &models.Settings{})
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

			service := NewCurrencyService(repo, repository.NewSettingsRepository(db))
			assert.Equal(t, tt.expected, service.IsEnabled())
		})
	}

	// Clean up
	os.Unsetenv("FIXER_API_KEY")
}

func TestCurrencyService_Integration_ConvertAmount_SameCurrency(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

	// Test same currency conversion (should return same amount)
	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "USD")

	assert.NoError(t, err)
	assert.Equal(t, amount, result.Amount)
}

func TestCurrencyService_Integration_ConvertAmount_WithCachedRate(t *testing.T) {
	os.Setenv("FIXER_API_KEY", "test-key")
	defer os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

	// Create a cached EUR-based rate
	cachedRate := &models.ExchangeRate{
		BaseCurrency: "EUR",
		Currency:     "USD",
		Rate:         1.25,
		Date:         time.Now(),
	}

	err := repo.SaveRates([]models.ExchangeRate{*cachedRate})
	assert.NoError(t, err)

	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "EUR")

	assert.NoError(t, err)
	assert.Equal(t, 80.0, result.Amount)
}

func TestCurrencyService_Integration_ConvertAmount_NoAPIKey(t *testing.T) {
	os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

	amount := 100.0
	result, err := service.ConvertAmount(amount, "USD", "EUR")

	assert.Error(t, err)
	assert.Equal(t, 0.0, result.Amount)
	assert.Contains(t, err.Error(), "exchange rate for USD to EUR not available")
}

func TestCurrencyService_Integration_ConvertAmount_InvalidAmount(t *testing.T) {
	os.Setenv("FIXER_API_KEY", "test-key")
	defer os.Unsetenv("FIXER_API_KEY")

	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

	// Pre-cache a rate to avoid API calls
	cachedRate := models.ExchangeRate{
		BaseCurrency: "EUR",
		Currency:     "USD",
		Rate:         1.25,
		Date:         time.Now(),
	}
	repo.SaveRates([]models.ExchangeRate{cachedRate})

	tests := []struct {
		name     string
		amount   float64
		expected float64
	}{
		{"Negative amount", -100.0, -80.0}, // Negative amounts are converted
		{"Zero amount", 0.0, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.ConvertAmount(tt.amount, "USD", "EUR")
			assert.NoError(t, err)
			assert.Equal(t, tt.expected, result.Amount)
		})
	}
}

func TestCurrencyService_Integration_SupportedCurrencies(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

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
			assert.Equal(t, 100.0, result.Amount)
		})
	}
}

func TestCurrencyService_Integration_BDTCurrency(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))

	// Test BDT currency support
	t.Run("BDT same currency conversion", func(t *testing.T) {
		result, err := service.ConvertAmount(100.0, "BDT", "BDT")
		assert.NoError(t, err, "BDT should be supported")
		assert.Equal(t, 100.0, result.Amount, "Same currency conversion should return same amount")
	})

	t.Run("BDT in SupportedCurrencies list", func(t *testing.T) {
		found := false
		for _, currency := range SupportedCurrencies {
			if currency == "BDT" {
				found = true
				break
			}
		}
		assert.True(t, found, "BDT should be in SupportedCurrencies list")
	})
}

func TestSettingsService_GetCurrencySymbol_BDT(t *testing.T) {
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

	// Set currency to BDT
	err = settingsService.SetCurrency("BDT")
	assert.NoError(t, err, "Should be able to set BDT currency")

	// Get currency symbol
	symbol := settingsService.GetCurrencySymbol()
	assert.Equal(t, "৳", symbol, "BDT currency symbol should be ৳")

	// Verify currency is set correctly
	currency := settingsService.GetCurrency()
	assert.Equal(t, "BDT", currency, "Currency should be BDT")
}

func TestSettingsService_SetCurrency_BDT(t *testing.T) {
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

	tests := []struct {
		name           string
		currency       string
		shouldSucceed  bool
		expectedSymbol string
	}{
		{
			name:           "Valid BDT currency",
			currency:       "BDT",
			shouldSucceed:  true,
			expectedSymbol: "৳",
		},
		{
			name:          "Invalid currency",
			currency:      "XYZ",
			shouldSucceed: false,
		},
		{
			name:           "Other valid currencies",
			currency:       "USD",
			shouldSucceed:  true,
			expectedSymbol: "$",
		},
		{
			name:           "EUR currency",
			currency:       "EUR",
			shouldSucceed:  true,
			expectedSymbol: "€",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := settingsService.SetCurrency(tt.currency)
			if tt.shouldSucceed {
				assert.NoError(t, err, "Should succeed for valid currency")
				if tt.expectedSymbol != "" {
					symbol := settingsService.GetCurrencySymbol()
					assert.Equal(t, tt.expectedSymbol, symbol, "Currency symbol should match")
				}
			} else {
				assert.Error(t, err, "Should fail for invalid currency")
				assert.Contains(t, err.Error(), "invalid currency", "Error should mention invalid currency")
			}
		})
	}
}

func saveEURRates(t *testing.T, repo *repository.ExchangeRateRepository, rates ...models.ExchangeRate) {
	t.Helper()
	if err := repo.SaveRates(rates); err != nil {
		t.Fatalf("save EUR rates: %v", err)
	}
}

func TestCurrencyService_ConvertAmount_DerivesFreshCrossRateWithoutAPIKey(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	rateDate := time.Now().Add(-time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate},
	)

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(3, "USD", "SEK")

	assert.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
	assert.True(t, conversion.RateDate.Equal(rateDate))
	assert.False(t, conversion.Stale)
}

func TestCurrencyService_ConvertAmount_UsesFreshDirectEURRateWithoutAPIKey(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	rateDate := time.Now().Add(-time.Hour)
	saveEURRates(t, repo, models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate})

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(2, "EUR", "SEK")

	assert.NoError(t, err)
	assert.Equal(t, 24.0, conversion.Amount)
	assert.True(t, conversion.RateDate.Equal(rateDate))
	assert.False(t, conversion.Stale)
}

func TestCurrencyService_ConvertAmount_UsesFreshInverseEURRateWithoutAPIKey(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	rateDate := time.Now().Add(-time.Hour)
	saveEURRates(t, repo, models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate})

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(12, "USD", "EUR")

	assert.NoError(t, err)
	assert.Equal(t, 10.0, conversion.Amount)
	assert.True(t, conversion.RateDate.Equal(rateDate))
	assert.False(t, conversion.Stale)
}

func TestCurrencyService_ConvertAmount_UsesOlderLegAsQuoteDate(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	newerDate := time.Now().Add(-time.Hour)
	olderDate := newerDate.Add(-time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: newerDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: olderDate},
	)

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(3, "USD", "SEK")

	assert.NoError(t, err)
	assert.True(t, conversion.RateDate.Equal(olderDate))
	assert.False(t, conversion.Stale)
}

func TestCurrencyService_ConvertAmount_ReturnsStaleCachedCrossRateWithoutAPIKey(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	rateDate := time.Now().Add(-5 * 24 * time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate},
	)

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(3, "USD", "SEK")

	assert.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
	assert.True(t, conversion.RateDate.Equal(rateDate))
	assert.True(t, conversion.Stale)
}

func TestCurrencyService_ConvertAmount_FailsWithoutCompleteCachedPairOrAPIKey(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	saveEURRates(t, repo, models.ExchangeRate{
		BaseCurrency: "EUR",
		Currency:     "USD",
		Rate:         1.2,
		Date:         time.Now().Add(-time.Hour),
	})

	conversion, err := NewCurrencyService(repo, repository.NewSettingsRepository(db)).ConvertAmount(3, "USD", "SEK")

	assert.Error(t, err)
	assert.Zero(t, conversion.Amount)
}

func fixerSuccessBody(at time.Time) string {
	return fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","date":"%s","rates":{"USD":1.2,"SEK":12,"GBP":0.8}}`, at.Unix(), at.Format("2006-01-02"))
}

func newFixerTestServer(t *testing.T, body string, requestCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.URL.Query().Get("access_key") != "test-key" {
			t.Errorf("access_key = %q, want test-key", r.URL.Query().Get("access_key"))
		}
		if r.URL.Query().Get("base") != "EUR" {
			t.Errorf("base = %q, want EUR", r.URL.Query().Get("base"))
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write Fixer response: %v", err)
		}
	}))
}

func newTestCurrencyService(repo *repository.ExchangeRateRepository, settingsRepo *repository.SettingsRepository, server *httptest.Server) *CurrencyService {
	return createCurrencyService(repo, settingsRepo, server.Client(), server.URL, "test-key")
}

func captureCurrencyLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previousWriter := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previousWriter) })
	return &logs
}

func TestCurrencyService_RefreshesAllRatesOnceForConcurrentConversions(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	staleDate := now.Add(-5 * 24 * time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.1, Date: staleDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 11, Date: staleDate},
	)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, fixerSuccessBody(now), &requestCount)
	defer server.Close()
	currencyService := newTestCurrencyService(repo, settingsRepo, server)

	const conversionCount = 20
	results := make(chan Conversion, conversionCount)
	errors := make(chan error, conversionCount)
	var conversions sync.WaitGroup
	for range conversionCount {
		conversions.Add(1)
		go func() {
			defer conversions.Done()
			conversion, err := currencyService.ConvertAmount(3, "USD", "SEK")
			results <- conversion
			errors <- err
		}()
	}
	conversions.Wait()
	close(results)
	close(errors)

	for err := range errors {
		assert.NoError(t, err)
	}
	for conversion := range results {
		assert.Equal(t, 30.0, conversion.Amount)
		assert.False(t, conversion.Stale)
	}
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestCurrencyService_ReusesFreshSnapshotForDifferentCurrencyPair(t *testing.T) {
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, fixerSuccessBody(now), &requestCount)
	defer server.Close()
	currencyService := newTestCurrencyService(repo, settingsRepo, server)

	sekConversion, err := currencyService.ConvertAmount(3, "USD", "SEK")
	assert.NoError(t, err)
	assert.Equal(t, 30.0, sekConversion.Amount)

	usdConversion, err := currencyService.ConvertAmount(8, "GBP", "USD")
	assert.NoError(t, err)
	assert.InDelta(t, 12.0, usdConversion.Amount, 0.000001)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestCurrencyService_UsesStaleRatesWhenFixerReturnsQuotaError(t *testing.T) {
	logs := captureCurrencyLogs(t)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	staleDate := time.Now().Add(-5 * 24 * time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: staleDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: staleDate},
	)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, `{"success":false,"error":{"code":104,"info":"monthly quota reached"}}`, &requestCount)
	defer server.Close()

	conversion, err := newTestCurrencyService(repo, settingsRepo, server).ConvertAmount(3, "USD", "SEK")

	assert.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
	assert.True(t, conversion.Stale)
	assert.True(t, conversion.RateDate.Equal(staleDate))
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Contains(t, logs.String(), "monthly quota reached")
}

func TestCurrencyService_DoesNotRetryFailedRefreshDuringCooldown(t *testing.T) {
	logs := captureCurrencyLogs(t)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, `{"success":false,"error":{"code":104,"info":"monthly quota reached"}}`, &requestCount)
	defer server.Close()

	_, firstError := newTestCurrencyService(repo, settingsRepo, server).ConvertAmount(3, "USD", "SEK")
	_, secondError := newTestCurrencyService(repo, settingsRepo, server).ConvertAmount(3, "USD", "SEK")

	assert.Error(t, firstError)
	assert.Error(t, secondError)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Equal(t, 1, bytes.Count(logs.Bytes(), []byte("monthly quota reached")))
}

func TestCurrencyService_RetriesAfterCooldown(t *testing.T) {
	logs := captureCurrencyLogs(t)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, `{"success":false,"error":{"code":104,"info":"monthly quota reached"}}`, &requestCount)
	defer server.Close()
	currencyService := newTestCurrencyService(repo, settingsRepo, server)

	_, firstError := currencyService.ConvertAmount(3, "USD", "SEK")
	oldAttempt := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	if err := settingsRepo.Set(currencyRefreshLastAttemptKey, oldAttempt); err != nil {
		t.Fatalf("age failed refresh status: %v", err)
	}
	_, secondError := currencyService.ConvertAmount(3, "USD", "SEK")

	assert.Error(t, firstError)
	assert.Error(t, secondError)
	assert.Equal(t, int32(2), requestCount.Load())
	assert.Equal(t, 2, bytes.Count(logs.Bytes(), []byte("monthly quota reached")))
}

func TestCurrencyService_ReturnsErrorWhenRefreshFailsWithoutCachedPair(t *testing.T) {
	logs := captureCurrencyLogs(t)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, `{"success":false,"error":{"code":104,"info":"monthly quota reached"}}`, &requestCount)
	defer server.Close()

	conversion, err := newTestCurrencyService(repo, settingsRepo, server).ConvertAmount(3, "USD", "SEK")

	assert.Error(t, err)
	assert.Zero(t, conversion.Amount)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Contains(t, logs.String(), "monthly quota reached")
}

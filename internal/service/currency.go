package service

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"time"
)

// Supported currencies for exchange rate fetching
// Keep in sync with settings.SupportedCurrencies
const supportedCurrencySymbols = "USD,EUR,GBP,JPY,RUB,SEK,PLN,INR"

type CurrencyService struct {
	repo   *repository.ExchangeRateRepository
	apiKey string
}

type FixerResponse struct {
	Success   bool               `json:"success"`
	Timestamp int64              `json:"timestamp"`
	Base      string             `json:"base"`
	Date      string             `json:"date"`
	Rates     map[string]float64 `json:"rates"`
	Error     *FixerError        `json:"error,omitempty"`
}

type FixerError struct {
	Code int    `json:"code"`
	Info string `json:"info"`
}

func NewCurrencyService(repo *repository.ExchangeRateRepository) *CurrencyService {
	return &CurrencyService{
		repo:   repo,
		apiKey: os.Getenv("FIXER_API_KEY"),
	}
}

// IsEnabled returns true if currency conversion is enabled (API key is set)
func (s *CurrencyService) IsEnabled() bool {
	return s.apiKey != ""
}

// GetExchangeRate retrieves exchange rate between two currencies
func (s *CurrencyService) GetExchangeRate(fromCurrency, toCurrency string) (float64, error) {
	if fromCurrency == toCurrency {
		return 1.0, nil
	}

	// Try to get cached rate first
	rate, err := s.repo.GetRate(fromCurrency, toCurrency)
	if err == nil && !rate.IsStale() {
		return rate.Rate, nil
	}

	// If no API key, return error
	if !s.IsEnabled() {
		return 0, fmt.Errorf("currency conversion not available - no Fixer API key configured")
	}

	// Fetch from Fixer.io API
	return s.fetchAndCacheRates(fromCurrency, toCurrency)
}

// ConvertAmount converts an amount from one currency to another
func (s *CurrencyService) ConvertAmount(amount float64, fromCurrency, toCurrency string) (float64, error) {
	rate, err := s.GetExchangeRate(fromCurrency, toCurrency)
	if err != nil {
		return 0, err
	}
	return amount * rate, nil
}

// fetchAndCacheRates fetches rates from Fixer.io and caches them
func (s *CurrencyService) fetchAndCacheRates(baseCurrency, targetCurrency string) (float64, error) {
	// Use package-level currency symbols constant
	symbols := supportedCurrencySymbols

	// Free Fixer.io plan only supports EUR as base currency
	// Always fetch with EUR as base and calculate cross-rates if needed
	url := fmt.Sprintf("https://data.fixer.io/api/latest?access_key=%s&base=EUR&symbols=%s",
		s.apiKey, symbols)

	// Configure HTTP client with timeout for production reliability
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch exchange rates: %w", err)
	}
	defer resp.Body.Close()

	var fixerResp FixerResponse
	if err := json.NewDecoder(resp.Body).Decode(&fixerResp); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	if !fixerResp.Success {
		if fixerResp.Error != nil {
			return 0, fmt.Errorf("Fixer API error: %s", fixerResp.Error.Info)
		}
		return 0, fmt.Errorf("Fixer API request failed")
	}

	// Parse date
	rateDate := time.Unix(fixerResp.Timestamp, 0)

	// Cache all rates (always with EUR as base from Fixer.io)
	var ratesToSave []models.ExchangeRate

	// Add EUR to EUR rate (1.0)
	ratesToSave = append(ratesToSave, models.ExchangeRate{
		BaseCurrency: "EUR",
		Currency:     "EUR",
		Rate:         1.0,
		Date:         rateDate,
	})

	// Add all other rates from API
	for currency, rate := range fixerResp.Rates {
		ratesToSave = append(ratesToSave, models.ExchangeRate{
			BaseCurrency: "EUR",
			Currency:     currency,
			Rate:         rate,
			Date:         rateDate,
		})
	}

	if len(ratesToSave) > 0 {
		if err := s.repo.SaveRates(ratesToSave); err != nil {
			// Log error but don't fail the request
			log.Printf("Warning: failed to cache exchange rates: %v", err)
		}
	}

	// Calculate the cross-rate if needed
	if baseCurrency == "EUR" {
		// Direct rate from EUR
		if rate, exists := fixerResp.Rates[targetCurrency]; exists {
			return rate, nil
		}
	} else if targetCurrency == "EUR" {
		// Inverse rate to EUR
		if rate, exists := fixerResp.Rates[baseCurrency]; exists && rate != 0 {
			return 1.0 / rate, nil
		}
	} else {
		// Cross-rate: base->EUR->target
		baseToEur, exists1 := fixerResp.Rates[baseCurrency]
		eurToTarget, exists2 := fixerResp.Rates[targetCurrency]

		if exists1 && exists2 && baseToEur != 0 {
			// Convert: (1/baseToEur) * eurToTarget = cross rate
			return eurToTarget / baseToEur, nil
		}
	}

	return 0, fmt.Errorf("exchange rate for %s to %s not available", baseCurrency, targetCurrency)
}

// RefreshRates updates all exchange rates from the API
func (s *CurrencyService) RefreshRates() error {
	if !s.IsEnabled() {
		return fmt.Errorf("currency service not enabled")
	}

	// Fetch rates for major base currencies
	baseCurrencies := []string{"USD", "EUR"}

	for _, base := range baseCurrencies {
		_, err := s.fetchAndCacheRates(base, "USD") // Fetch all supported currencies
		if err != nil {
			return fmt.Errorf("failed to refresh rates for %s: %w", base, err)
		}
	}

	// Clean up old rates (keep last 7 days)
	return s.repo.DeleteStaleRates(7 * 24 * time.Hour)
}
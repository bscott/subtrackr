package service

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"time"
)

// CurrencyInfo holds metadata for a supported currency
type CurrencyInfo struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

// BuiltinCurrencies is the comprehensive list of supported currencies
var BuiltinCurrencies = []CurrencyInfo{
	{Code: "USD", Symbol: "$", Name: "US Dollar"},
	{Code: "EUR", Symbol: "€", Name: "Euro"},
	{Code: "GBP", Symbol: "£", Name: "British Pound"},
	{Code: "AUD", Symbol: "A$", Name: "Australian Dollar"},
	{Code: "CAD", Symbol: "C$", Name: "Canadian Dollar"},
	{Code: "NZD", Symbol: "NZ$", Name: "New Zealand Dollar"},
	{Code: "JPY", Symbol: "¥", Name: "Japanese Yen"},
	{Code: "CHF", Symbol: "Fr.", Name: "Swiss Franc"},
	{Code: "CNY", Symbol: "¥", Name: "Chinese Yuan"},
	{Code: "SEK", Symbol: "kr", Name: "Swedish Krona"},
	{Code: "NOK", Symbol: "kr", Name: "Norwegian Krone"},
	{Code: "DKK", Symbol: "kr", Name: "Danish Krone"},
	{Code: "INR", Symbol: "₹", Name: "Indian Rupee"},
	{Code: "RUB", Symbol: "₽", Name: "Russian Ruble"},
	{Code: "BRL", Symbol: "R$", Name: "Brazilian Real"},
	{Code: "PLN", Symbol: "zł", Name: "Polish Zloty"},
	{Code: "KRW", Symbol: "₩", Name: "South Korean Won"},
	{Code: "SGD", Symbol: "S$", Name: "Singapore Dollar"},
	{Code: "HKD", Symbol: "HK$", Name: "Hong Kong Dollar"},
	{Code: "MXN", Symbol: "Mex$", Name: "Mexican Peso"},
	{Code: "ZAR", Symbol: "R", Name: "South African Rand"},
	{Code: "TRY", Symbol: "₺", Name: "Turkish Lira"},
	{Code: "THB", Symbol: "฿", Name: "Thai Baht"},
	{Code: "COP", Symbol: "COL$", Name: "Colombian Peso"},
	{Code: "BDT", Symbol: "৳", Name: "Bangladeshi Taka"},
	{Code: "IDR", Symbol: "Rp", Name: "Indonesian Rupiah"},
	{Code: "PHP", Symbol: "₱", Name: "Philippine Peso"},
	{Code: "TWD", Symbol: "NT$", Name: "New Taiwan Dollar"},
	{Code: "MYR", Symbol: "RM", Name: "Malaysian Ringgit"},
	{Code: "AED", Symbol: "د.إ", Name: "UAE Dirham"},
	{Code: "SAR", Symbol: "﷼", Name: "Saudi Riyal"},
	{Code: "ILS", Symbol: "₪", Name: "Israeli Shekel"},
	{Code: "CZK", Symbol: "Kč", Name: "Czech Koruna"},
	{Code: "HUF", Symbol: "Ft", Name: "Hungarian Forint"},
	{Code: "RON", Symbol: "lei", Name: "Romanian Leu"},
}

// currencyInfoMap provides O(1) lookup by code
var currencyInfoMap map[string]CurrencyInfo

// SupportedCurrencies is derived from BuiltinCurrencies for backward compatibility
var SupportedCurrencies []string

func init() {
	currencyInfoMap = make(map[string]CurrencyInfo, len(BuiltinCurrencies))
	SupportedCurrencies = make([]string, len(BuiltinCurrencies))
	for i, c := range BuiltinCurrencies {
		currencyInfoMap[c.Code] = c
		SupportedCurrencies[i] = c.Code
	}
}

// GetCurrencyInfo returns metadata for a currency code, with a fallback for unknown codes
func GetCurrencyInfo(code string) CurrencyInfo {
	if info, ok := currencyInfoMap[code]; ok {
		return info
	}
	return CurrencyInfo{Code: code, Symbol: code, Name: code}
}

// GetAvailableCurrencies returns all supported currencies
func GetAvailableCurrencies() []CurrencyInfo {
	return BuiltinCurrencies
}

// supportedCurrencySymbols returns the currencies as a comma-separated string for API calls
func supportedCurrencySymbols() string {
	return strings.Join(SupportedCurrencies, ",")
}

type CurrencyService struct {
	repo   *repository.ExchangeRateRepository
	apiKey string
}

// Conversion contains a converted amount and the cache state used to calculate it.
type Conversion struct {
	Amount   float64
	RateDate time.Time
	Stale    bool
}

type exchangeRateQuote struct {
	Rate  float64
	Date  time.Time
	Stale bool
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
	quote, err := s.cachedExchangeRate(fromCurrency, toCurrency)
	if err != nil {
		return 0, err
	}
	return quote.Rate, nil
}

// ConvertAmount converts an amount from one currency to another
func (s *CurrencyService) ConvertAmount(amount float64, fromCurrency, toCurrency string) (Conversion, error) {
	if fromCurrency == toCurrency {
		return Conversion{Amount: amount, RateDate: time.Now()}, nil
	}

	quote, err := s.cachedExchangeRate(fromCurrency, toCurrency)
	if err != nil {
		return Conversion{}, err
	}
	return Conversion{
		Amount:   amount * quote.Rate,
		RateDate: quote.Date,
		Stale:    quote.Stale,
	}, nil
}

func (s *CurrencyService) cachedExchangeRate(fromCurrency, toCurrency string) (*exchangeRateQuote, error) {
	if fromCurrency == toCurrency {
		return &exchangeRateQuote{Rate: 1, Date: time.Now()}, nil
	}

	if fromCurrency == "EUR" {
		targetRate, err := s.eurRate(toCurrency)
		if err != nil {
			return nil, fmt.Errorf("exchange rate for %s to %s not available: %w", fromCurrency, toCurrency, err)
		}
		return &exchangeRateQuote{Rate: targetRate.Rate, Date: targetRate.Date, Stale: targetRate.IsStale()}, nil
	}

	if toCurrency == "EUR" {
		sourceRate, err := s.eurRate(fromCurrency)
		if err != nil || sourceRate.Rate == 0 {
			return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
		}
		return &exchangeRateQuote{Rate: 1 / sourceRate.Rate, Date: sourceRate.Date, Stale: sourceRate.IsStale()}, nil
	}

	sourceRate, err := s.eurRate(fromCurrency)
	if err != nil || sourceRate.Rate == 0 {
		return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
	}
	targetRate, err := s.eurRate(toCurrency)
	if err != nil {
		return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
	}

	return &exchangeRateQuote{
		Rate:  targetRate.Rate / sourceRate.Rate,
		Date:  olderTime(sourceRate.Date, targetRate.Date),
		Stale: sourceRate.IsStale() || targetRate.IsStale(),
	}, nil
}

func (s *CurrencyService) eurRate(currency string) (*models.ExchangeRate, error) {
	if currency == "EUR" {
		return &models.ExchangeRate{BaseCurrency: "EUR", Currency: "EUR", Rate: 1, Date: time.Now()}, nil
	}
	return s.repo.GetRate("EUR", currency)
}

func olderTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// fetchAndCacheRates fetches rates from Fixer.io and caches them.
// Note: Free Fixer.io plan only supports EUR base, so baseCurrency parameter
// is used for cross-rate calculations but API always fetches with EUR base.
func (s *CurrencyService) fetchAndCacheRates(baseCurrency, targetCurrency string) (float64, error) {
	// Use supported currencies as comma-separated string
	symbols := supportedCurrencySymbols()

	// Free Fixer.io plan only supports EUR as base currency
	// Always fetch with EUR as base and calculate cross-rates if needed
	apiURL := fmt.Sprintf("https://data.fixer.io/api/latest?access_key=%s&base=EUR&symbols=%s",
		s.apiKey, symbols)

	// Validate URL to ensure we're calling the expected API
	parsedURL, err := url.Parse(apiURL)
	if err != nil {
		return 0, fmt.Errorf("invalid API URL: %w", err)
	}
	if parsedURL.Host != "data.fixer.io" {
		return 0, fmt.Errorf("unauthorized API host: %s", parsedURL.Host)
	}

	// Configure HTTP client with security and timeout settings
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12, // Require TLS 1.2 or higher
			},
		},
	}
	resp, err := client.Get(apiURL)
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

	// Fetch rates once with EUR base (free Fixer.io plan only supports EUR base)
	// All cross-rates are calculated from this single API call
	_, err := s.fetchAndCacheRates("EUR", "USD")
	if err != nil {
		return fmt.Errorf("failed to refresh rates: %w", err)
	}

	// Clean up old rates (keep last 7 days)
	return s.repo.DeleteStaleRates(7 * 24 * time.Hour)
}

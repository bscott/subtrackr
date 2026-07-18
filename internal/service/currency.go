package service

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync"
	"time"

	"gorm.io/gorm"
)

const (
	fixerURL                      = "https://data.fixer.io/api/latest"
	currencyRefreshLastAttemptKey = "currency_refresh_last_attempt"
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
	repo         *repository.ExchangeRateRepository
	settingsRepo *repository.SettingsRepository
	apiKey       string

	refreshMu sync.Mutex
	client    *http.Client
	endpoint  string
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

func NewCurrencyService(repo *repository.ExchangeRateRepository, settingsRepo *repository.SettingsRepository) *CurrencyService {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
	}
	return createCurrencyService(repo, settingsRepo, client, fixerURL, os.Getenv("FIXER_API_KEY"))
}

func createCurrencyService(repo *repository.ExchangeRateRepository, settingsRepo *repository.SettingsRepository, client *http.Client, endpoint, apiKey string) *CurrencyService {
	return &CurrencyService{
		repo:         repo,
		settingsRepo: settingsRepo,
		apiKey:       apiKey,
		client:       client,
		endpoint:     endpoint,
	}
}

// IsEnabled returns true if currency conversion is enabled (API key is set)
func (s *CurrencyService) IsEnabled() bool {
	return s.apiKey != ""
}

// GetExchangeRate retrieves exchange rate between two currencies
func (s *CurrencyService) GetExchangeRate(fromCurrency, toCurrency string) (float64, error) {
	quote, err := s.exchangeRate(fromCurrency, toCurrency)
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

	quote, err := s.exchangeRate(fromCurrency, toCurrency)
	if err != nil {
		return Conversion{}, err
	}
	return Conversion{
		Amount:   amount * quote.Rate,
		RateDate: quote.Date,
		Stale:    quote.Stale,
	}, nil
}

func (s *CurrencyService) exchangeRate(fromCurrency, toCurrency string) (*exchangeRateQuote, error) {
	// A complete fresh quote avoids both the Fixer quota and network dependency.
	quote, cacheErr := s.cachedExchangeRate(fromCurrency, toCurrency)
	if cacheErr == nil && !quote.Stale {
		return quote, nil
	}

	// Stale cached rates remain useful when Fixer is not configured or unavailable.
	if !s.IsEnabled() {
		if cacheErr == nil {
			return quote, nil
		}
		return nil, fmt.Errorf("currency conversion not available - no Fixer API key configured: %w", cacheErr)
	}

	refreshErr := s.refreshRates(false)
	// Another request may have refreshed the shared snapshot while this one waited.
	refreshedQuote, refreshedErr := s.cachedExchangeRate(fromCurrency, toCurrency)
	if refreshedErr == nil {
		return refreshedQuote, nil
	}
	if cacheErr == nil {
		quote.Stale = true
		return quote, nil
	}
	if refreshErr != nil {
		return nil, fmt.Errorf("exchange rate for %s to %s not available: %w", fromCurrency, toCurrency, refreshErr)
	}
	return nil, refreshedErr
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

	// Free Fixer.io plans provide EUR-based legs, so derive cross-rates as
	// EUR-to-target divided by EUR-to-source.
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

// fetchAndCacheRates fetches the full EUR-based snapshot from Fixer.io and caches it.
// Cross-rates are derived from the cached EUR legs so one request serves every pair.
func (s *CurrencyService) fetchAndCacheRates() error {
	// Persist before the network call so failed attempts also enforce the cooldown
	// across requests and application restarts.
	attemptAt := time.Now().UTC().Format(time.RFC3339Nano)
	if err := s.settingsRepo.Set(currencyRefreshLastAttemptKey, attemptAt); err != nil {
		return fmt.Errorf("failed to record exchange-rate refresh attempt: %w", err)
	}

	// Free Fixer.io plans only support EUR as the base currency, so always fetch
	// every supported EUR leg and calculate cross-rates from the cache.
	parsedURL, err := url.Parse(s.endpoint)
	if err != nil {
		return fmt.Errorf("invalid Fixer endpoint: %w", err)
	}
	// Validate the production URL to ensure requests only go to the expected API.
	if s.endpoint == fixerURL && parsedURL.Host != "data.fixer.io" {
		return fmt.Errorf("unauthorized Fixer host: %s", parsedURL.Host)
	}
	query := parsedURL.Query()
	query.Set("access_key", s.apiKey)
	query.Set("base", "EUR")
	query.Set("symbols", supportedCurrencySymbols())
	parsedURL.RawQuery = query.Encode()

	resp, err := s.client.Get(parsedURL.String())
	if err != nil {
		return errors.New("failed to fetch exchange rates")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Fixer API returned HTTP status %d", resp.StatusCode)
	}

	var fixerResp FixerResponse
	if err := json.NewDecoder(resp.Body).Decode(&fixerResp); err != nil {
		return fmt.Errorf("failed to decode Fixer response: %w", err)
	}

	if !fixerResp.Success {
		if fixerResp.Error != nil {
			return fmt.Errorf("Fixer API error: %s", fixerResp.Error.Info)
		}
		return errors.New("Fixer API request failed")
	}

	// Parse the snapshot date supplied by Fixer rather than the request time.
	rateDate := time.Unix(fixerResp.Timestamp, 0)

	// Include the EUR identity rate because Fixer may omit it from the response.
	ratesToSave := []models.ExchangeRate{{
		BaseCurrency: "EUR",
		Currency:     "EUR",
		Rate:         1,
		Date:         rateDate,
	}}

	for currency, rate := range fixerResp.Rates {
		if currency == "EUR" {
			continue
		}
		ratesToSave = append(ratesToSave, models.ExchangeRate{
			BaseCurrency: "EUR",
			Currency:     currency,
			Rate:         rate,
			Date:         rateDate,
		})
	}

	// A refresh is only successful when the complete snapshot is persisted;
	// otherwise later conversions could observe an incomplete currency pair.
	if err := s.repo.SaveRates(ratesToSave); err != nil {
		return fmt.Errorf("failed to cache exchange rates: %w", err)
	}

	return nil
}

func (s *CurrencyService) refreshAllowed() (bool, error) {
	lastAttemptValue, err := s.settingsRepo.Get(currencyRefreshLastAttemptKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	lastAttemptAt, err := time.Parse(time.RFC3339Nano, lastAttemptValue)
	if err != nil {
		return false, fmt.Errorf("invalid currency refresh timestamp: %w", err)
	}
	// Use the last attempt, not the last success, so quota and outage failures do
	// not trigger another Fixer request on every page load.
	return !time.Now().Before(lastAttemptAt.Add(24 * time.Hour)), nil
}

func (s *CurrencyService) refreshRates(ignoreCooldown bool) error {
	// Serialize refreshes within the service; the persisted attempt timestamp then
	// prevents later service instances from immediately repeating the request.
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	// Explicit administrative refreshes retain the existing ability to bypass
	// automatic refresh timing.
	if !ignoreCooldown {
		allowed, err := s.refreshAllowed()
		if err != nil {
			return fmt.Errorf("failed to read exchange-rate refresh status: %w", err)
		}
		if !allowed {
			return errors.New("currency refresh cooldown is active")
		}
	}

	if err := s.fetchAndCacheRates(); err != nil {
		log.Printf("Warning: failed to refresh exchange rates: %v", err)
		return err
	}
	return nil
}

// RefreshRates updates all exchange rates from the API
func (s *CurrencyService) RefreshRates() error {
	if !s.IsEnabled() {
		return fmt.Errorf("currency service not enabled")
	}

	// Fetch one full EUR snapshot because the free Fixer.io plan only supports
	// EUR as its base; all cross-rates are derived from that single response.
	if err := s.refreshRates(true); err != nil {
		return fmt.Errorf("failed to refresh rates: %w", err)
	}

	// Clean up old rates (keep last 7 days)
	return s.repo.DeleteStaleRates(7 * 24 * time.Hour)
}

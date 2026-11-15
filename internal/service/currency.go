package service

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
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
	currencyRefreshFailuresKey    = "currency_refresh_failures"
	// currencyRateSourceKey records which source produced the cached rates.
	// Installations from before this key existed only ever used Fixer.io.
	currencyRateSourceKey = "currency_rate_source"

	// rateRefreshAfter is the age at which a cached rate triggers a refresh
	// attempt (still subject to the refresh cooldown).
	rateRefreshAfter = 24 * time.Hour
	// customSourceStaleAfter covers a normal weekend plus the daily refresh lag
	// for sources that publish once per business day, including ones that stamp
	// their snapshot with the date at midnight. Longer gaps, such as a bank
	// holiday weekend, are shown as out of date.
	customSourceStaleAfter = 120 * time.Hour
	// maxRateResponseBytes bounds the response read from the rate source.
	maxRateResponseBytes = 1 << 20
	// maxRateClockSkew is how far in the future a snapshot timestamp may be.
	// Anything later is rejected: it would outrank every later snapshot in the
	// cache, and usually means the source sent milliseconds, not seconds.
	maxRateClockSkew = time.Hour
	fixerHost        = "data.fixer.io"
	// minPlausibleRate and maxPlausibleRate bound an EUR rate. Every supported
	// currency is well inside them; values outside are bad data, and extreme
	// ones overflow to +Inf in 1/rate or cross-rates.
	minPlausibleRate = 1e-6
	maxPlausibleRate = 1e9
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
	{Code: "UAH", Symbol: "₴", Name: "Ukrainian Hryvnia"},
	{Code: "VND", Symbol: "₫", Name: "Vietnamese Dong"},
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

// zeroDecimalCurrencies lists the ISO 4217 currencies without a minor unit,
// whose amounts are shown as whole numbers
var zeroDecimalCurrencies = map[string]bool{
	"JPY": true,
	"KRW": true,
	"VND": true,
}

// CurrencyDecimals returns the number of fraction digits shown for a currency
func CurrencyDecimals(code string) int {
	if zeroDecimalCurrencies[code] {
		return 0
	}
	return 2
}

// FormatAmount formats an amount with the number of decimals its currency uses
func FormatAmount(amount float64, code string) string {
	return fmt.Sprintf("%.*f", CurrencyDecimals(code), amount)
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
	// customSource is set when FIXER_API_URL replaced the Fixer.io endpoint.
	customSource bool
	// misconfigured is set when FIXER_API_URL is set but unusable. Conversion
	// is then disabled rather than falling back to Fixer.io, so a key meant
	// for the operator's own source is never sent to a third party.
	misconfigured bool
}

// Conversion contains a converted amount and the cache state used to calculate it.
type Conversion struct {
	Amount   float64
	RateDate time.Time
	Stale    bool
	// Source is where the rate used for this conversion came from. It is the
	// zero value when no rate was needed (same currency).
	Source RateSource
}

type exchangeRateQuote struct {
	Rate  float64
	Date  time.Time
	Stale bool
	// RefreshDue is set once the rate is old enough that a refresh should be
	// attempted, which can be before it counts as Stale for a custom source.
	RefreshDue bool
	// sourceID identifies the source that produced the cached rate.
	sourceID string
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
	apiKey := os.Getenv("FIXER_API_KEY")
	endpoint, ok := rateEndpointFromEnv(apiKey)
	if !ok {
		s := createCurrencyService(repo, settingsRepo, newRateHTTPClient(), fixerURL, "")
		s.misconfigured = true
		return s
	}
	s := createCurrencyService(repo, settingsRepo, newRateHTTPClient(), endpoint, apiKey)
	s.customSource = !isFixerEndpoint(endpoint)
	return s
}

// newRateHTTPClient returns the client used for rate refreshes. It does not
// follow redirects: the request URL carries access_key, and following a
// redirect would hand that URL to the next host in the Referer header (and,
// for a Location that keeps the query, in the request itself). A redirect
// therefore fails the refresh with its status code.
func newRateHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			// Honour HTTPS_PROXY/HTTP_PROXY/NO_PROXY, as http.DefaultTransport does.
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// isFixerEndpoint reports whether endpoint is Fixer.io itself, including when
// FIXER_API_URL spells the Fixer.io URL differently (http, a trailing slash,
// upper-case host). Fixer.io keeps its own rules: it needs FIXER_API_KEY and
// uses the 24-hour stale window.
func isFixerEndpoint(endpoint string) bool {
	if endpoint == fixerURL {
		return true
	}
	parsed, err := url.Parse(endpoint)
	return err == nil && strings.EqualFold(parsed.Hostname(), fixerHost)
}

// rateEndpointFromEnv returns the exchange-rate endpoint to use. FIXER_API_URL
// lets the operator point the refresh at another server that answers in
// Fixer's "latest" format; unset keeps Fixer.io. ok is false when it is set
// but unusable, in which case conversion must be disabled (fail closed).
// The URL itself is never logged because it may carry credentials.
func rateEndpointFromEnv(apiKey string) (endpoint string, ok bool) {
	custom := strings.TrimSpace(os.Getenv("FIXER_API_URL"))
	if custom == "" {
		return fixerURL, true
	}
	parsed, err := url.Parse(custom)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		log.Printf("Error: FIXER_API_URL must be an absolute http(s) URL such as https://example.com/api/latest; currency conversion is disabled until it is fixed, and FIXER_API_KEY is not sent anywhere")
		return "", false
	}
	if parsed.Scheme == "http" && (apiKey != "" || parsed.RawQuery != "" || parsed.User != nil) {
		log.Printf("Warning: FIXER_API_URL uses http, so credentials are sent to %s unencrypted", parsed.Host)
	}
	return custom, true
}

// SourceMisconfigured reports whether FIXER_API_URL is set but unusable.
func (s *CurrencyService) SourceMisconfigured() bool {
	return s.misconfigured
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

// IsEnabled returns true if currency conversion is enabled: an API key is set,
// or the operator configured their own endpoint, which may not need a key.
func (s *CurrencyService) IsEnabled() bool {
	return s.apiKey != "" || s.usesCustomEndpoint()
}

func (s *CurrencyService) usesCustomEndpoint() bool {
	return s.customSource
}

// RateSource names where exchange rates come from, for attribution in the UI.
type RateSource struct {
	Name string
	URL  string
}

// RateSource returns the configured exchange-rate source. For a custom
// endpoint only the scheme and host are exposed, never the path, query or
// user info, which may hold credentials.
func (s *CurrencyService) RateSource() RateSource {
	return rateSourceFor(s.sourceID())
}

// sourceID identifies the configured source: Fixer.io, or the custom
// endpoint's scheme and host followed by a short hash of the whole URL. It is
// stored in the settings table, so the path, query and user info, any of
// which may hold credentials, appear only through the hash. A rotated key
// therefore counts as a new source, which costs one extra refresh.
func (s *CurrencyService) sourceID() string {
	if !s.usesCustomEndpoint() {
		return fixerURL
	}
	parsed, err := url.Parse(s.endpoint)
	if err != nil {
		return "custom"
	}
	return parsed.Scheme + "://" + strings.ToLower(parsed.Host) + "/" + s.sourceHash()
}

// sourceHash is a short, non-reversible tag for the configured endpoint.
func (s *CurrencyService) sourceHash() string {
	sum := sha256.Sum256([]byte(s.endpoint))
	return "sha256-" + hex.EncodeToString(sum[:8])
}

// refreshKeys returns the settings keys holding the refresh cooldown state
// for the configured source. Each source has its own, so a source change is
// not held back by the old source's cooldown, and two processes configured
// with different sources (a misconfiguration) still respect each source's
// daily limit instead of resetting each other. Fixer.io keeps the original
// key names, so existing installations keep their state.
func (s *CurrencyService) refreshKeys() (lastAttemptKey, failuresKey string) {
	if !s.usesCustomEndpoint() {
		return currencyRefreshLastAttemptKey, currencyRefreshFailuresKey
	}
	suffix := "_" + s.sourceHash()
	return currencyRefreshLastAttemptKey + suffix, currencyRefreshFailuresKey + suffix
}

// rateSourceFor turns a stored source identity into a displayable source.
func rateSourceFor(id string) RateSource {
	if id == "" || id == fixerURL {
		return RateSource{Name: "Fixer.io", URL: "https://fixer.io"}
	}
	parsed, err := url.Parse(id)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return RateSource{Name: "a custom source"}
	}
	return RateSource{Name: parsed.Hostname(), URL: parsed.Scheme + "://" + parsed.Host}
}

// storedSourceID reads a source identity from settings. A missing value means
// an installation from before sources were recorded, which only used Fixer.io.
func (s *CurrencyService) storedSourceID(key string) string {
	value, err := s.settingsRepo.Get(key)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("Warning: failed to read %s; assuming Fixer.io: %v", key, err)
		}
		return fixerURL
	}
	if value == "" {
		return fixerURL
	}
	return value
}

// staleAfter is how old a rate may be before it is shown as out of date.
// Fixer.io updates throughout the day. A custom source is often a central-bank
// feed such as the ECB's, which publishes once per business day and not at
// weekends, so its Friday rates are still the latest on Monday morning.
func (s *CurrencyService) staleAfter() time.Duration {
	if s.usesCustomEndpoint() {
		return customSourceStaleAfter
	}
	return rateRefreshAfter
}

func (s *CurrencyService) quote(rate float64, date time.Time, sourceID string) *exchangeRateQuote {
	age := time.Since(date)
	return &exchangeRateQuote{
		Rate:       rate,
		Date:       date,
		Stale:      age > s.staleAfter(),
		RefreshDue: age > rateRefreshAfter,
		sourceID:   sourceID,
	}
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
	converted := amount * quote.Rate
	// A non-finite amount would render as +Inf and break JSON encoding of
	// stats and API responses, so refuse it rather than pass it on.
	if math.IsInf(converted, 0) || math.IsNaN(converted) {
		return Conversion{}, fmt.Errorf("converting %v %s to %s does not give a finite amount", amount, fromCurrency, toCurrency)
	}
	return Conversion{
		Amount:   converted,
		RateDate: quote.Date,
		Stale:    quote.Stale,
		Source:   rateSourceFor(quote.sourceID),
	}, nil
}

func (s *CurrencyService) exchangeRate(fromCurrency, toCurrency string) (*exchangeRateQuote, error) {
	// A complete fresh quote avoids both the Fixer quota and network dependency.
	// Rates cached from a different source than the configured one are
	// refreshed even when fresh, so a source change takes effect promptly.
	quote, cacheErr := s.cachedExchangeRate(fromCurrency, toCurrency)
	if cacheErr == nil && !quote.RefreshDue && quote.sourceID == s.sourceID() {
		return quote, nil
	}

	// Stale cached rates remain useful when Fixer is not configured or unavailable.
	if !s.IsEnabled() {
		if cacheErr == nil {
			return quote, nil
		}
		return nil, fmt.Errorf("currency conversion not available - no Fixer API key configured: %w", cacheErr)
	}

	// Concurrent requests wait for one shared refresh and then all use its
	// snapshot. A slow source can therefore delay them by up to the client
	// timeout, but only once per cooldown window.
	refreshErr := s.refreshRates(false)
	// Another request may have refreshed the shared snapshot while this one waited.
	refreshedQuote, refreshedErr := s.cachedExchangeRate(fromCurrency, toCurrency)
	if refreshedErr == nil {
		return refreshedQuote, nil
	}
	if cacheErr == nil {
		// Stale already reflects the cached rate's age against this source's
		// window; a failed or skipped refresh does not make it older.
		return quote, nil
	}
	if refreshErr != nil {
		return nil, fmt.Errorf("exchange rate for %s to %s not available: %w", fromCurrency, toCurrency, refreshErr)
	}
	return nil, refreshedErr
}

func (s *CurrencyService) cachedExchangeRate(fromCurrency, toCurrency string) (*exchangeRateQuote, error) {
	if fromCurrency == toCurrency {
		return &exchangeRateQuote{Rate: 1, Date: time.Now(), sourceID: s.sourceID()}, nil
	}
	sourceID := s.storedSourceID(currencyRateSourceKey)

	if fromCurrency == "EUR" {
		targetRate, err := s.eurRate(toCurrency)
		if err != nil || !plausibleRate(targetRate.Rate) {
			return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
		}
		return s.quote(targetRate.Rate, targetRate.Date, sourceID), nil
	}

	if toCurrency == "EUR" {
		sourceRate, err := s.eurRate(fromCurrency)
		if err != nil || !plausibleRate(sourceRate.Rate) {
			return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
		}
		return s.quote(1/sourceRate.Rate, sourceRate.Date, sourceID), nil
	}

	// Free Fixer.io plans provide EUR-based legs, so derive cross-rates as
	// EUR-to-target divided by EUR-to-source.
	sourceRate, err := s.eurRate(fromCurrency)
	if err != nil || !plausibleRate(sourceRate.Rate) {
		return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
	}
	targetRate, err := s.eurRate(toCurrency)
	if err != nil || !plausibleRate(targetRate.Rate) {
		return nil, fmt.Errorf("exchange rate for %s to %s not available", fromCurrency, toCurrency)
	}

	// The older leg determines both staleness and whether a refresh is due.
	return s.quote(targetRate.Rate/sourceRate.Rate, olderTime(sourceRate.Date, targetRate.Date), sourceID), nil
}

func (s *CurrencyService) eurRate(currency string) (*models.ExchangeRate, error) {
	if currency == "EUR" {
		return &models.ExchangeRate{BaseCurrency: "EUR", Currency: "EUR", Rate: 1, Date: time.Now()}, nil
	}
	return s.repo.GetRate("EUR", currency)
}

// truncateForLog bounds a value from a remote response before it is logged.
func truncateForLog(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func olderTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// fetchAndCacheRates fetches the full EUR-based snapshot from the rate source
// and caches it. Cross-rates are derived from the cached EUR legs so one
// request serves every pair.
func (s *CurrencyService) fetchAndCacheRates() error {
	sourceID := s.sourceID()
	// Persist before the network call so failed attempts also enforce the cooldown
	// across requests and application restarts.
	attemptAt := time.Now().UTC().Format(time.RFC3339Nano)
	lastAttemptKey, _ := s.refreshKeys()
	if err := s.settingsRepo.Set(lastAttemptKey, attemptAt); err != nil {
		return fmt.Errorf("failed to record exchange-rate refresh attempt: %w", err)
	}

	// Free Fixer.io plans only support EUR as the base currency, so always fetch
	// every supported EUR leg and calculate cross-rates from the cache.
	parsedURL, err := url.Parse(s.endpoint)
	if err != nil {
		return errors.New("invalid exchange-rate endpoint")
	}
	// Validate the production URL to ensure requests only go to the expected API.
	if s.endpoint == fixerURL && parsedURL.Host != fixerHost {
		return fmt.Errorf("unauthorized Fixer host: %s", parsedURL.Host)
	}
	query := parsedURL.Query()
	if s.apiKey != "" {
		query.Set("access_key", s.apiKey)
	}
	query.Set("base", "EUR")
	query.Set("symbols", supportedCurrencySymbols())
	parsedURL.RawQuery = query.Encode()

	resp, err := s.client.Get(parsedURL.String())
	if err != nil {
		// *url.Error embeds the request URL, which carries access_key. The
		// underlying cause can also quote remote input, such as a malformed
		// Location header, so credentials are redacted from it as well.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("failed to fetch exchange rates from %s: %s", parsedURL.Host, s.redact(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusMultipleChoices && resp.StatusCode < http.StatusBadRequest {
		return fmt.Errorf("exchange-rate source returned HTTP status %d; redirects are not followed, so set FIXER_API_URL to the final URL", resp.StatusCode)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Fixer API returned HTTP status %d", resp.StatusCode)
	}

	var fixerResp FixerResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRateResponseBytes)).Decode(&fixerResp); err != nil {
		return fmt.Errorf("failed to decode Fixer response: %w", err)
	}

	if !fixerResp.Success {
		if fixerResp.Error != nil {
			return fmt.Errorf("Fixer API error: %q", truncateForLog(s.redact(fixerResp.Error.Info), 200))
		}
		return errors.New("Fixer API request failed")
	}
	// Every rate is stored as an EUR leg, so a snapshot on any other base, or
	// one that does not say, would produce silently wrong conversions.
	if !strings.EqualFold(fixerResp.Base, "EUR") {
		return fmt.Errorf("exchange-rate source returned base %q, want EUR", truncateForLog(s.redact(fixerResp.Base), 16))
	}
	if eur, ok := fixerResp.Rates["EUR"]; ok && math.Abs(eur-1) > 1e-9 {
		return fmt.Errorf("exchange-rate source returned EUR rate %v on an EUR base, want 1", eur)
	}

	rateDate, err := snapshotTime(fixerResp, time.Now())
	if err != nil {
		// The error quotes the response's date, which a source could fill
		// with anything, including the credentials it was sent.
		return errors.New(s.redact(err.Error()))
	}

	// Include the EUR identity rate because Fixer may omit it from the response.
	ratesToSave := []models.ExchangeRate{{
		BaseCurrency: "EUR",
		Currency:     "EUR",
		Rate:         1,
		Date:         rateDate,
	}}

	for currency, rate := range fixerResp.Rates {
		// Only cache the currencies that were requested, with usable rates; a
		// source may ignore the symbols parameter.
		if currency == "EUR" || !isSupportedCurrency(currency) || !plausibleRate(rate) {
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
	// After a source change the old source's rates are dropped, so they can
	// neither outrank the new snapshot nor stand in for currencies it omits.
	if s.storedSourceID(currencyRateSourceKey) != sourceID {
		err = s.repo.ReplaceRates("EUR", ratesToSave)
	} else {
		err = s.repo.SaveRates(ratesToSave)
	}
	if err != nil {
		return fmt.Errorf("failed to cache exchange rates: %w", err)
	}
	if err := s.settingsRepo.Set(currencyRateSourceKey, sourceID); err != nil {
		log.Printf("Warning: failed to record exchange-rate source: %v", err)
	}

	return nil
}

// snapshotTime returns when the snapshot's rates were published: the Unix
// timestamp, or else the date at midnight UTC. A snapshot with neither, or one
// dated in the future, is rejected because rates are ranked by this date.
func snapshotTime(resp FixerResponse, now time.Time) (time.Time, error) {
	var at time.Time
	switch {
	case resp.Timestamp > 0:
		at = time.Unix(resp.Timestamp, 0)
	case resp.Date != "":
		parsed, err := time.Parse("2006-01-02", resp.Date)
		if err != nil {
			return time.Time{}, fmt.Errorf("exchange-rate source returned an unreadable date %q", truncateForLog(resp.Date, 32))
		}
		at = parsed
	default:
		return time.Time{}, errors.New("exchange-rate source returned no timestamp or date")
	}
	if at.After(now.Add(maxRateClockSkew)) {
		return time.Time{}, fmt.Errorf("exchange-rate source returned a timestamp in the future (%s); timestamp must be in Unix seconds", at.UTC().Format(time.RFC3339))
	}
	return at, nil
}

// redact removes credentials from text that may echo the request: the API key
// and, from the configured endpoint, its query values, user info, full path,
// and any long path segment (a token embedded in the path, such as
// /v1/<token>/latest). Short segments like "api" or "latest" are kept.
func (s *CurrencyService) redact(text string) string {
	secrets := []string{s.apiKey}
	if parsed, err := url.Parse(s.endpoint); err == nil {
		for _, values := range parsed.Query() {
			secrets = append(secrets, values...)
		}
		if s.usesCustomEndpoint() && parsed.Path != "" && parsed.Path != "/" {
			secrets = append(secrets, parsed.Path)
			for _, segment := range strings.Split(parsed.Path, "/") {
				if len(segment) >= 12 {
					secrets = append(secrets, segment)
				}
			}
		}
		if parsed.User != nil {
			secrets = append(secrets, parsed.User.Username())
			if password, ok := parsed.User.Password(); ok {
				secrets = append(secrets, password)
			}
		}
	}
	for _, secret := range secrets {
		// Very short values would redact ordinary words; real keys are long.
		if len(secret) < 4 {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, form, "[redacted]")
		}
	}
	return text
}

// plausibleRate reports whether an EUR rate is usable: finite and within the
// plausibility band. It is checked when rates are saved and again when they
// are read, so rows cached by an earlier version cannot produce +Inf or NaN.
func plausibleRate(rate float64) bool {
	return !math.IsNaN(rate) && rate >= minPlausibleRate && rate <= maxPlausibleRate
}

func isSupportedCurrency(code string) bool {
	for _, supported := range SupportedCurrencies {
		if code == supported {
			return true
		}
	}
	return false
}

func (s *CurrencyService) refreshAllowed() (bool, error) {
	lastAttemptKey, failuresKey := s.refreshKeys()
	lastAttemptValue, err := s.settingsRepo.Get(lastAttemptKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	lastAttemptAt, err := time.Parse(time.RFC3339Nano, lastAttemptValue)
	if err != nil {
		log.Printf("Warning: invalid currency refresh timestamp; allowing a new attempt: %v", err)
		return true, nil
	}
	// Permit one earlier retry after a failed request. Subsequent failures use
	// the normal daily cooldown to stay within Fixer's free monthly quota.
	cooldown := 24 * time.Hour
	failures, err := s.settingsRepo.Get(failuresKey)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	if failures == "1" {
		cooldown = time.Hour
	}
	return !time.Now().Before(lastAttemptAt.Add(cooldown)), nil
}

func (s *CurrencyService) recordRefreshFailure() {
	_, failuresKey := s.refreshKeys()
	failures, err := s.settingsRepo.Get(failuresKey)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("Warning: failed to read currency refresh failure count: %v", err)
		return
	}
	count := "1"
	if failures == "1" || failures == "2" {
		count = "2"
	}
	if err := s.settingsRepo.Set(failuresKey, count); err != nil {
		log.Printf("Warning: failed to record currency refresh failure: %v", err)
	}
}

// refreshRates refreshes the cached snapshot, waiting for any refresh already
// in progress.
func (s *CurrencyService) refreshRates(ignoreCooldown bool) error {
	// Serialize refreshes within the service; the persisted attempt timestamp then
	// prevents later service instances from immediately repeating the request.
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshLocked(ignoreCooldown)
}

func (s *CurrencyService) refreshLocked(ignoreCooldown bool) error {
	// Explicit administrative refreshes retain the existing ability to bypass
	// automatic refresh timing. Each source has its own cooldown (see
	// refreshKeys), so a newly configured source refreshes at once.
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
		s.recordRefreshFailure()
		log.Printf("Warning: failed to refresh exchange rates: %v", err)
		return err
	}
	_, failuresKey := s.refreshKeys()
	if err := s.settingsRepo.Set(failuresKey, "0"); err != nil {
		log.Printf("Warning: failed to reset currency refresh failure count: %v", err)
	}
	return nil
}

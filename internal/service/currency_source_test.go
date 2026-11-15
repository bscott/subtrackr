package service

import (
	"crypto/tls"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCustomSourceServer serves body to any request, as a keyless
// Fixer-compatible rate source would.
func newCustomSourceServer(t *testing.T, body string, requestCount *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write rate response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func newCustomSourceService(t *testing.T, endpoint string) (*CurrencyService, *repository.ExchangeRateRepository) {
	t.Helper()
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", endpoint)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	return NewCurrencyService(repo, repository.NewSettingsRepository(db)), repo
}

func TestRateEndpointFromEnv(t *testing.T) {
	tests := []struct {
		name   string
		value  string
		want   string
		wantOK bool
	}{
		{name: "unset keeps Fixer", value: "", want: fixerURL, wantOK: true},
		{name: "https URL", value: "https://rates.example.com/api/latest", want: "https://rates.example.com/api/latest", wantOK: true},
		{name: "http URL with port, trimmed", value: "  http://10.0.0.5:8080/latest \n", want: "http://10.0.0.5:8080/latest", wantOK: true},
		{name: "missing scheme", value: "rates.example.com/api/latest"},
		{name: "unsupported scheme", value: "ftp://rates.example.com/latest"},
		{name: "missing host", value: "https:///api/latest"},
		{name: "unparseable", value: "https://%zz/latest"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FIXER_API_URL", tt.value)
			got, ok := rateEndpointFromEnv("")
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRateEndpointFromEnv_WarnsWithoutLeakingURL(t *testing.T) {
	logs := captureCurrencyLogs(t)

	t.Setenv("FIXER_API_URL", "rates.example.com/latest?access_key=secret-in-url")
	rateEndpointFromEnv("")
	assert.Contains(t, logs.String(), "FIXER_API_URL must be an absolute http(s) URL")
	assert.NotContains(t, logs.String(), "secret-in-url")

	// Over http, any credential travels in the clear: the key, a query
	// string or user info. A plain http URL with none of them is fine.
	for _, tc := range []struct{ url, key string }{
		{"http://rates.example.com/latest", "test-key"},
		{"http://rates.example.com/latest?apikey=query-secret", ""},
		{"http://reader:pass-secret@rates.example.com/latest", ""},
	} {
		logs.Reset()
		t.Setenv("FIXER_API_URL", tc.url)
		rateEndpointFromEnv(tc.key)
		assert.Contains(t, logs.String(), "credentials are sent to rates.example.com unencrypted", tc.url)
		for _, secret := range []string{"test-key", "query-secret", "pass-secret"} {
			assert.NotContains(t, logs.String(), secret)
		}
	}
	logs.Reset()
	t.Setenv("FIXER_API_URL", "http://rates.example.com/latest")
	rateEndpointFromEnv("")
	assert.NotContains(t, logs.String(), "unencrypted")
}

// An unusable FIXER_API_URL fails closed: conversion is off and a key meant
// for the operator's own source is never sent to Fixer.io (or anywhere).
func TestCurrencyService_InvalidCustomEndpointFailsClosed(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "private-proxy-key")
	t.Setenv("FIXER_API_URL", "rates.internal:8080/api/latest")
	db := setupTestDB(t)
	var outbound atomic.Int32
	service := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))
	service.client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		outbound.Add(1)
		return nil, fmt.Errorf("unexpected request to %s", r.URL.Host)
	})}

	assert.True(t, service.SourceMisconfigured())
	assert.False(t, service.IsEnabled())
	assert.Empty(t, service.apiKey, "the key is dropped, not kept for a fallback")
	_, err := service.ConvertAmount(3, "USD", "SEK")
	assert.Error(t, err)
	assert.Equal(t, int32(0), outbound.Load())
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCurrencyService_RateSource(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", "")
	db := setupTestDB(t)
	fixer := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))
	assert.Equal(t, RateSource{Name: "Fixer.io", URL: "https://fixer.io"}, fixer.RateSource())

	// Credentials in the user info, path or query must never be exposed.
	custom, _ := newCustomSourceService(t, "https://user:hunter2@rates.example.com:8443/private/latest?access_key=k3y")
	source := custom.RateSource()
	assert.Equal(t, RateSource{Name: "rates.example.com", URL: "https://rates.example.com:8443"}, source)
	for _, secret := range []string{"user", "hunter2", "private", "k3y"} {
		assert.NotContains(t, source.Name+source.URL, secret)
	}
}

func TestCurrencyService_RejectsResponseWithNonEURBase(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"USD","rates":{"EUR":0.9,"SEK":11}}`, now.Unix())
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, repo := newCustomSourceService(t, server.URL)
	logs := captureCurrencyLogs(t)

	_, err := service.ConvertAmount(3, "USD", "SEK")

	require.Error(t, err)
	assert.Contains(t, err.Error(), `returned base "USD", want EUR`)
	assert.Contains(t, logs.String(), `returned base "USD"`)
	assert.Equal(t, int32(1), requestCount.Load())
	_, cacheErr := repo.GetRate("EUR", "SEK")
	assert.Error(t, cacheErr, "rates from a non-EUR snapshot must not be cached as EUR legs")
}

func TestCurrencyService_AcceptsLowercaseEURBase(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"eur","rates":{"USD":1.2,"SEK":12}}`, now.Unix())
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, _ := newCustomSourceService(t, server.URL)

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
}

// A once-per-business-day source still serves Friday's rates on Monday
// morning; they must not be shown as out of date, and the refresh cooldown
// must still hold the request rate to one a day.
func TestCurrencyService_CustomSourceDoesNotFlagWeekendRatesStale(t *testing.T) {
	friday := time.Now().UTC().Add(-66 * time.Hour).Truncate(time.Second)
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(friday), &requestCount)
	service, _ := newCustomSourceService(t, server.URL)

	first, err := service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)
	second, err := service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)

	assert.Equal(t, 30.0, first.Amount)
	assert.False(t, first.Stale)
	assert.False(t, second.Stale)
	assert.True(t, second.RateDate.Equal(friday))
	assert.Equal(t, int32(1), requestCount.Load(), "the cooldown still limits refreshes to one a day")
}

func TestCurrencyService_CustomSourceFlagsRatesStaleAfterWindow(t *testing.T) {
	old := time.Now().UTC().Add(-(customSourceStaleAfter + time.Hour)).Truncate(time.Second)
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(old), &requestCount)
	service, _ := newCustomSourceService(t, server.URL)

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.True(t, conversion.Stale)
}

// The longer stale window must not delay refreshes: a day-old cached rate
// from a custom source still triggers the daily refresh.
func TestCurrencyService_CustomSourceRefreshesDayOldCachedRates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(now), &requestCount)
	service, repo := newCustomSourceService(t, server.URL)
	dayOld := now.Add(-30 * time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.1, Date: dayOld},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 11, Date: dayOld},
	)

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load())
	assert.Equal(t, 30.0, conversion.Amount, "the refreshed snapshot is used")
	assert.True(t, conversion.RateDate.Equal(now))
}

// Fixer.io keeps the original 24-hour window.
func TestCurrencyService_FixerFlagsDayOldRatesStale(t *testing.T) {
	dayOld := time.Now().UTC().Add(-30 * time.Hour).Truncate(time.Second)
	var requestCount atomic.Int32
	server := newFixerTestServer(t, fixerSuccessBody(dayOld), &requestCount)
	defer server.Close()
	db := setupTestDB(t)
	service := newTestCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db), server)

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.True(t, conversion.Stale)
}

func TestCurrencyService_FetchErrorNamesHostButNotAPIKey(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	endpoint := server.URL
	host := server.Listener.Addr().String()
	server.Close() // refuse connections so the request itself fails
	db := setupTestDB(t)
	service := createCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db), http.DefaultClient, endpoint, "secret-key-123")
	logs := captureCurrencyLogs(t)

	err := service.refreshRates(true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch exchange rates from "+host)
	assert.NotContains(t, err.Error(), "secret-key-123")
	assert.NotContains(t, err.Error(), "access_key")
	assert.NotContains(t, logs.String(), "secret-key-123")
}

// --- Review fixes: redirects, redaction, response validation, source changes ---

func newHandlerServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestCurrencyService_DoesNotFollowRedirectsOrLeakKeyInReferer(t *testing.T) {
	var targetHits atomic.Int32
	var leakedReferer atomic.Value
	target := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		targetHits.Add(1)
		leakedReferer.Store(r.Header.Get("Referer"))
		_, _ = w.Write([]byte(fixerSuccessBody(time.Now())))
	})
	source := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/latest", http.StatusFound)
	})
	t.Setenv("FIXER_API_KEY", "secret-key-123")
	t.Setenv("FIXER_API_URL", source.URL)
	db := setupTestDB(t)
	service := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))

	err := service.refreshRates(true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP status 302")
	assert.Contains(t, err.Error(), "redirects are not followed")
	assert.Equal(t, int32(0), targetHits.Load(), "the redirect target must not be contacted; got Referer %v", leakedReferer.Load())
}

func TestCurrencyService_RedactsKeyFromMalformedRedirectError(t *testing.T) {
	// Go parses Location before consulting CheckRedirect, and quotes it in the
	// error when it is malformed, so the source can echo the key back to us.
	source := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/%zz?access_key="+r.URL.Query().Get("access_key"))
		w.WriteHeader(http.StatusFound)
	})
	t.Setenv("FIXER_API_KEY", "secret-key-123")
	t.Setenv("FIXER_API_URL", source.URL)
	db := setupTestDB(t)
	service := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))
	logs := captureCurrencyLogs(t)

	err := service.refreshRates(true)

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret-key-123")
	assert.Contains(t, err.Error(), "[redacted]")
	assert.NotContains(t, logs.String(), "secret-key-123")
}

func TestCurrencyService_RedactsCredentialsEchoedInSourceErrorInfo(t *testing.T) {
	tests := []struct {
		name     string
		endpoint func(base string) string
		apiKey   string
		secret   string
	}{
		{name: "API key", endpoint: func(base string) string { return base }, apiKey: "secret-key-123", secret: "secret-key-123"},
		{name: "token in FIXER_API_URL query", endpoint: func(base string) string { return base + "/latest?token=url-secret-999" }, secret: "url-secret-999"},
		{name: "password in FIXER_API_URL user info", endpoint: func(base string) string {
			return strings.Replace(base, "://", "://reader:pass-secret-777@", 1)
		}, secret: "pass-secret-777"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
				_, _ = fmt.Fprintf(w, `{"success":false,"error":{"code":101,"info":"invalid credential %s\nFORGED log line"}}`, tt.secret)
			})
			t.Setenv("FIXER_API_KEY", tt.apiKey)
			t.Setenv("FIXER_API_URL", tt.endpoint(server.URL))
			db := setupTestDB(t)
			service := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))
			logs := captureCurrencyLogs(t)

			err := service.refreshRates(true)

			require.Error(t, err)
			assert.NotContains(t, err.Error(), tt.secret)
			assert.Contains(t, err.Error(), "[redacted]")
			assert.Contains(t, err.Error(), `\nFORGED`, "remote text is quoted, so it cannot forge log lines")
			assert.NotContains(t, logs.String(), tt.secret)
		})
	}
}

func TestCurrencyService_TruncatesLongSourceErrorInfo(t *testing.T) {
	server := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, `{"success":false,"error":{"info":"%s"}}`, strings.Repeat("x", 5000))
	})
	service, _ := newCustomSourceService(t, server.URL)

	err := service.refreshRates(true)

	require.Error(t, err)
	assert.Less(t, len(err.Error()), 400)
}

func TestCurrencyService_RejectsSnapshotWithoutEURBase(t *testing.T) {
	now := time.Now().Unix()
	tests := map[string]string{
		"missing base": fmt.Sprintf(`{"success":true,"timestamp":%d,"rates":{"EUR":0.9,"SEK":11}}`, now),
		"empty base":   fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"","rates":{"SEK":11}}`, now),
		"null base":    fmt.Sprintf(`{"success":true,"timestamp":%d,"base":null,"rates":{"SEK":11}}`, now),
		"USD base":     fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"USD","rates":{"SEK":11}}`, now),
		"EUR not 1":    fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{"EUR":0.9,"SEK":11}}`, now),
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			var requestCount atomic.Int32
			server := newCustomSourceServer(t, body, &requestCount)
			service, repo := newCustomSourceService(t, server.URL)

			err := service.refreshRates(true)

			require.Error(t, err)
			_, cacheErr := repo.GetRate("EUR", "SEK")
			assert.Error(t, cacheErr, "nothing may be cached from a snapshot that is not EUR-based")
		})
	}
}

func TestSnapshotTime(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		resp    FixerResponse
		want    time.Time
		wantErr string
	}{
		{name: "unix seconds", resp: FixerResponse{Timestamp: now.Add(-time.Hour).Unix()}, want: now.Add(-time.Hour)},
		{name: "timestamp wins over date", resp: FixerResponse{Timestamp: now.Add(-time.Hour).Unix(), Date: "2020-01-01"}, want: now.Add(-time.Hour)},
		{name: "date only", resp: FixerResponse{Date: "2026-11-13"}, want: time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)},
		{name: "small clock skew allowed", resp: FixerResponse{Timestamp: now.Add(30 * time.Minute).Unix()}, want: now.Add(30 * time.Minute)},
		{name: "neither", resp: FixerResponse{}, wantErr: "no timestamp or date"},
		{name: "unreadable date", resp: FixerResponse{Date: "13/11/2026"}, wantErr: "unreadable date"},
		{name: "future", resp: FixerResponse{Timestamp: now.Add(2 * time.Hour).Unix()}, wantErr: "in the future"},
		{name: "milliseconds", resp: FixerResponse{Timestamp: now.UnixMilli()}, wantErr: "Unix seconds"},
		{name: "future date", resp: FixerResponse{Date: "2026-11-18"}, wantErr: "in the future"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := snapshotTime(tt.resp, now)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.True(t, got.Equal(tt.want), "got %s, want %s", got, tt.want)
		})
	}
}

// A future-dated snapshot would outrank every later one (rates are ranked by
// date), so it must never reach the cache.
func TestCurrencyService_MillisecondTimestampDoesNotPoisonCache(t *testing.T) {
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{"USD":1,"SEK":30}}`, time.Now().UnixMilli())
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, repo := newCustomSourceService(t, server.URL)

	require.Error(t, service.refreshRates(true))
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: time.Now()},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: time.Now()},
	)
	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
}

func TestCurrencyService_UsesDateWhenTimestampMissing(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	body := fmt.Sprintf(`{"success":true,"base":"EUR","date":%q,"rates":{"USD":1.2,"SEK":12}}`, today)
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, _ := newCustomSourceService(t, server.URL)

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
	assert.Equal(t, today, conversion.RateDate.UTC().Format("2006-01-02"))
	assert.False(t, conversion.Stale)
}

func TestCurrencyService_RejectsOversizedResponse(t *testing.T) {
	padding := strings.Repeat("a", maxRateResponseBytes+1)
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","pad":"%s","rates":{"SEK":12}}`, time.Now().Unix(), padding)
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, repo := newCustomSourceService(t, server.URL)

	err := service.refreshRates(true)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode")
	_, cacheErr := repo.GetRate("EUR", "SEK")
	assert.Error(t, cacheErr)
}

func TestCurrencyService_CachesOnlySupportedCurrenciesWithPositiveRates(t *testing.T) {
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{"USD":1.2,"XAU":0.0005,"SEK":-1,"GBP":0,"INJECTED_CODE_THAT_IS_LONG":3}}`, time.Now().Unix())
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, repo := newCustomSourceService(t, server.URL)

	require.NoError(t, service.refreshRates(true))

	_, err := repo.GetRate("EUR", "USD")
	assert.NoError(t, err)
	for _, code := range []string{"XAU", "SEK", "GBP", "INJECTED_CODE_THAT_IS_LONG"} {
		_, err := repo.GetRate("EUR", code)
		assert.Error(t, err, "%s must not be cached", code)
	}
}

func TestIsFixerEndpoint(t *testing.T) {
	for _, endpoint := range []string{fixerURL, "http://data.fixer.io/api/latest", "HTTPS://DATA.FIXER.IO/api/latest/", "https://data.fixer.io:443/api/latest"} {
		assert.True(t, isFixerEndpoint(endpoint), endpoint)
	}
	for _, endpoint := range []string{"https://rates.example.com/latest", "https://data.fixer.io.example.com/latest", "https://example.com/data.fixer.io"} {
		assert.False(t, isFixerEndpoint(endpoint), endpoint)
	}
}

func TestCurrencyService_FixerSpelledAsCustomURLKeepsFixerRules(t *testing.T) {
	service, _ := newCustomSourceService(t, "http://DATA.fixer.io/api/latest/")

	assert.False(t, service.IsEnabled(), "Fixer.io still needs FIXER_API_KEY")
	assert.Equal(t, rateRefreshAfter, service.staleAfter())
	assert.Equal(t, RateSource{Name: "Fixer.io", URL: "https://fixer.io"}, service.RateSource())
}

// seedLegacyFixerCache stores rates as an installation from before sources
// were recorded would have them, with the daily cooldown active.
func seedLegacyFixerCache(t *testing.T, service *CurrencyService, repo *repository.ExchangeRateRepository, dated time.Time) {
	t.Helper()
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1, Date: dated},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 20, Date: dated},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "BDT", Rate: 130, Date: dated},
	)
	require.NoError(t, service.settingsRepo.Set(currencyRefreshLastAttemptKey, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)))
	require.NoError(t, service.settingsRepo.Set(currencyRefreshFailuresKey, "0"))
}

// Switching from Fixer.io to a daily source: the old Fixer rows are newer
// than the new source's snapshot, and the Fixer cooldown is active. The new
// source must still be used at once, and its rates must replace Fixer's.
func TestCurrencyService_SourceChangeRefreshesAtOnceAndReplacesOldRates(t *testing.T) {
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(time.Now().Add(-20*time.Hour)), &requestCount)
	service, repo := newCustomSourceService(t, server.URL)
	seedLegacyFixerCache(t, service, repo, time.Now().Add(-2*time.Hour))

	conversion, err := service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount, "the new source's rates, not Fixer's (60)")
	assert.Equal(t, service.RateSource(), conversion.Source)
	assert.Equal(t, int32(1), requestCount.Load())

	_, err = service.ConvertAmount(3, "USD", "BDT")
	assert.Error(t, err, "a currency the new source omits must not fall back to the old source's rate")

	_, err = service.ConvertAmount(5, "USD", "SEK")
	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load(), "after the switch the normal cooldown applies")
}

func TestCurrencyService_FailedSourceChangeKeepsOldRatesAndAttribution(t *testing.T) {
	var requestCount atomic.Int32
	server := newHandlerServer(t, func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	service, repo := newCustomSourceService(t, server.URL)
	seedLegacyFixerCache(t, service, repo, time.Now().Add(-2*time.Hour))
	logs := captureCurrencyLogs(t)

	first, err := service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)
	second, err := service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)

	assert.Equal(t, 60.0, first.Amount, "the old rates are still used")
	assert.Equal(t, RateSource{Name: "Fixer.io", URL: "https://fixer.io"}, first.Source, "and still attributed to Fixer.io")
	assert.Equal(t, first, second)
	assert.Equal(t, int32(1), requestCount.Load(), "a failing new source is not retried on every request")
	assert.Contains(t, logs.String(), "HTTP status 500")
}

func TestCurrencyService_DisabledSourceStillAttributesCachedRates(t *testing.T) {
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(time.Now()), &requestCount)
	custom, repo := newCustomSourceService(t, server.URL)
	_, err := custom.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err)

	// Remove FIXER_API_URL and run without a key: conversion keeps using the
	// cached rates and names where they came from.
	t.Setenv("FIXER_API_URL", "")
	disabled := NewCurrencyService(repo, custom.settingsRepo)
	conversion, err := disabled.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.False(t, disabled.IsEnabled())
	assert.Equal(t, custom.RateSource(), conversion.Source)
	assert.Equal(t, int32(1), requestCount.Load())
}

func TestCurrencyService_SourceIDOmitsCredentials(t *testing.T) {
	service, _ := newCustomSourceService(t, "https://user:hunter2@Rates.Example.com:8443/api/latest?access_key=k3y-value")

	id := service.sourceID()

	assert.Regexp(t, `^https://rates\.example\.com:8443/sha256-[0-9a-f]{16}$`, id)
	for _, secret := range []string{"user", "hunter2", "api", "latest", "k3y-value"} {
		assert.NotContains(t, id, secret, "the stored identity must not contain the path, query or user info")
	}
	assert.Equal(t, RateSource{Name: "rates.example.com", URL: "https://rates.example.com:8443"}, rateSourceFor(id))
}

// --- Security review fixes ---

// Extreme but positive rates overflow to +Inf in 1/rate or cross-rates, which
// then breaks JSON encoding of stats and API responses.
func TestCurrencyService_SkipsImplausibleRatesFromSource(t *testing.T) {
	body := fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{"USD":5e-324,"GBP":1e308,"JPY":1e-7,"SEK":11,"VND":28000}}`, time.Now().Unix())
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, body, &requestCount)
	service, repo := newCustomSourceService(t, server.URL)

	require.NoError(t, service.refreshRates(true))

	for _, code := range []string{"USD", "GBP", "JPY"} {
		_, err := repo.GetRate("EUR", code)
		assert.Error(t, err, "%s must not be cached", code)
	}
	for _, code := range []string{"SEK", "VND"} {
		_, err := repo.GetRate("EUR", code)
		assert.NoError(t, err, "%s is a real rate and must be cached", code)
	}
}

// Rows cached before the plausibility band existed must not produce +Inf.
func TestCurrencyService_RefusesImplausibleCachedRates(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 5e-324, Date: time.Now()},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "GBP", Rate: 1e308, Date: time.Now()},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 11, Date: time.Now()},
	)

	for _, pair := range [][2]string{{"USD", "EUR"}, {"USD", "SEK"}, {"EUR", "GBP"}, {"SEK", "GBP"}} {
		conversion, err := service.ConvertAmount(1, pair[0], pair[1])
		assert.Error(t, err, "%s->%s gave %v", pair[0], pair[1], conversion.Amount)
	}
}

func TestCurrencyService_ConvertAmountNeverReturnsNonFinite(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	service := NewCurrencyService(repo, repository.NewSettingsRepository(db))
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: minPlausibleRate, Date: time.Now()},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: maxPlausibleRate, Date: time.Now()},
	)

	conversion, err := service.ConvertAmount(math.MaxFloat64, "USD", "SEK")
	assert.Error(t, err, "got %v", conversion.Amount)

	conversion, err = service.ConvertAmount(3, "USD", "SEK")
	require.NoError(t, err, "the band's extremes still convert ordinary amounts")
	assert.False(t, math.IsInf(conversion.Amount, 0))
}

// Two processes sharing one database but configured with different sources
// (say the web server with FIXER_API_URL and the MCP server without) must not
// reset each other's cooldown on every alternation.
func TestCurrencyService_DifferentSourcesSharingADatabaseKeepTheirOwnCooldown(t *testing.T) {
	var customRequests, fixerRequests atomic.Int32
	custom := newCustomSourceServer(t, fixerSuccessBody(time.Now()), &customRequests)
	fixer := newFixerTestServer(t, fixerSuccessBody(time.Now()), &fixerRequests)
	defer fixer.Close()
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", custom.URL)
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	web := NewCurrencyService(repo, settingsRepo)
	mcp := newTestCurrencyService(repo, settingsRepo, fixer)

	for range 50 {
		_, err := web.ConvertAmount(3, "USD", "SEK")
		require.NoError(t, err)
		_, err = mcp.ConvertAmount(3, "USD", "SEK")
		require.NoError(t, err)
	}

	assert.Equal(t, int32(1), customRequests.Load(), "the custom source is asked once a day at most")
	assert.Equal(t, int32(1), fixerRequests.Load(), "and so is Fixer.io")
}

func TestCurrencyService_CustomSourceCooldownIsSeparateFromFixer(t *testing.T) {
	var requestCount atomic.Int32
	server := newCustomSourceServer(t, fixerSuccessBody(time.Now()), &requestCount)
	service, _ := newCustomSourceService(t, server.URL)
	// A Fixer.io attempt a minute ago must not block the new source...
	require.NoError(t, service.settingsRepo.Set(currencyRefreshLastAttemptKey, time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)))

	allowed, err := service.refreshAllowed()
	require.NoError(t, err)
	assert.True(t, allowed)

	// ...and the new source's own attempt does block it, under its own key.
	require.NoError(t, service.refreshRates(true))
	allowed, err = service.refreshAllowed()
	require.NoError(t, err)
	assert.False(t, allowed)
	lastAttemptKey, failuresKey := service.refreshKeys()
	assert.NotEqual(t, currencyRefreshLastAttemptKey, lastAttemptKey)
	assert.NotEqual(t, currencyRefreshFailuresKey, failuresKey)
	assert.LessOrEqual(t, len(lastAttemptKey), 64)
}

func TestNewRateHTTPClient_HonoursProxyEnvironmentAndTLS12(t *testing.T) {
	transport, ok := newRateHTTPClient().Transport.(*http.Transport)
	require.True(t, ok)
	assert.NotNil(t, transport.Proxy, "HTTPS_PROXY/NO_PROXY must be honoured")
	assert.GreaterOrEqual(t, transport.TLSClientConfig.MinVersion, uint16(tls.VersionTLS12))
	assert.False(t, transport.TLSClientConfig.InsecureSkipVerify)
}

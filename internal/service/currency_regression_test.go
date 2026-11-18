package service

// Regression tests for the exchange-rate refresh as a whole: response
// fixtures, invariants under arbitrary responses (fuzzed), cross-rate
// consistency, a sweep for leaked credentials, the configuration matrix, and
// compatibility with installations upgraded from v0.7.0.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const regressionEndpoint = "https://rates.example.com/latest"

// stubClient answers every request with respond, without any network.
func stubClient(respond func(*http.Request) (*http.Response, error)) *http.Client {
	client := newRateHTTPClient()
	client.Transport = roundTripFunc(respond)
	return client
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// newStubbedCustomService builds a service for a keyless custom source whose
// responses come from respond.
func newStubbedCustomService(t testing.TB, db *gorm.DB, respond func(*http.Request) (*http.Response, error)) *CurrencyService {
	t.Helper()
	service := createCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db), stubClient(respond), regressionEndpoint, "")
	service.customSource = true
	return service
}

func loadRateFixture(t testing.TB, name string, at time.Time) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "rates", name))
	require.NoError(t, err)
	return strings.NewReplacer(
		"{{TIMESTAMP_MS}}", fmt.Sprint(at.UnixMilli()),
		"{{TIMESTAMP}}", fmt.Sprint(at.Unix()),
		"{{DATE}}", at.UTC().Format("2006-01-02"),
	).Replace(string(raw))
}

// --- Fixtures ---------------------------------------------------------------

func TestRateSourceResponseFixtures(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	tests := []struct {
		fixture string
		status  int
		wantErr string // empty: refresh must succeed
		// After a successful refresh: USD->SEK must convert to wantUSDSEK
		// (0 = must not convert), and these currencies must not be cached.
		wantUSDSEK float64
		notCached  []string
	}{
		{fixture: "fixer_success.json", wantUSDSEK: 11.1075 / 1.1052},
		{fixture: "ecb_mirror_success.json", wantUSDSEK: 11.1075 / 1.1052, notCached: []string{"BDT", "VND", "JPY"}},
		{fixture: "date_only_success.json", wantUSDSEK: 11.1075 / 1.1052},
		{fixture: "empty_rates.json", notCached: []string{"USD", "SEK"}},
		{fixture: "fixer_error_invalid_key.json", wantErr: "You have not supplied a valid API Access Key"},
		{fixture: "fixer_error_quota.json", wantErr: "monthly usage limit"},
		{fixture: "fixer_error_base_restricted.json", wantErr: "Fixer API error"},
		{fixture: "usd_base.json", wantErr: `base "USD"`},
		{fixture: "missing_base.json", wantErr: `base ""`},
		{fixture: "milliseconds_timestamp.json", wantErr: "Unix seconds"},
		{fixture: "no_timestamp_or_date.json", wantErr: "no timestamp or date"},
		{fixture: "rates_wrong_type.json", wantErr: "failed to decode"},
		{fixture: "number_overflow.json", wantErr: "failed to decode"},
		{fixture: "html_error_page.json", status: http.StatusBadGateway, wantErr: "HTTP status 502"},
		{fixture: "html_error_page.json", wantErr: "failed to decode"},
	}
	for _, tt := range tests {
		name := strings.TrimSuffix(tt.fixture, ".json")
		if tt.status != 0 {
			name += fmt.Sprintf("_status_%d", tt.status)
		}
		t.Run(name, func(t *testing.T) {
			body := loadRateFixture(t, tt.fixture, now)
			status := tt.status
			if status == 0 {
				status = http.StatusOK
			}
			db := setupTestDB(t)
			service := newStubbedCustomService(t, db, func(*http.Request) (*http.Response, error) {
				return jsonResponse(status, body), nil
			})
			captureCurrencyLogs(t)

			err := service.refreshRates(true)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				var count int64
				require.NoError(t, db.Model(&models.ExchangeRate{}).Count(&count).Error)
				assert.Zero(t, count, "a rejected response must cache nothing")
				return
			}
			require.NoError(t, err)
			conversion, convErr := service.ConvertAmount(1, "USD", "SEK")
			if tt.wantUSDSEK == 0 {
				assert.Error(t, convErr)
			} else {
				require.NoError(t, convErr)
				assert.InDelta(t, tt.wantUSDSEK, conversion.Amount, 1e-9)
				assert.False(t, conversion.Stale)
				assert.Equal(t, RateSource{Name: "rates.example.com", URL: "https://rates.example.com"}, conversion.Source)
			}
			for _, code := range tt.notCached {
				_, err := service.repo.GetRate("EUR", code)
				assert.Error(t, err, "%s must not be cached", code)
			}
		})
	}
}

// --- Invariants under arbitrary responses ----------------------------------

// FuzzRateSourceResponse feeds arbitrary bodies to the refresh. Run with
// `go test -fuzz=FuzzRateSourceResponse ./internal/service/`; plain `go test`
// runs the seed corpus (the fixtures) as a regression test.
func FuzzRateSourceResponse(f *testing.F) {
	now := time.Now().UTC().Add(-time.Minute)
	entries, err := os.ReadDir(filepath.Join("testdata", "rates"))
	require.NoError(f, err)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			f.Add([]byte(loadRateFixture(f, entry.Name(), now)))
		}
	}
	f.Add([]byte(fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{"USD":5e-324,"SEK":1e308,"GBP":-1,"JPY":0}}`, now.Unix())))
	f.Add([]byte(fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"eur","rates":{"USD":1.1,"USD":2.2,"EUR":1}}`, now.Unix())))

	f.Fuzz(func(t *testing.T, body []byte) {
		db := setupTestDB(t)
		service := newStubbedCustomService(t, db, func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, string(body)), nil
		})
		captureCurrencyLogs(t)

		refreshErr := service.refreshRates(true) // must not panic

		var rows []models.ExchangeRate
		require.NoError(t, db.Find(&rows).Error)
		if refreshErr != nil {
			assert.Empty(t, rows, "a failed refresh must cache nothing")
		}
		for _, row := range rows {
			assert.Equal(t, "EUR", row.BaseCurrency)
			assert.True(t, row.Currency == "EUR" || isSupportedCurrency(row.Currency), "unsupported currency %q cached", row.Currency)
			assert.True(t, plausibleRate(row.Rate), "implausible rate %v cached for %s", row.Rate, row.Currency)
			assert.False(t, row.Date.After(time.Now().Add(maxRateClockSkew)), "future-dated row cached")
		}
		for _, from := range []string{"EUR", "USD", "SEK", "VND"} {
			for _, to := range []string{"EUR", "GBP", "JPY", "VND"} {
				conversion, err := service.ConvertAmount(123.45, from, to)
				if err != nil {
					continue
				}
				assert.False(t, math.IsInf(conversion.Amount, 0) || math.IsNaN(conversion.Amount), "%s->%s = %v", from, to, conversion.Amount)
				assert.Greater(t, conversion.Amount, 0.0, "%s->%s", from, to)
			}
		}
	})
}

// --- Cross-rate consistency -------------------------------------------------

func TestCrossRatesAreConsistent(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "")
	t.Setenv("FIXER_API_URL", "")
	rng := rand.New(rand.NewSource(20261115))
	logUniform := func() float64 {
		lo, hi := math.Log(minPlausibleRate), math.Log(maxPlausibleRate)
		return math.Exp(lo + rng.Float64()*(hi-lo))
	}
	codes := []string{"USD", "GBP", "SEK", "JPY", "VND", "INR"}

	for round := range 25 {
		db := setupTestDB(t)
		repo := repository.NewExchangeRateRepository(db)
		service := NewCurrencyService(repo, repository.NewSettingsRepository(db))
		for _, code := range codes {
			saveEURRates(t, repo, models.ExchangeRate{BaseCurrency: "EUR", Currency: code, Rate: logUniform(), Date: time.Now()})
		}
		all := append([]string{"EUR"}, codes...)
		rate := func(from, to string) float64 {
			t.Helper()
			conversion, err := service.ConvertAmount(1, from, to)
			require.NoError(t, err, "round %d %s->%s", round, from, to)
			return conversion.Amount
		}
		for _, a := range all {
			for _, b := range all {
				assert.InEpsilon(t, 1, rate(a, b)*rate(b, a), 1e-9, "round %d: %s->%s->%s", round, a, b, a)
				for _, c := range all {
					assert.InEpsilon(t, rate(a, c), rate(a, b)*rate(b, c), 1e-9, "round %d: %s->%s->%s", round, a, b, c)
				}
			}
		}
	}
}

// --- No secret escapes ------------------------------------------------------

// Drives every failure mode with credentials in the key, the user info, the
// path and the query, and checks that none of them reach the logs, the
// returned errors, the settings table, or the cached rows.
func TestCredentialsNeverEscape(t *testing.T) {
	const (
		key       = "SWEEP-KEY-8f3a61"
		user      = "SWEEP-USER-2b"
		password  = "SWEEP-PASS-71c2"
		pathToken = "SWEEP-PATH-TOKEN-55aa"
		queryTok  = "SWEEP-QUERY-9d1e"
	)
	secrets := []string{key, user, password, pathToken, queryTok}
	endpoint := "https://" + user + ":" + password + "@rates.example.com/v1/" + pathToken + "/latest?token=" + queryTok
	echo := func(r *http.Request) string { return r.URL.String() + " " + r.URL.RawQuery + " " + key }
	now := time.Now().Unix()

	modes := map[string]func(*http.Request) (*http.Response, error){
		"transport error quoting the URL": func(r *http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("dial failed for %s", echo(r))
		},
		"malformed redirect quoting the URL": func(r *http.Request) (*http.Response, error) {
			resp := jsonResponse(http.StatusFound, "")
			resp.Header.Set("Location", "/%zz?"+echo(r))
			return resp, nil
		},
		"valid redirect": func(r *http.Request) (*http.Response, error) {
			resp := jsonResponse(http.StatusFound, "")
			resp.Header.Set("Location", "https://elsewhere.example.com/?"+r.URL.RawQuery)
			return resp, nil
		},
		"server error": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusInternalServerError, key), nil
		},
		"error info echoing the request": func(r *http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"success":false,"error":{"info":%q}}`, echo(r))), nil
		},
		"error info quoting only the path token": func(r *http.Request) (*http.Response, error) {
			segments := strings.Split(r.URL.Path, "/") // ["", "v1", "<token>", "latest"]
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"success":false,"error":{"info":"unknown token %s"}}`, segments[2])), nil
		},
		"base echoing the key": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"timestamp":%d,"base":%q,"rates":{}}`, now, key)), nil
		},
		"date echoing the key": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"base":"EUR","date":%q,"rates":{}}`, key)), nil
		},
		"invalid JSON echoing the key": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, "{"+key), nil
		},
		"rate codes echoing secrets": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fmt.Sprintf(`{"success":true,"timestamp":%d,"base":"EUR","rates":{%q:1.1,%q:2,"SEK":11}}`, now, key, pathToken)), nil
		},
		"success": func(*http.Request) (*http.Response, error) {
			return jsonResponse(http.StatusOK, fixerSuccessBody(time.Now())), nil
		},
	}
	for name, respond := range modes {
		t.Run(name, func(t *testing.T) {
			db := setupTestDB(t)
			service := createCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db), stubClient(respond), endpoint, key)
			service.customSource = true
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previous) })

			refreshErr := service.refreshRates(true)
			_, convertErr := service.ConvertAmount(1, "USD", "SEK")

			var settings []models.Settings
			require.NoError(t, db.Find(&settings).Error)
			var rows []models.ExchangeRate
			require.NoError(t, db.Find(&rows).Error)
			var stored strings.Builder
			for _, s := range settings {
				stored.WriteString(s.Key + "=" + s.Value + "\n")
			}
			for _, r := range rows {
				stored.WriteString(r.Currency + "\n")
			}
			source := service.RateSource()
			surfaces := map[string]string{
				"logs":        logs.String(),
				"refresh err": errString(refreshErr),
				"convert err": errString(convertErr),
				"settings":    stored.String(),
				"rate source": source.Name + " " + source.URL + " " + service.sourceID(),
			}
			for surface, text := range surfaces {
				for _, secret := range secrets {
					assert.NotContains(t, text, secret, "%s leaked into %s:\n%s", secret, surface, text)
				}
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// --- Configuration matrix ---------------------------------------------------

func TestCurrencyConfigurationMatrix(t *testing.T) {
	fixer := RateSource{Name: "Fixer.io", URL: "https://fixer.io"}
	custom := RateSource{Name: "rates.example.com", URL: "https://rates.example.com"}
	tests := []struct {
		name          string
		key, url      string
		enabled       bool
		misconfigured bool
		source        RateSource
		staleAfter    time.Duration
		wantHost      string // "" = no request may be made
		wantKeySent   bool
	}{
		{name: "nothing set", source: fixer, staleAfter: rateRefreshAfter},
		{name: "Fixer key", key: "k-123456", enabled: true, source: fixer, staleAfter: rateRefreshAfter, wantHost: "data.fixer.io", wantKeySent: true},
		{name: "keyless custom URL", url: regressionEndpoint, enabled: true, source: custom, staleAfter: customSourceStaleAfter, wantHost: "rates.example.com"},
		{name: "custom URL with key", key: "k-123456", url: regressionEndpoint, enabled: true, source: custom, staleAfter: customSourceStaleAfter, wantHost: "rates.example.com", wantKeySent: true},
		{name: "Fixer spelled as custom URL, no key", url: "http://data.fixer.io/api/latest", source: fixer, staleAfter: rateRefreshAfter},
		{name: "Fixer spelled as custom URL, key", key: "k-123456", url: "http://data.fixer.io/api/latest", enabled: true, source: fixer, staleAfter: rateRefreshAfter, wantHost: "data.fixer.io", wantKeySent: true},
		{name: "invalid URL, no key", url: "rates.example.com/latest", misconfigured: true, source: fixer, staleAfter: rateRefreshAfter},
		{name: "invalid URL with key fails closed", key: "k-123456", url: "rates.example.com/latest", misconfigured: true, source: fixer, staleAfter: rateRefreshAfter},
		{name: "whitespace URL is unset", key: "k-123456", url: "   ", enabled: true, source: fixer, staleAfter: rateRefreshAfter, wantHost: "data.fixer.io", wantKeySent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("FIXER_API_KEY", tt.key)
			t.Setenv("FIXER_API_URL", tt.url)
			db := setupTestDB(t)
			captureCurrencyLogs(t)
			service := NewCurrencyService(repository.NewExchangeRateRepository(db), repository.NewSettingsRepository(db))
			var requests []*http.Request
			service.client = stubClient(func(r *http.Request) (*http.Response, error) {
				requests = append(requests, r)
				return jsonResponse(http.StatusOK, fixerSuccessBody(time.Now())), nil
			})

			assert.Equal(t, tt.enabled, service.IsEnabled())
			assert.Equal(t, tt.misconfigured, service.SourceMisconfigured())
			assert.Equal(t, tt.source, service.RateSource())
			assert.Equal(t, tt.staleAfter, service.staleAfter())

			_, _ = service.ConvertAmount(1, "USD", "SEK")
			if tt.wantHost == "" {
				assert.Empty(t, requests, "no request may be made")
				return
			}
			require.Len(t, requests, 1)
			assert.Equal(t, tt.wantHost, requests[0].URL.Hostname())
			assert.Equal(t, tt.wantKeySent, requests[0].URL.Query().Has("access_key"))
			assert.Equal(t, "EUR", requests[0].URL.Query().Get("base"))
		})
	}
}

// --- Upgrade compatibility --------------------------------------------------

// A Fixer.io installation upgraded from v0.7.0 has rates and cooldown state
// but no recorded source. It must behave exactly as before: no extra request
// during the cooldown, the 24-hour stale window, Fixer.io attribution, and its
// cached rows kept.
func TestUpgradeFromV070FixerInstallationBehavesAsBefore(t *testing.T) {
	t.Setenv("FIXER_API_KEY", "existing-fixer-key")
	t.Setenv("FIXER_API_URL", "")
	db := setupTestDB(t)
	repo := repository.NewExchangeRateRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	rateDate := time.Now().Add(-25 * time.Hour)
	saveEURRates(t, repo,
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate},
		models.ExchangeRate{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate},
	)
	require.NoError(t, settingsRepo.Set(currencyRefreshLastAttemptKey, time.Now().Add(-2*time.Hour).UTC().Format(time.RFC3339Nano)))
	require.NoError(t, settingsRepo.Set(currencyRefreshFailuresKey, "0"))
	service := NewCurrencyService(repo, settingsRepo)
	var requestCount atomic.Int32
	service.client = stubClient(func(*http.Request) (*http.Response, error) {
		requestCount.Add(1)
		return nil, errors.New("no request expected")
	})

	conversion, err := service.ConvertAmount(3, "USD", "SEK")

	require.NoError(t, err)
	assert.Equal(t, 30.0, conversion.Amount)
	assert.True(t, conversion.Stale, "25-hour-old Fixer rates are stale, as before")
	assert.Equal(t, RateSource{Name: "Fixer.io", URL: "https://fixer.io"}, conversion.Source)
	assert.Equal(t, int32(0), requestCount.Load(), "the existing cooldown is honoured")
	var count int64
	require.NoError(t, db.Model(&models.ExchangeRate{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

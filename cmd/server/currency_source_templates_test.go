package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"subtrackr/internal/handlers"
	"subtrackr/internal/i18n"
	"subtrackr/internal/models"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadShippedTemplates(t *testing.T) (*i18n.Catalog, func(name string, data any) string) {
	t.Helper()
	t.Chdir("../..")
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir("web/locales"))
	tmpl := loadTemplates(catalog)
	return catalog, func(name string, data any) string {
		t.Helper()
		var out bytes.Buffer
		require.NoError(t, tmpl.ExecuteTemplate(&out, name, data))
		return out.String()
	}
}

func convertedSubscription(source service.RateSource) handlers.SubscriptionWithConversion {
	return handlers.SubscriptionWithConversion{
		Subscription:          &models.Subscription{ID: 1, Name: "Streaming", Cost: 3, OriginalCurrency: "USD", Schedule: "Monthly", Status: "Active"},
		ConvertedCost:         30,
		DisplayCurrency:       "SEK",
		DisplayCurrencySymbol: "kr",
		ShowConversion:        true,
		RateSourceName:        source.Name,
		RateSourceURL:         source.URL,
	}
}

func TestSubscriptionList_ConversionTooltipNamesRateSource(t *testing.T) {
	_, render := loadShippedTemplates(t)

	fixer := render("subscription-list.html", gin.H{
		"Subscriptions": []handlers.SubscriptionWithConversion{convertedSubscription(service.RateSource{Name: "Fixer.io", URL: "https://fixer.io"})},
		"Lang":          "en",
	})
	assert.Contains(t, fixer, `href="https://fixer.io"`)
	assert.Contains(t, fixer, `title="Original amount before conversion (rates from Fixer.io)"`)

	custom := render("subscription-list.html", gin.H{
		"Subscriptions": []handlers.SubscriptionWithConversion{convertedSubscription(service.RateSource{Name: "rates.example.com", URL: "https://rates.example.com:8443"})},
		"Lang":          "de",
	})
	assert.Contains(t, custom, `href="https://rates.example.com:8443"`)
	assert.Contains(t, custom, "(Kurse von rates.example.com)")
	assert.NotContains(t, custom, "fixer.io")
}

func TestSubscriptionList_ConversionLinkOmitsHrefWithoutSourceURL(t *testing.T) {
	_, render := loadShippedTemplates(t)

	html := render("subscription-list.html", gin.H{
		"Subscriptions": []handlers.SubscriptionWithConversion{convertedSubscription(service.RateSource{Name: "a custom source"})},
		"Lang":          "en",
	})

	start := strings.Index(html, "data-rate-source")
	require.NotEqual(t, -1, start)
	tag := html[strings.LastIndex(html[:start], "<a"):start]
	assert.NotContains(t, tag, "href=")
	assert.Contains(t, html, "(rates from a custom source)")
}

// Reads each locale file directly: catalog.T falls back to English, so it
// cannot show that a translation is missing.
func TestLocales_RateSourceKeysTranslatedEverywhere(t *testing.T) {
	keys := map[string]int{ // key -> number of %s placeholders
		"currency.original_amount_title":       1,
		"settings.currency_rates_source":       1,
		"settings.currency_rates_disabled":     0,
		"settings.currency_rates_invalid_url":  0,
		"settings.currency_rates_configurable": 0,
		"settings.currency_rates_docs":         0,
	}
	files, err := filepath.Glob("../../web/locales/*.json")
	require.NoError(t, err)
	require.Len(t, files, 5)
	var english map[string]string
	for _, file := range files {
		contents, err := os.ReadFile(file)
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, json.Unmarshal(contents, &messages), file)
		if filepath.Base(file) == "en.json" {
			english = messages
		}
		for key, placeholders := range keys {
			value, ok := messages[key]
			if !assert.True(t, ok, "%s is missing %s", file, key) {
				continue
			}
			assert.NotEmpty(t, strings.TrimSpace(value), "%s %s", file, key)
			assert.Equal(t, placeholders, strings.Count(value, "%"), "%s %s must have exactly %d %%s and no other %% verbs", file, key, placeholders)
			assert.Equal(t, placeholders, strings.Count(value, "%s"), "%s %s", file, key)
		}
	}
	for _, file := range files {
		if filepath.Base(file) == "en.json" {
			continue
		}
		contents, err := os.ReadFile(file)
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, json.Unmarshal(contents, &messages))
		for key := range keys {
			if key == "settings.currency_rates_docs" {
				continue
			}
			assert.NotEqual(t, english[key], messages[key], "%s %s is still the English text", file, key)
		}
	}
}

func TestDashboard_ConversionTooltipNamesRateSource(t *testing.T) {
	_, render := loadShippedTemplates(t)

	html := render("dashboard.html", gin.H{
		"Lang":          "nl",
		"Stats":         &models.Stats{},
		"Currency":      "SEK",
		"Subscriptions": []handlers.SubscriptionWithConversion{convertedSubscription(service.RateSource{Name: "rates.example.com", URL: "https://rates.example.com"})},
	})

	assert.Contains(t, html, `href="https://rates.example.com"`)
	assert.Contains(t, html, "(koersen van rates.example.com)")
	assert.NotContains(t, html, "fixer.io")
}

func settingsCurrencySection(t *testing.T, html string) string {
	t.Helper()
	start := strings.Index(html, "data-currency-rate-source")
	require.NotEqual(t, -1, start, "settings page has no rate-source note")
	end := strings.Index(html[start:], "</p>")
	require.NotEqual(t, -1, end)
	return html[start : start+end]
}

func TestSettings_CurrencySectionExplainsRateSource(t *testing.T) {
	_, render := loadShippedTemplates(t)
	page := func(enabled bool, source service.RateSource) string {
		return settingsCurrencySection(t, render("settings.html", gin.H{
			"Lang":                      "en",
			"Currency":                  "SEK",
			"Currencies":                service.GetAvailableCurrencies(),
			"CurrencyConversionEnabled": enabled,
			"RateSource":                source,
			"HighCostThreshold":         50.0,
			"ReminderDays":              7,
			"CancellationReminderDays":  7,
		}))
	}

	custom := page(true, service.RateSource{Name: "rates.example.com", URL: "https://rates.example.com"})
	assert.Contains(t, custom, "Exchange rates come from rates.example.com.")
	assert.Contains(t, custom, "Set FIXER_API_URL to use another source")
	assert.Contains(t, custom, `href="https://github.com/bscott/subtrackr#currency-conversion-optional"`)

	fixer := page(true, service.RateSource{Name: "Fixer.io", URL: "https://fixer.io"})
	assert.Contains(t, fixer, "Exchange rates come from Fixer.io.")

	disabled := page(false, service.RateSource{Name: "Fixer.io", URL: "https://fixer.io"})
	assert.Contains(t, disabled, "Exchange rates are not updated: no exchange-rate source is configured.")
	assert.NotContains(t, disabled, "Exchange rates come from")
	assert.Contains(t, disabled, "Fixer.io is the default (set FIXER_API_KEY)")
}

func TestSettings_CurrencySectionReportsInvalidSourceURL(t *testing.T) {
	_, render := loadShippedTemplates(t)

	html := settingsCurrencySection(t, render("settings.html", gin.H{
		"Lang":                        "en",
		"Currency":                    "SEK",
		"Currencies":                  service.GetAvailableCurrencies(),
		"CurrencySourceMisconfigured": true,
		"CurrencyConversionEnabled":   false,
		"RateSource":                  service.RateSource{Name: "Fixer.io", URL: "https://fixer.io"},
		"HighCostThreshold":           50.0,
		"ReminderDays":                7,
		"CancellationReminderDays":    7,
	}))

	assert.Contains(t, html, "FIXER_API_URL is not a valid http(s) URL")
	assert.NotContains(t, html, "no exchange-rate source is configured")
}

// RateSourceName and RateSourceURL come from operator config today, but the
// templates must stay safe if that ever changes: html/template has to keep
// escaping them, so nobody may switch these fields to template.HTML/URL.
func TestConversionTooltip_EscapesHostileRateSource(t *testing.T) {
	_, render := loadShippedTemplates(t)
	hostile := service.RateSource{Name: `x"><script>alert(1)</script>`, URL: "javascript:alert(1)"}

	for _, page := range []string{"subscription-list.html", "dashboard.html"} {
		html := render(page, gin.H{
			"Lang":          "en",
			"Stats":         &models.Stats{},
			"Currency":      "SEK",
			"Subscriptions": []handlers.SubscriptionWithConversion{convertedSubscription(hostile)},
		})
		assert.NotContains(t, html, "<script>alert(1)</script>", page)
		assert.NotContains(t, html, `href="javascript:`, page)
		assert.Contains(t, html, "#ZgotmplZ", "%s: html/template must neutralise the javascript: URL", page)
	}
}

// Every translation must use the same format verbs as English, for every key,
// or printf in a template renders %!s(MISSING) or %!(EXTRA ...).
func TestLocales_FormatVerbsMatchEnglishForEveryKey(t *testing.T) {
	verbs := regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z%]`)
	read := func(path string) map[string]string {
		t.Helper()
		contents, err := os.ReadFile(path)
		require.NoError(t, err)
		var messages map[string]string
		require.NoError(t, json.Unmarshal(contents, &messages), path)
		return messages
	}
	sortedVerbs := func(s string) []string {
		found := verbs.FindAllString(s, -1)
		sort.Strings(found)
		return found
	}
	english := read("../../web/locales/en.json")
	files, err := filepath.Glob("../../web/locales/*.json")
	require.NoError(t, err)
	for _, file := range files {
		messages := read(file)
		for key, value := range english {
			if translated, ok := messages[key]; ok {
				assert.Equal(t, sortedVerbs(value), sortedVerbs(translated), "%s %s", filepath.Base(file), key)
			}
		}
	}
}

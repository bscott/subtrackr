package handlers

import (
	"bytes"
	"html/template"
	"path/filepath"
	"testing"

	"subtrackr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func renderAnalyticsTemplate(t *testing.T, stats *models.Stats) string {
	t.Helper()

	tmpl, err := template.New("analytics.html").Funcs(template.FuncMap{
		"t":   func(_ interface{}, key string) string { return key },
		"mul": func(a, b float64) float64 { return a * b },
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
	}).ParseFiles(filepath.Join("..", "..", "templates", "analytics.html"))
	require.NoError(t, err)

	var output bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&output, "analytics.html", map[string]interface{}{
		"Title":          "Analytics",
		"Lang":           "en",
		"CurrencySymbol": "kr",
		"Stats":          stats,
	}))
	return output.String()
}

func renderDashboardTemplate(t *testing.T, stats *models.Stats) string {
	t.Helper()

	tmpl, err := template.New("dashboard.html").Funcs(template.FuncMap{
		"t":   func(_ interface{}, key string) string { return key },
		"mul": func(a, b float64) float64 { return a * b },
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"statusLabel":   func(_ interface{}, status string) string { return status },
		"scheduleLabel": func(_ interface{}, schedule string, _ int) string { return schedule },
	}).ParseFiles(filepath.Join("..", "..", "templates", "dashboard.html"))
	require.NoError(t, err)

	var output bytes.Buffer
	require.NoError(t, tmpl.ExecuteTemplate(&output, "dashboard.html", map[string]interface{}{
		"Title":          "Dashboard",
		"Lang":           "en",
		"CurrencySymbol": "kr",
		"Stats":          stats,
		"Subscriptions":  []SubscriptionWithConversion{},
	}))
	return output.String()
}

func TestAnalyticsTemplate_GroupsCurrenciesWithoutPercentageBarsWhenConversionIsIncomplete(t *testing.T) {
	output := renderAnalyticsTemplate(t, &models.Stats{
		ActiveSubscriptions:    2,
		CancelledSubscriptions: 1,
		ConversionComplete:     false,
		TotalsByCurrency: map[string]models.CurrencyTotals{
			"SEK": {
				TotalMonthlySpend:   20,
				TotalAnnualSpend:    240,
				ActiveSubscriptions: 1,
			},
			"USD": {
				TotalMonthlySpend:      3,
				TotalAnnualSpend:       36,
				TotalSaved:             120,
				MonthlySaved:           10,
				ActiveSubscriptions:    1,
				CancelledSubscriptions: 1,
			},
		},
		CategorySpendingByCurrency: map[string]map[string]float64{
			"Foreign": {"USD": 3},
			"Local":   {"SEK": 20},
		},
	})

	assert.Contains(t, output, "20.00 SEK")
	assert.Contains(t, output, "3.00 USD")
	assert.Contains(t, output, "240.00 SEK")
	assert.Contains(t, output, "36.00 USD")
	assert.Contains(t, output, "120.00 USD")
	assert.Contains(t, output, "Foreign")
	assert.Contains(t, output, "Local")
	assert.NotContains(t, output, "style=\"width:")
}

func TestAnalyticsTemplate_ShowsConvertedAggregateAndPercentageBarsWhenConversionIsComplete(t *testing.T) {
	output := renderAnalyticsTemplate(t, &models.Stats{
		TotalMonthlySpend:  50,
		TotalAnnualSpend:   600,
		ConversionComplete: true,
		CategorySpending: map[string]float64{
			"Foreign": 30,
			"Local":   20,
		},
	})

	assert.Contains(t, output, "kr50.00")
	assert.Contains(t, output, "kr600.00")
	assert.Contains(t, output, "style=\"width:")
	assert.NotContains(t, output, "20.00 SEK")
}

func TestDashboardTemplate_GroupsCurrenciesWithoutPercentageBarsWhenConversionIsIncomplete(t *testing.T) {
	output := renderDashboardTemplate(t, &models.Stats{
		ActiveSubscriptions: 2,
		ConversionComplete:  false,
		TotalsByCurrency: map[string]models.CurrencyTotals{
			"SEK": {
				TotalMonthlySpend:   20,
				TotalAnnualSpend:    240,
				ActiveSubscriptions: 1,
			},
			"USD": {
				TotalMonthlySpend:   3,
				TotalAnnualSpend:    36,
				ActiveSubscriptions: 1,
			},
		},
		CategorySpendingByCurrency: map[string]map[string]float64{
			"Foreign": {"USD": 3},
			"Local":   {"SEK": 20},
		},
	})

	assert.Contains(t, output, "20.00 SEK")
	assert.Contains(t, output, "3.00 USD")
	assert.Contains(t, output, "Foreign")
	assert.Contains(t, output, "Local")
	assert.NotContains(t, output, "style=\"width:")
}

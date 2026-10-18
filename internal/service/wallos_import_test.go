package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sampleWallosExport is a representative Wallos "Export to JSON" payload.
const sampleWallosExport = `{
  "success": true,
  "subscriptions": [
    {
      "Name": "Netflix",
      "Payment Cycle": "Monthly",
      "Next Payment": "2026-01-15",
      "Renewal": "Automatic",
      "Category": "Entertainment",
      "Payment Method": "Visa",
      "Paid By": "Alice",
      "Price": "$15.99",
      "Notes": "Family plan",
      "URL": "https://netflix.com",
      "State": "Enabled",
      "Notifications": "Enabled",
      "Cancellation Date": "",
      "Active": "Yes"
    },
    {
      "Name": "Adobe CC",
      "Payment Cycle": "Yearly",
      "Next Payment": "2026-06-01",
      "Renewal": "Manual",
      "Category": "Productivity",
      "Payment Method": "PayPal",
      "Paid By": "Bob",
      "Price": "€59,99",
      "Notes": "",
      "URL": "https://adobe.com",
      "State": "Disabled",
      "Notifications": "Disabled",
      "Cancellation Date": "2026-05-20",
      "Active": "No"
    },
    {
      "Name": "Storage Box",
      "Payment Cycle": "Every 3 Months",
      "Next Payment": "2026-02-10",
      "Renewal": "Automatic",
      "Category": "Storage",
      "Payment Method": "SEPA",
      "Paid By": "Alice",
      "Price": "£8.50",
      "Notes": "Hetzner",
      "URL": "",
      "State": "Enabled",
      "Notifications": "Enabled",
      "Cancellation Date": "",
      "Active": "Yes"
    }
  ]
}`

// -----------------------------------------------------------------------------
// Functional — full parse of a realistic export maps every field correctly
// -----------------------------------------------------------------------------

func TestWallosImport_Parse_Functional(t *testing.T) {
	svc := NewWallosImportService()
	subs, warnings, err := svc.Parse([]byte(sampleWallosExport))
	require.NoError(t, err)
	assert.Empty(t, warnings, "well-formed export should produce no warnings")
	require.Len(t, subs, 3)

	// Netflix: monthly, active, USD, has category + renewal date.
	nf := subs[0]
	assert.Equal(t, "Netflix", nf.Name)
	assert.InDelta(t, 15.99, nf.Cost, 0.001)
	assert.Equal(t, "Monthly", nf.Schedule)
	assert.Equal(t, 1, nf.ScheduleInterval)
	assert.Equal(t, "Active", nf.Status)
	assert.Equal(t, "USD", nf.OriginalCurrency)
	assert.Equal(t, "Visa", nf.PaymentMethod)
	assert.Equal(t, "Alice", nf.Account)
	assert.Equal(t, "Entertainment", nf.Category.Name)
	assert.Equal(t, "Family plan", nf.Notes)
	assert.True(t, nf.ReminderEnabled)
	require.NotNil(t, nf.RenewalDate)
	assert.Equal(t, "2026-01-15", nf.RenewalDate.Format("2006-01-02"))
	assert.Nil(t, nf.CancellationDate)

	// Adobe: yearly -> Annual, cancelled (Active No + cancellation date), EUR,
	// European decimal comma, notifications disabled.
	ad := subs[1]
	assert.Equal(t, "Adobe CC", ad.Name)
	assert.InDelta(t, 59.99, ad.Cost, 0.001)
	assert.Equal(t, "Annual", ad.Schedule)
	assert.Equal(t, "Cancelled", ad.Status)
	assert.Equal(t, "EUR", ad.OriginalCurrency)
	assert.False(t, ad.ReminderEnabled)
	require.NotNil(t, ad.CancellationDate)
	assert.Equal(t, "2026-05-20", ad.CancellationDate.Format("2006-01-02"))

	// Storage: "Every 3 Months" -> Monthly interval 3, GBP.
	sb := subs[2]
	assert.Equal(t, "Monthly", sb.Schedule)
	assert.Equal(t, 3, sb.ScheduleInterval)
	assert.Equal(t, "GBP", sb.OriginalCurrency)
	assert.InDelta(t, 8.50, sb.Cost, 0.001)
}

// -----------------------------------------------------------------------------
// Unit — individual field mapping helpers
// -----------------------------------------------------------------------------

func TestWallosImport_ParseCycle_Unit(t *testing.T) {
	cases := []struct {
		in       string
		schedule string
		interval int
	}{
		{"Daily", "Daily", 1},
		{"Weekly", "Weekly", 1},
		{"Monthly", "Monthly", 1},
		{"Yearly", "Annual", 1},
		{"Every 2 Weeks", "Weekly", 2},
		{"Every 3 Months", "Monthly", 3},
		{"Every 5 Years", "Annual", 5},
		{"every 4 days", "Daily", 4}, // case-insensitive
		{"Bogus", "Monthly", 1},      // unknown -> default
		{"", "Monthly", 1},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			s, i := parseWallosCycle(c.in)
			assert.Equal(t, c.schedule, s)
			assert.Equal(t, c.interval, i)
		})
	}
}

func TestWallosImport_ParsePrice_Unit(t *testing.T) {
	cases := []struct {
		in       string
		amount   float64
		currency string
		wantErr  bool
	}{
		{"$15.99", 15.99, "USD", false},
		{"€9,99", 9.99, "EUR", false},
		{"£8.50", 8.50, "GBP", false},
		{"15.99", 15.99, "", false}, // no symbol -> default currency
		{"$1,234.56", 1234.56, "USD", false},
		{"1.234,56 €", 1234.56, "EUR", false},
		{"9,99 €", 9.99, "EUR", false},
		{"₫100000", 100000, "VND", false},
		{"₫ 100.000", 100000, "VND", false},
		{"100.000 ₫", 100000, "VND", false},
		{"1.000.000 ₫", 1000000, "VND", false},
		{"₩1,000", 1000, "KRW", false},
		{"₩0.125", 0.125, "KRW", false}, // zero integer part stays a decimal
		{"$1,234,567", 1234567, "USD", false},
		{"€1.000.000,50", 1000000.50, "EUR", false},
		{"$1.5", 1.5, "USD", false},
		{"€1.000", 1, "EUR", false}, // single dot in a two-decimal currency stays a decimal point
		{"1.2.3", 0, "", true},
		{"", 0, "", true},
		{"abc", 0, "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			amt, cur, err := parseWallosPrice(c.in)
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, c.amount, amt, 0.001)
			assert.Equal(t, c.currency, cur)
		})
	}
}

func TestWallosImport_Status_Unit(t *testing.T) {
	assert.Equal(t, "Active", wallosStatus(wallosSubscription{Active: "Yes"}))
	assert.Equal(t, "Cancelled", wallosStatus(wallosSubscription{Active: "No", CancellationDate: "2026-01-01"}))
	assert.Equal(t, "Paused", wallosStatus(wallosSubscription{Active: "No"}))
}

func TestWallosImport_ParseDate_Unit(t *testing.T) {
	assert.Nil(t, parseWallosDate(""))
	assert.Nil(t, parseWallosDate("not-a-date"))
	d := parseWallosDate("2026-03-04")
	require.NotNil(t, d)
	assert.Equal(t, "2026-03-04", d.Format("2006-01-02"))
}

// -----------------------------------------------------------------------------
// Frame — boundary conditions and skipping rules
// -----------------------------------------------------------------------------

func TestWallosImport_Frame_SkipsAndBoundaries(t *testing.T) {
	svc := NewWallosImportService()

	payload := `{"success":true,"subscriptions":[
	  {"Name":"","Price":"$5","Payment Cycle":"Monthly","Active":"Yes"},
	  {"Name":"NoPrice","Price":"","Payment Cycle":"Monthly","Active":"Yes"},
	  {"Name":"ZeroPrice","Price":"$0","Payment Cycle":"Monthly","Active":"Yes"},
	  {"Name":"Good","Price":"$1","Payment Cycle":"Monthly","Active":"Yes"}
	]}`
	subs, warnings, err := svc.Parse([]byte(payload))
	require.NoError(t, err)
	require.Len(t, subs, 1, "only the valid row should be imported")
	assert.Equal(t, "Good", subs[0].Name)
	assert.Len(t, warnings, 3, "missing name, empty price, and zero price should each warn")

	// Empty subscriptions array is valid but yields nothing.
	subs, warnings, err = svc.Parse([]byte(`{"success":true,"subscriptions":[]}`))
	require.NoError(t, err)
	assert.Empty(t, subs)
	assert.Empty(t, warnings)
}

// -----------------------------------------------------------------------------
// Security — adversarial / malformed input is rejected without panicking
// -----------------------------------------------------------------------------

func TestWallosImport_Security_RejectsBadInput(t *testing.T) {
	svc := NewWallosImportService()

	// Not JSON at all.
	_, _, err := svc.Parse([]byte("not json"))
	assert.Error(t, err)

	// Valid JSON but not a Wallos export (no subscriptions key). Must be
	// rejected so arbitrary uploads can't be silently imported.
	_, _, err = svc.Parse([]byte(`{"foo":"bar"}`))
	assert.Error(t, err)

	// Wrong types where strings are expected must not panic.
	assert.NotPanics(t, func() {
		_, _, _ = svc.Parse([]byte(`{"subscriptions":[{"Name":123}]}`))
	})

	// Deeply nested / hostile JSON must not panic.
	assert.NotPanics(t, func() {
		_, _, _ = svc.Parse([]byte(`{"subscriptions":[{"Name":"x","Price":{"nested":true}}]}`))
	})
}

// -----------------------------------------------------------------------------
// Performance — parsing a large export stays fast
// -----------------------------------------------------------------------------

func TestWallosImport_Performance_LargeExport(t *testing.T) {
	const n = 5000
	entries := make([]string, 0, n)
	for i := 0; i < n; i++ {
		entries = append(entries, fmt.Sprintf(
			`{"Name":"Sub %d","Price":"$%d.99","Payment Cycle":"Monthly","Next Payment":"2026-01-15","Category":"Cat %d","Active":"Yes","Notifications":"Enabled"}`,
			i, i%50+1, i%10))
	}
	payload := `{"success":true,"subscriptions":[` + strings.Join(entries, ",") + `]}`

	svc := NewWallosImportService()
	start := time.Now()
	subs, warnings, err := svc.Parse([]byte(payload))
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Len(t, subs, n)
	assert.Less(t, elapsed, 2*time.Second, "parsing %d entries should be fast", n)
}

// -----------------------------------------------------------------------------
// Retry — not applicable
// -----------------------------------------------------------------------------

// The Wallos importer is a purely local, deterministic transformation of an
// uploaded file into subscription models. It performs no network or other
// external I/O, so there is nothing to retry with backoff. This placeholder
// documents that the Retry category was considered and is intentionally N/A.
func TestWallosImport_Retry_NotApplicable(t *testing.T) {
	t.Skip("N/A: import parsing has no external calls, so retry/backoff does not apply")
}

// -----------------------------------------------------------------------------
// Integration — parse a realistic multi-entry export and confirm the aggregate
// mapping (currencies, schedules, statuses, categories) end to end.
// -----------------------------------------------------------------------------

func TestWallosImport_Integration_AggregateMapping(t *testing.T) {
	svc := NewWallosImportService()
	subs, warnings, err := svc.Parse([]byte(sampleWallosExport))
	require.NoError(t, err)
	require.Empty(t, warnings)

	// Round-trip through JSON to ensure the produced models serialise cleanly
	// (as they would when handed to the create path / API).
	raw, err := json.Marshal(subs)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Netflix")

	schedules := map[string]int{}
	statuses := map[string]int{}
	categories := map[string]bool{}
	for _, s := range subs {
		schedules[s.Schedule]++
		statuses[s.Status]++
		if s.Category.Name != "" {
			categories[s.Category.Name] = true
		}
	}
	assert.Equal(t, 2, schedules["Monthly"]) // Netflix + Storage(every 3 months)
	assert.Equal(t, 1, schedules["Annual"])  // Adobe
	assert.Equal(t, 2, statuses["Active"])
	assert.Equal(t, 1, statuses["Cancelled"])
	assert.Len(t, categories, 3)
}

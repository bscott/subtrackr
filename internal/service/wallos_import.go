package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"subtrackr/internal/models"
	"time"
)

// WallosImportService parses a Wallos "Export to JSON" file into SubTrackr
// subscription models. It complements the existing CSV/JSON/iCal import paths.
//
// The Wallos export (endpoints/subscriptions/export.php) produces a top-level
// object of the shape:
//
//	{
//	  "success": true,
//	  "subscriptions": [
//	    {
//	      "Name": "Netflix",
//	      "Payment Cycle": "Monthly",          // or "Every 3 Months"
//	      "Next Payment": "2026-01-15",
//	      "Renewal": "Automatic",              // or "Manual"
//	      "Category": "Entertainment",
//	      "Payment Method": "Visa",
//	      "Paid By": "Alice",
//	      "Price": "$15.99",                    // currency symbol + amount
//	      "Notes": "...",
//	      "URL": "https://netflix.com",
//	      "State": "Enabled",                   // or "Disabled"
//	      "Notifications": "Enabled",           // or "Disabled"
//	      "Cancellation Date": "",
//	      "Active": "Yes"                        // or "No"
//	    }
//	  ]
//	}
type WallosImportService struct{}

// NewWallosImportService creates a new Wallos import service.
func NewWallosImportService() *WallosImportService {
	return &WallosImportService{}
}

// wallosSubscription mirrors a single entry in the Wallos export. Every field is
// a string in the Wallos export, including Price and Active.
type wallosSubscription struct {
	Name             string `json:"Name"`
	PaymentCycle     string `json:"Payment Cycle"`
	NextPayment      string `json:"Next Payment"`
	Renewal          string `json:"Renewal"`
	Category         string `json:"Category"`
	PaymentMethod    string `json:"Payment Method"`
	PaidBy           string `json:"Paid By"`
	Price            string `json:"Price"`
	Notes            string `json:"Notes"`
	URL              string `json:"URL"`
	State            string `json:"State"`
	Notifications    string `json:"Notifications"`
	CancellationDate string `json:"Cancellation Date"`
	Active           string `json:"Active"`
}

// wallosExport is the top-level Wallos export envelope.
type wallosExport struct {
	Success       bool                 `json:"success"`
	Subscriptions []wallosSubscription `json:"subscriptions"`
}

// everyCycleRe matches Wallos multi-period cycles like "Every 3 Months".
var everyCycleRe = regexp.MustCompile(`(?i)^every\s+(\d+)\s+(day|week|month|year)s?$`)

// symbolToCurrency maps distinctive currency symbols to their canonical ISO
// code. Symbols that are genuinely shared across currencies (notably "¥" for
// JPY/CNY and "kr" for the Scandinavian krona/krone) are intentionally left out
// so we fall back to the instance default rather than guessing wrong. Currency
// is a best-effort convenience only; SubTrackr defaults to the instance
// currency when this is empty.
var symbolToCurrency = map[string]string{
	"$":    "USD",
	"€":    "EUR",
	"£":    "GBP",
	"₹":    "INR",
	"₽":    "RUB",
	"₩":    "KRW",
	"R$":   "BRL",
	"A$":   "AUD",
	"C$":   "CAD",
	"NZ$":  "NZD",
	"S$":   "SGD",
	"HK$":  "HKD",
	"NT$":  "TWD",
	"Mex$": "MXN",
	"zł":   "PLN",
	"₫":    "VND",
}

// Parse reads a Wallos export and returns the mapped subscriptions along with
// per-row warnings for entries that were skipped or could not be fully mapped.
// A non-nil error is only returned when the file itself is not a valid Wallos
// export (bad JSON or no subscriptions array).
func (s *WallosImportService) Parse(data []byte) ([]models.Subscription, []string, error) {
	var export wallosExport
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, nil, fmt.Errorf("invalid Wallos export: not valid JSON: %w", err)
	}

	// Guard against arbitrary JSON that happens to parse but isn't a Wallos
	// export. The "subscriptions" key is required.
	if export.Subscriptions == nil {
		return nil, nil, fmt.Errorf("invalid Wallos export: missing \"subscriptions\" array")
	}

	var subs []models.Subscription
	var warnings []string

	for i, w := range export.Subscriptions {
		name := strings.TrimSpace(w.Name)
		if name == "" {
			warnings = append(warnings, fmt.Sprintf("entry %d skipped: missing name", i+1))
			continue
		}

		cost, currency, err := parseWallosPrice(w.Price)
		if err != nil || cost <= 0 {
			warnings = append(warnings, fmt.Sprintf("%q skipped: unusable price %q", name, w.Price))
			continue
		}

		schedule, interval := parseWallosCycle(w.PaymentCycle)

		sub := models.Subscription{
			Name:             name,
			Cost:             cost,
			Schedule:         schedule,
			ScheduleInterval: interval,
			Status:           wallosStatus(w),
			OriginalCurrency: currency,
			PaymentMethod:    strings.TrimSpace(w.PaymentMethod),
			Account:          strings.TrimSpace(w.PaidBy),
			URL:              strings.TrimSpace(w.URL),
			Notes:            strings.TrimSpace(w.Notes),
			RenewalDate:      parseWallosDate(w.NextPayment),
			CancellationDate: parseWallosDate(w.CancellationDate),
			ReminderEnabled:  !strings.EqualFold(strings.TrimSpace(w.Notifications), "Disabled"),
		}

		if cat := strings.TrimSpace(w.Category); cat != "" {
			sub.Category = models.Category{Name: cat}
		}

		subs = append(subs, sub)
	}

	return subs, warnings, nil
}

// parseWallosPrice extracts the numeric amount and (best-effort) currency code
// from a Wallos price string such as "$15.99", "€9,99", "100.000 ₫" or "15.99".
func parseWallosPrice(raw string) (float64, string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, "", fmt.Errorf("empty price")
	}

	// Identify the currency symbol: everything before the first digit, sign, or
	// decimal separator, falling back to everything after the last one.
	numStart := strings.IndexFunc(s, isWallosNumberRune)
	numEnd := strings.LastIndexFunc(s, isWallosNumberRune)
	currency := ""
	if numStart > 0 {
		currency = symbolToCurrency[strings.TrimSpace(s[:numStart])]
	}
	if currency == "" && numEnd >= 0 && numEnd+1 < len(s) {
		currency = symbolToCurrency[strings.TrimSpace(s[numEnd+1:])]
	}

	// Keep only the numeric portion.
	numPart := s
	if numStart >= 0 {
		numPart = s[numStart:]
	}
	numPart = strings.Map(func(r rune) rune {
		if isWallosNumberRune(r) {
			return r
		}
		return -1
	}, numPart)

	numPart = normaliseDecimal(numPart, CurrencyDecimals(currency) == 0)
	if numPart == "" {
		return 0, "", fmt.Errorf("no numeric value in %q", raw)
	}

	val, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, "", fmt.Errorf("could not parse amount from %q: %w", raw, err)
	}
	return val, currency, nil
}

func isWallosNumberRune(r rune) bool {
	return (r >= '0' && r <= '9') || r == '-' || r == '.' || r == ','
}

// normaliseDecimal converts a numeric string that may use "." or "," as either
// a thousands or decimal separator into a plain "." decimal form. zeroDecimal
// reports whether the currency has no minor unit (e.g. VND, JPY, KRW).
func normaliseDecimal(s string, zeroDecimal bool) string {
	lastDot := strings.LastIndex(s, ".")
	lastComma := strings.LastIndex(s, ",")
	switch {
	case lastDot == -1 && lastComma == -1:
		return s
	case lastComma == -1:
		return normaliseSingleSeparator(s, ".", zeroDecimal)
	case lastDot == -1:
		return normaliseSingleSeparator(s, ",", zeroDecimal)
	case lastComma > lastDot:
		// "1.234,56" -> comma is decimal
		return strings.ReplaceAll(s[:lastComma], ".", "") + "." + s[lastComma+1:]
	default:
		// "1,234.56" -> dot is decimal
		return strings.ReplaceAll(s[:lastDot], ",", "") + "." + s[lastDot+1:]
	}
}

// normaliseSingleSeparator handles numbers that use only one kind of separator.
// A repeated separator can only be thousands grouping ("1.000.000"). A single
// one is the decimal point ("15.99", "9,99"), except for zero-decimal
// currencies, where exactly three trailing digits mean grouping ("100.000").
func normaliseSingleSeparator(s, sep string, zeroDecimal bool) string {
	parts := strings.Split(s, sep)
	if len(parts) == 2 && !(zeroDecimal && len(parts[1]) == 3) {
		return parts[0] + "." + parts[1]
	}
	for _, group := range parts[1:] {
		if len(group) != 3 {
			return s // not a valid grouping; let ParseFloat reject it
		}
	}
	return strings.Join(parts, "")
}

// parseWallosCycle maps a Wallos "Payment Cycle" string to a SubTrackr schedule
// and interval. Recognised singular cycles: Daily/Weekly/Monthly/Yearly.
// Multi-period cycles ("Every N Days/Weeks/Months/Years") map to the same base
// schedule with the interval set to N. Unknown values default to Monthly/1.
func parseWallosCycle(raw string) (schedule string, interval int) {
	s := strings.TrimSpace(raw)

	unitToSchedule := map[string]string{
		"day":   "Daily",
		"week":  "Weekly",
		"month": "Monthly",
		"year":  "Annual",
	}

	if m := everyCycleRe.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n < 1 {
			n = 1
		}
		if sched, ok := unitToSchedule[strings.ToLower(m[2])]; ok {
			return sched, n
		}
	}

	switch strings.ToLower(s) {
	case "daily":
		return "Daily", 1
	case "weekly":
		return "Weekly", 1
	case "monthly":
		return "Monthly", 1
	case "yearly", "annual", "annually":
		return "Annual", 1
	default:
		return "Monthly", 1
	}
}

// parseWallosDate parses a Wallos date field, tolerating the common formats and
// empty values (returning nil for empty/unparseable input).
func parseWallosDate(raw string) *time.Time {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	formats := []string{"2006-01-02", "2006-01-02 15:04:05", time.RFC3339, "02/01/2006", "01/02/2006"}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return &t
		}
	}
	return nil
}

// wallosStatus maps the Wallos active/cancellation fields to a SubTrackr status.
func wallosStatus(w wallosSubscription) string {
	active := strings.EqualFold(strings.TrimSpace(w.Active), "Yes")
	if active {
		return "Active"
	}
	if strings.TrimSpace(w.CancellationDate) != "" {
		return "Cancelled"
	}
	return "Paused"
}

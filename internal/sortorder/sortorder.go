package sortorder

import "strings"

// Rule describes one column and direction in a user's ordered sort chain.
type Rule struct {
	Field     string
	Direction string
}

var validFields = map[string]struct{}{
	"name":         {},
	"category":     {},
	"cost":         {},
	"schedule":     {},
	"status":       {},
	"renewal_date": {},
	"created_at":   {},
}

// Parse decodes a comma-separated list of field:direction rules.
func Parse(spec string) []Rule {
	var rules []Rule
	for _, item := range strings.Split(spec, ",") {
		parts := strings.Split(strings.TrimSpace(item), ":")
		if len(parts) != 2 {
			continue
		}
		rules = append(rules, Rule{
			Field:     strings.TrimSpace(parts[0]),
			Direction: strings.TrimSpace(parts[1]),
		})
	}
	return Normalize(rules)
}

// FromLegacy converts the former single-column query parameters to one rule.
func FromLegacy(sortBy, order string) []Rule {
	var rules []Rule
	if _, ok := validFields[sortBy]; ok {
		if order != "asc" && order != "desc" {
			order = "desc"
		}
		rules = append(rules, Rule{Field: sortBy, Direction: order})
	}
	return Normalize(rules)
}

// Normalize removes invalid and duplicate rules while preserving their order.
func Normalize(rules []Rule) []Rule {
	seen := make(map[string]struct{}, len(rules))
	out := make([]Rule, 0, len(rules))
	for _, rule := range rules {
		if _, ok := validFields[rule.Field]; !ok {
			continue
		}
		if rule.Direction != "asc" && rule.Direction != "desc" {
			continue
		}
		if _, ok := seen[rule.Field]; ok {
			continue
		}
		seen[rule.Field] = struct{}{}
		out = append(out, rule)
	}
	return out
}

// Encode serializes valid rules in their current priority order.
func Encode(rules []Rule) string {
	rules = Normalize(rules)
	items := make([]string, 0, len(rules))
	for _, rule := range rules {
		items = append(items, rule.Field+":"+rule.Direction)
	}
	return strings.Join(items, ",")
}

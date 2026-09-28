package sortorder

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		spec string
		want []Rule
	}{
		{
			name: "empty spec",
			spec: "",
			want: nil,
		},
		{
			name: "single rule",
			spec: "name:asc",
			want: []Rule{{Field: "name", Direction: "asc"}},
		},
		{
			name: "multiple rules keep their order",
			spec: "category:asc,cost:desc,name:asc",
			want: []Rule{
				{Field: "category", Direction: "asc"},
				{Field: "cost", Direction: "desc"},
				{Field: "name", Direction: "asc"},
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			spec: "  name : asc , cost : desc  ",
			want: []Rule{
				{Field: "name", Direction: "asc"},
				{Field: "cost", Direction: "desc"},
			},
		},
		{
			name: "unknown field is dropped",
			spec: "not_a_column:asc,name:asc",
			want: []Rule{{Field: "name", Direction: "asc"}},
		},
		{
			name: "uppercase direction is rejected, not coerced",
			spec: "name:ASC",
			want: nil,
		},
		{
			name: "unknown direction is dropped",
			spec: "name:sideways",
			want: nil,
		},
		{
			name: "duplicate field keeps the first occurrence",
			spec: "name:asc,name:desc",
			want: []Rule{{Field: "name", Direction: "asc"}},
		},
		{
			name: "item with too many parts is dropped",
			spec: "name:asc:desc,cost:asc",
			want: []Rule{{Field: "cost", Direction: "asc"}},
		},
		{
			name: "item with no direction is dropped",
			spec: "name,cost:asc",
			want: []Rule{{Field: "cost", Direction: "asc"}},
		},
		{
			name: "empty items are skipped",
			spec: ",,name:asc,,",
			want: []Rule{{Field: "name", Direction: "asc"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertRules(t, Parse(tc.spec), tc.want)
		})
	}
}

// TestParseRejectsInjection guards the boundary that keeps untrusted input out
// of the ORDER BY clause built in repository.GetAllSorted. These specs must
// reduce to zero rules, so the caller falls back to the default ordering.
func TestParseRejectsInjection(t *testing.T) {
	specs := []string{
		"name); DROP TABLE subscriptions; --:asc",
		"name:asc); DROP TABLE subscriptions--",
		"subscriptions.name:asc",
		"cost;:desc",
		"1=1:asc",
		"name:asc--",
		"*:asc",
		"name:asc OR 1=1",
	}

	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			if got := Parse(spec); len(got) != 0 {
				t.Fatalf("Parse(%q) = %+v, want no rules", spec, got)
			}
		})
	}
}

// TestParseNeverEmitsUnsafeTokens asserts the invariant the ORDER BY builder
// actually relies on: whatever Parse returns, every Field is a known column key
// and every Direction is exactly "asc" or "desc". A spec may legitimately carry
// a valid rule alongside a malicious fragment ("name:asc, (SELECT 1)") — the
// fragment must be discarded while the valid rule survives.
func TestParseNeverEmitsUnsafeTokens(t *testing.T) {
	specs := []string{
		"name:asc, (SELECT 1)",
		"name:asc,cost:desc); DELETE FROM subscriptions--",
		"name:asc,(CASE WHEN 1=1 THEN 1 END):asc",
		"name:asc,cost:desc UNION SELECT NULL",
		"name); DROP TABLE subscriptions; --:asc,cost:desc",
		"'name':asc",
		"name:'asc'",
	}

	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			for _, rule := range Parse(spec) {
				if _, ok := validFields[rule.Field]; !ok {
					t.Errorf("Parse(%q) emitted unknown field %q", spec, rule.Field)
				}
				if rule.Direction != "asc" && rule.Direction != "desc" {
					t.Errorf("Parse(%q) emitted unsafe direction %q", spec, rule.Direction)
				}
			}
		})
	}
}

// TestNormalizeRejectsUnsafeDirectValues covers callers that build Rules in Go
// rather than parsing a spec, which is how a future refactor could reintroduce
// an injection path.
func TestNormalizeRejectsUnsafeDirectValues(t *testing.T) {
	unsafe := []Rule{
		{Field: "name); DROP TABLE subscriptions; --", Direction: "asc"},
		{Field: "name", Direction: "asc, (SELECT 1)"},
		{Field: "name", Direction: "asc; DELETE FROM subscriptions"},
		{Field: "subscriptions.name", Direction: "asc"},
		{Field: "*", Direction: "desc"},
	}

	for _, rule := range unsafe {
		t.Run(rule.Field+":"+rule.Direction, func(t *testing.T) {
			if got := Normalize([]Rule{rule}); len(got) != 0 {
				t.Fatalf("Normalize(%+v) = %+v, want no rules", rule, got)
			}
		})
	}
}

func TestFromLegacy(t *testing.T) {
	tests := []struct {
		name   string
		sortBy string
		order  string
		want   []Rule
	}{
		{
			name:   "valid field and direction",
			sortBy: "cost",
			order:  "asc",
			want:   []Rule{{Field: "cost", Direction: "asc"}},
		},
		{
			name:   "missing direction defaults to desc",
			sortBy: "cost",
			order:  "",
			want:   []Rule{{Field: "cost", Direction: "desc"}},
		},
		{
			name:   "unknown direction defaults to desc",
			sortBy: "cost",
			order:  "sideways",
			want:   []Rule{{Field: "cost", Direction: "desc"}},
		},
		{
			name:   "unknown field yields no rules",
			sortBy: "not_a_column",
			order:  "asc",
			want:   nil,
		},
		{
			name:   "empty field yields no rules",
			sortBy: "",
			order:  "asc",
			want:   nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertRules(t, FromLegacy(tc.sortBy, tc.order), tc.want)
		})
	}
}

func TestNormalize(t *testing.T) {
	in := []Rule{
		{Field: "name", Direction: "asc"},
		{Field: "bogus", Direction: "asc"},
		{Field: "cost", Direction: "ASC"},
		{Field: "name", Direction: "desc"},
		{Field: "status", Direction: "desc"},
	}

	assertRules(t, Normalize(in), []Rule{
		{Field: "name", Direction: "asc"},
		{Field: "status", Direction: "desc"},
	})
}

func TestNormalizeAcceptsEveryValidField(t *testing.T) {
	for field := range validFields {
		t.Run(field, func(t *testing.T) {
			for _, dir := range []string{"asc", "desc"} {
				got := Normalize([]Rule{{Field: field, Direction: dir}})
				if len(got) != 1 || got[0].Field != field || got[0].Direction != dir {
					t.Fatalf("Normalize(%s:%s) = %+v, want it preserved", field, dir, got)
				}
			}
		})
	}
}

func TestEncode(t *testing.T) {
	tests := []struct {
		name  string
		rules []Rule
		want  string
	}{
		{
			name:  "no rules encodes to empty string",
			rules: nil,
			want:  "",
		},
		{
			name:  "single rule",
			rules: []Rule{{Field: "name", Direction: "asc"}},
			want:  "name:asc",
		},
		{
			name: "priority order is preserved",
			rules: []Rule{
				{Field: "category", Direction: "asc"},
				{Field: "cost", Direction: "desc"},
			},
			want: "category:asc,cost:desc",
		},
		{
			name: "invalid rules are dropped before encoding",
			rules: []Rule{
				{Field: "bogus", Direction: "asc"},
				{Field: "name", Direction: "ASC"},
				{Field: "cost", Direction: "desc"},
			},
			want: "cost:desc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Encode(tc.rules); got != tc.want {
				t.Fatalf("Encode(%+v) = %q, want %q", tc.rules, got, tc.want)
			}
		})
	}
}

func TestEncodeParseRoundTrip(t *testing.T) {
	spec := "category:asc,cost:desc,renewal_date:asc"
	if got := Encode(Parse(spec)); got != spec {
		t.Fatalf("Encode(Parse(%q)) = %q, want the original spec", spec, got)
	}
}

func assertRules(t *testing.T, got, want []Rule) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d rules (%+v), want %d (%+v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("rule %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

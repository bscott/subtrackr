package handlers

import "testing"

func TestEscapeICalText(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"Gym, Pool; Spa", `Gym\, Pool\; Spa`},
		{"literal\\n and real\nline", `literal\\n and real\nline`},
		{"name\r\nEND:VEVENT\rBEGIN:VEVENT", `name\nEND:VEVENT\nBEGIN:VEVENT`},
		{"中文 € ñ", "中文 € ñ"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := escapeICalText(tc.input); got != tc.want {
				t.Fatalf("escapeICalText(%q) = %q; want %q", tc.input, got, tc.want)
			}
		})
	}
}

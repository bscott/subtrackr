package handlers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestParseDatePtr(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *time.Time
		valid    bool
	}{
		{
			name:     "Valid date string",
			input:    "2024-01-15",
			expected: timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
			valid:    true,
		},
		{
			name:     "Valid date with leap year",
			input:    "2024-02-29",
			expected: timePtr(time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)),
			valid:    true,
		},
		{
			name:     "Valid date at year boundary",
			input:    "2024-12-31",
			expected: timePtr(time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC)),
			valid:    true,
		},
		{
			name:     "Empty string",
			input:    "",
			expected: nil,
			valid:    true,
		},
		{
			name:     "Invalid date format - wrong separator",
			input:    "2024/01/15",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date format - wrong order",
			input:    "15-01-2024",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date - invalid month",
			input:    "2024-13-15",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date - invalid day",
			input:    "2024-02-30",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date - non-leap year Feb 29",
			input:    "2025-02-29",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date - text",
			input:    "not-a-date",
			expected: nil,
			valid:    false,
		},
		{
			name:     "Invalid date - partial",
			input:    "2024-01",
			expected: nil,
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseDatePtr(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result, "Expected nil for invalid/empty input")
			} else {
				assert.NotNil(t, result, "Expected non-nil result for valid input")
				if result != nil {
					// Compare date components only (Year, Month, Day) as parseDatePtr returns UTC dates with zero time components
					assert.Equal(t, tt.expected.Year(), result.Year(), "Year should match")
					assert.Equal(t, tt.expected.Month(), result.Month(), "Month should match")
					assert.Equal(t, tt.expected.Day(), result.Day(), "Day should match")
				}
			}
		})
	}
}

// Helper function to create time pointer
func timePtr(t time.Time) *time.Time {
	return &t
}


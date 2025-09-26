package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExchangeRate_IsStale(t *testing.T) {
	tests := []struct {
		name           string
		lastUpdated    time.Time
		expectedStale  bool
		description    string
	}{
		{
			name:          "Fresh rate - just updated",
			lastUpdated:   time.Now(),
			expectedStale: false,
			description:   "Rate updated now should not be stale",
		},
		{
			name:          "Fresh rate - 30 minutes old",
			lastUpdated:   time.Now().Add(-30 * time.Minute),
			expectedStale: false,
			description:   "Rate updated 30 minutes ago should not be stale",
		},
		{
			name:          "Stale rate - 25 hours old",
			lastUpdated:   time.Now().Add(-25 * time.Hour),
			expectedStale: true,
			description:   "Rate updated 25 hours ago should be stale",
		},
		{
			name:          "Very stale rate - 2 days old",
			lastUpdated:   time.Now().Add(-48 * time.Hour),
			expectedStale: true,
			description:   "Rate updated 2 days ago should be stale",
		},
		{
			name:          "Boundary case - just over 24 hours old",
			lastUpdated:   time.Now().Add(-24*time.Hour - time.Minute),
			expectedStale: true,
			description:   "Rate updated just over 24 hours ago should be stale",
		},
		{
			name:          "Boundary case - just under 24 hours",
			lastUpdated:   time.Now().Add(-23 * time.Hour),
			expectedStale: false,
			description:   "Rate updated 23 hours ago should not be stale",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := &ExchangeRate{
				Date: tt.lastUpdated,
			}

			result := rate.IsStale()
			assert.Equal(t, tt.expectedStale, result, tt.description)
		})
	}
}
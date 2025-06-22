package models

import (
	"testing"
	"time"
)

func TestSubscription_AnnualCost(t *testing.T) {
	tests := []struct {
		name     string
		sub      Subscription
		expected float64
	}{
		{
			name: "Monthly subscription",
			sub: Subscription{
				Cost:     10.99,
				Schedule: "Monthly",
			},
			expected: 131.88, // 10.99 * 12
		},
		{
			name: "Annual subscription",
			sub: Subscription{
				Cost:     99.99,
				Schedule: "Annual",
			},
			expected: 99.99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sub.AnnualCost()
			if result != tt.expected {
				t.Errorf("AnnualCost() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSubscription_MonthlyCost(t *testing.T) {
	tests := []struct {
		name     string
		sub      Subscription
		expected float64
	}{
		{
			name: "Monthly subscription",
			sub: Subscription{
				Cost:     15.99,
				Schedule: "Monthly",
			},
			expected: 15.99,
		},
		{
			name: "Annual subscription",
			sub: Subscription{
				Cost:     120.00,
				Schedule: "Annual",
			},
			expected: 10.00, // 120.00 / 12
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sub.MonthlyCost()
			if result != tt.expected {
				t.Errorf("MonthlyCost() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSubscription_DailyCost(t *testing.T) {
	tests := []struct {
		name     string
		sub      Subscription
		expected float64
	}{
		{
			name: "Monthly subscription",
			sub: Subscription{
				Cost:     30.44,
				Schedule: "Monthly",
			},
			expected: 1.0, // 30.44 / 30.44
		},
		{
			name: "Annual subscription",
			sub: Subscription{
				Cost:     365.28,
				Schedule: "Annual",
			},
			expected: 1.0, // (365.28 / 12) / 30.44
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sub.DailyCost()
			// Use delta comparison for floating point
			delta := 0.001
			if diff := result - tt.expected; diff < -delta || diff > delta {
				t.Errorf("DailyCost() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSubscription_IsHighCost(t *testing.T) {
	tests := []struct {
		name     string
		sub      Subscription
		expected bool
	}{
		{
			name: "Low cost monthly",
			sub: Subscription{
				Cost:     25.00,
				Schedule: "Monthly",
			},
			expected: false,
		},
		{
			name: "High cost monthly",
			sub: Subscription{
				Cost:     75.00,
				Schedule: "Monthly",
			},
			expected: true,
		},
		{
			name: "High cost annual",
			sub: Subscription{
				Cost:     1200.00, // $100/month
				Schedule: "Annual",
			},
			expected: true,
		},
		{
			name: "Low cost annual",
			sub: Subscription{
				Cost:     360.00, // $30/month
				Schedule: "Annual",
			},
			expected: false,
		},
		{
			name: "Exactly $50 monthly",
			sub: Subscription{
				Cost:     50.00,
				Schedule: "Monthly",
			},
			expected: false, // > 50, not >= 50
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.sub.IsHighCost()
			if result != tt.expected {
				t.Errorf("IsHighCost() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSubscription_BeforeCreate(t *testing.T) {
	// Test that renewal date is set for active subscriptions
	t.Run("Active monthly subscription", func(t *testing.T) {
		sub := &Subscription{
			Status:   "Active",
			Schedule: "Monthly",
		}
		
		err := sub.BeforeCreate(nil)
		if err != nil {
			t.Errorf("BeforeCreate() error = %v", err)
		}
		
		if sub.RenewalDate == nil {
			t.Error("RenewalDate should be set for active subscription")
		}
		
		// Check that renewal date is approximately 30 days from now
		expectedDays := 30
		actualDays := int(sub.RenewalDate.Sub(time.Now()).Hours() / 24)
		if actualDays < expectedDays-1 || actualDays > expectedDays+1 {
			t.Errorf("RenewalDate should be ~%d days from now, got %d", expectedDays, actualDays)
		}
	})

	t.Run("Active annual subscription", func(t *testing.T) {
		sub := &Subscription{
			Status:   "Active",
			Schedule: "Annual",
		}
		
		err := sub.BeforeCreate(nil)
		if err != nil {
			t.Errorf("BeforeCreate() error = %v", err)
		}
		
		if sub.RenewalDate == nil {
			t.Error("RenewalDate should be set for active subscription")
		}
		
		// Check that renewal date is approximately 365 days from now
		expectedDays := 365
		actualDays := int(sub.RenewalDate.Sub(time.Now()).Hours() / 24)
		if actualDays < expectedDays-1 || actualDays > expectedDays+1 {
			t.Errorf("RenewalDate should be ~%d days from now, got %d", expectedDays, actualDays)
		}
	})

	t.Run("Cancelled subscription", func(t *testing.T) {
		sub := &Subscription{
			Status:   "Cancelled",
			Schedule: "Monthly",
		}
		
		err := sub.BeforeCreate(nil)
		if err != nil {
			t.Errorf("BeforeCreate() error = %v", err)
		}
		
		if sub.RenewalDate != nil {
			t.Error("RenewalDate should not be set for cancelled subscription")
		}
	})

	t.Run("Active subscription with existing renewal date", func(t *testing.T) {
		existingDate := time.Now().AddDate(0, 2, 0)
		sub := &Subscription{
			Status:      "Active",
			Schedule:    "Monthly",
			RenewalDate: &existingDate,
		}
		
		err := sub.BeforeCreate(nil)
		if err != nil {
			t.Errorf("BeforeCreate() error = %v", err)
		}
		
		if sub.RenewalDate != &existingDate {
			t.Error("RenewalDate should not be changed if already set")
		}
	})
}
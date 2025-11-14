package models

import (
	"time"

	"github.com/dromara/carbon/v2"
	"gorm.io/gorm"
)

type Subscription struct {
	ID                     uint       `json:"id" gorm:"primaryKey"`
	Name                   string     `json:"name" gorm:"not null" validate:"required"`
	Cost                   float64    `json:"cost" gorm:"not null" validate:"required,gt=0"`
	OriginalCurrency       string     `json:"original_currency" gorm:"size:3;default:'USD'"`
	Schedule               string     `json:"schedule" gorm:"not null" validate:"required,oneof=Monthly Annual Weekly Daily"`
	Status                 string     `json:"status" gorm:"not null" validate:"required,oneof=Active Cancelled Paused Trial"`
	CategoryID             uint       `json:"category_id"`
	Category               Category   `json:"category" gorm:"foreignKey:CategoryID"`
	PaymentMethod          string     `json:"payment_method" gorm:""`
	Account                string     `json:"account" gorm:""`
	StartDate              *time.Time `json:"start_date" gorm:""`
	RenewalDate            *time.Time `json:"renewal_date" gorm:""`
	CancellationDate       *time.Time `json:"cancellation_date" gorm:""`
	URL                    string     `json:"url" gorm:""`
	IconURL                string     `json:"icon_url" gorm:""` // URL to subscription icon/logo
	Notes                  string     `json:"notes" gorm:""`
	Usage                  string     `json:"usage" gorm:"" validate:"omitempty,oneof=High Medium Low None"`
	DateCalculationVersion int        `json:"date_calculation_version" gorm:"default:1"`
	CreatedAt              time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt              time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

// AnnualCost calculates the annual cost based on schedule
func (s *Subscription) AnnualCost() float64 {
	switch s.Schedule {
	case "Annual":
		return s.Cost
	case "Monthly":
		return s.Cost * 12
	case "Weekly":
		return s.Cost * 52
	case "Daily":
		return s.Cost * 365
	default:
		return s.Cost * 12
	}
}

// MonthlyCost calculates the monthly cost based on schedule
func (s *Subscription) MonthlyCost() float64 {
	switch s.Schedule {
	case "Annual":
		return s.Cost / 12
	case "Monthly":
		return s.Cost
	case "Weekly":
		return s.Cost * 4.33 // 52 weeks / 12 months
	case "Daily":
		return s.Cost * 30.44 // Average days per month
	default:
		return s.Cost
	}
}

// DailyCost calculates the daily cost
func (s *Subscription) DailyCost() float64 {
	return s.MonthlyCost() / 30.44 // Average days per month
}

// IsHighCost determines if this is a high-cost subscription (>$50/month)
func (s *Subscription) IsHighCost() bool {
	return s.MonthlyCost() > 50
}

// BeforeCreate hook to set renewal date for active subscriptions
func (s *Subscription) BeforeCreate(tx *gorm.DB) error {
	if s.Status == "Active" && s.RenewalDate == nil {
		// Set renewal date based on schedule
		s.calculateNextRenewalDate()
	}
	return nil
}

// BeforeUpdate hook to recalculate renewal date when schedule changes
func (s *Subscription) BeforeUpdate(tx *gorm.DB) error {
	// Get the original values to check for schedule change
	var original Subscription
	if err := tx.Model(&Subscription{}).Where("id = ?", s.ID).First(&original).Error; err == nil {
		// If schedule changed and status is Active, recalculate renewal date
		// Use start date if available to preserve billing anniversary
		if original.Schedule != s.Schedule && s.Status == "Active" {
			s.calculateNextRenewalDate()
		}
	}

	// Also calculate if renewal date is nil and status is Active
	if s.RenewalDate == nil && s.Status == "Active" {
		s.calculateNextRenewalDate()
	}
	return nil
}

// calculateNextRenewalDate calculates the next renewal date based on schedule and version.
//
// Version Selection Logic:
// - V1 (default): Original calculation logic for backward compatibility
//   - All existing subscriptions use V1 unless explicitly migrated
//   - Uses standard Go time.AddDate() which may cause edge cases
//   - Example: Jan 31 + 1 month = Mar 3 (due to Feb having 28 days)
// - V2: Enhanced calculation using Carbon library for robust date arithmetic
//   - Must be explicitly set via DateCalculationVersion = 2
//   - Uses Carbon's AddMonthsNoOverflow/AddYearsNoOverflow for better handling
//   - Example: Jan 31 + 1 month = Feb 28 (preserves month-end semantics)
//   - Recommended for new subscriptions and can be migrated via migrate-dates command
func (s *Subscription) calculateNextRenewalDate() {
	// Use versioned calculation approach
	switch s.DateCalculationVersion {
	case 2:
		s.calculateNextRenewalDateV2()
	default:
		// Use V1 logic for backward compatibility
		s.calculateNextRenewalDateV1()
	}
}

// calculateNextRenewalDateV1 uses the original calculation logic
func (s *Subscription) calculateNextRenewalDateV1() {
	// If we have a start date, calculate renewal from start date
	// Otherwise, calculate from now
	if s.StartDate != nil {
		s.calculateNextRenewalDateFromStartDate()
	} else {
		s.calculateNextRenewalDateFromNow()
	}
}

// calculateNextRenewalDateV2 uses Carbon library for robust date handling
func (s *Subscription) calculateNextRenewalDateV2() {
	if s.StartDate == nil {
		s.calculateNextRenewalDateFromNowV2()
		return
	}

	start := carbon.CreateFromStdTime(*s.StartDate)
	now := carbon.Now()

	switch s.Schedule {
	case "Monthly":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddMonthsNoOverflow(1)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Annual":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddYearsNoOverflow(1)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Weekly":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddWeeks(1)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Daily":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddDays(1)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	default:
		// Default to monthly
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddMonthsNoOverflow(1)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate
	}
}

// calculateNextRenewalDateFromStartDate calculates the next renewal date from start date
func (s *Subscription) calculateNextRenewalDateFromStartDate() {
	if s.StartDate == nil {
		s.calculateNextRenewalDateFromNow()
		return
	}

	var renewalDate time.Time
	baseDate := *s.StartDate
	now := time.Now()

	// Calculate the next renewal date based on the schedule
	switch s.Schedule {
	case "Annual":
		// Find the next anniversary of the start date
		years := 1 // Start with first renewal period
		for {
			renewalDate = baseDate.AddDate(years, 0, 0)
			if renewalDate.After(now) {
				break
			}
			years++
		}
	case "Monthly":
		// Find the next monthly anniversary
		// Handle month-end dates specially to preserve "last day of month" semantics
		startDay := baseDate.Day()
		startYear := baseDate.Year()
		startMonth := int(baseDate.Month())
		months := 1 // Start with first renewal period, not the start date itself

		for {
			// Calculate the target year and month properly without Go's overflow behavior
			totalMonths := startMonth + months - 1 // Convert to 0-based
			targetYear := startYear + totalMonths/12
			targetMonth := time.Month((totalMonths % 12) + 1) // Convert back to 1-based

			// Get the last day of the target month
			lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, baseDate.Location()).Day()

			// If original date was on a day that doesn't exist in target month,
			// use the last day of that month
			targetDay := startDay
			if startDay > lastDay {
				targetDay = lastDay
			}

			renewalDate = time.Date(targetYear, targetMonth, targetDay,
				baseDate.Hour(), baseDate.Minute(), baseDate.Second(),
				baseDate.Nanosecond(), baseDate.Location())

			if renewalDate.After(now) {
				break
			}
			months++
		}
	case "Weekly":
		// Find the next weekly anniversary
		weeks := 1 // Start with first renewal period
		for {
			renewalDate = baseDate.AddDate(0, 0, weeks*7)
			if renewalDate.After(now) {
				break
			}
			weeks++
		}
	case "Daily":
		// Find the next daily renewal
		days := 1 // Start with first renewal period
		for {
			renewalDate = baseDate.AddDate(0, 0, days)
			if renewalDate.After(now) {
				break
			}
			days++
		}
	default:
		// Default to monthly
		startDay := baseDate.Day()
		startYear := baseDate.Year()
		startMonth := int(baseDate.Month())
		months := 1 // Start with first renewal period, not the start date itself

		for {
			// Calculate the target year and month properly without Go's overflow behavior
			totalMonths := startMonth + months - 1 // Convert to 0-based
			targetYear := startYear + totalMonths/12
			targetMonth := time.Month((totalMonths % 12) + 1) // Convert back to 1-based

			// Get the last day of the target month
			lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, baseDate.Location()).Day()

			// If original date was on a day that doesn't exist in target month,
			// use the last day of that month
			targetDay := startDay
			if startDay > lastDay {
				targetDay = lastDay
			}

			renewalDate = time.Date(targetYear, targetMonth, targetDay,
				baseDate.Hour(), baseDate.Minute(), baseDate.Second(),
				baseDate.Nanosecond(), baseDate.Location())

			if renewalDate.After(now) {
				break
			}
			months++
		}
	}

	s.RenewalDate = &renewalDate
}

// calculateNextRenewalDateFromNow calculates the next renewal date from current time
func (s *Subscription) calculateNextRenewalDateFromNow() {
	var renewalDate time.Time
	baseDate := time.Now()

	switch s.Schedule {
	case "Annual":
		renewalDate = baseDate.AddDate(1, 0, 0)
	case "Monthly":
		renewalDate = baseDate.AddDate(0, 1, 0)
	case "Weekly":
		renewalDate = baseDate.AddDate(0, 0, 7)
	case "Daily":
		renewalDate = baseDate.AddDate(0, 0, 1)
	default:
		renewalDate = baseDate.AddDate(0, 1, 0)
	}
	s.RenewalDate = &renewalDate
}

// calculateNextRenewalDateFromNowV2 calculates renewal date from now using Carbon
func (s *Subscription) calculateNextRenewalDateFromNowV2() {
	now := carbon.Now()

	switch s.Schedule {
	case "Annual":
		renewalDate := now.AddYear().StdTime()
		s.RenewalDate = &renewalDate
	case "Monthly":
		renewalDate := now.AddMonthsNoOverflow(1).StdTime()
		s.RenewalDate = &renewalDate
	case "Weekly":
		renewalDate := now.AddWeek().StdTime()
		s.RenewalDate = &renewalDate
	case "Daily":
		renewalDate := now.AddDay().StdTime()
		s.RenewalDate = &renewalDate
	default:
		renewalDate := now.AddMonthsNoOverflow(1).StdTime()
		s.RenewalDate = &renewalDate
	}
}

// Stats represents aggregated subscription statistics
type Stats struct {
	TotalMonthlySpend      float64            `json:"total_monthly_spend"`
	TotalAnnualSpend       float64            `json:"total_annual_spend"`
	ActiveSubscriptions    int                `json:"active_subscriptions"`
	CancelledSubscriptions int                `json:"cancelled_subscriptions"`
	TotalSaved             float64            `json:"total_saved"`
	MonthlySaved           float64            `json:"monthly_saved"`
	UpcomingRenewals       int                `json:"upcoming_renewals"`
	CategorySpending       map[string]float64 `json:"category_spending"`
}

// CategoryStat represents spending by category
type CategoryStat struct {
	Category string  `json:"category"`
	Amount   float64 `json:"amount"`
	Count    int     `json:"count"`
}

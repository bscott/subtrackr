package models

import (
	"fmt"
	"time"

	"github.com/dromara/carbon/v2"
	"gorm.io/gorm"
)

type Subscription struct {
	ID                     uint       `json:"id" gorm:"primaryKey"`
	Name                   string     `json:"name" gorm:"not null" validate:"required"`
	Cost                   float64    `json:"cost" gorm:"not null" validate:"required,gt=0"`
	OriginalCurrency       string     `json:"original_currency" gorm:"size:3;default:'USD'"`
	Schedule               string     `json:"schedule" gorm:"not null" validate:"required,oneof=Monthly Annual Weekly Daily Quarterly"`
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
	ScheduleInterval       int        `json:"schedule_interval" gorm:"default:1"`
	DateCalculationVersion int        `json:"date_calculation_version" gorm:"default:1"`
	LastReminderSent       *time.Time `json:"last_reminder_sent" gorm:""` // Tracks when the last reminder was sent
	LastReminderRenewalDate *time.Time `json:"last_reminder_renewal_date" gorm:""` // Tracks which renewal date the last reminder was for
	LastCancellationReminderSent *time.Time `json:"last_cancellation_reminder_sent" gorm:""` // Tracks when the last cancellation reminder was sent
	LastCancellationReminderDate *time.Time `json:"last_cancellation_reminder_date" gorm:""` // Tracks which cancellation date the last reminder was for
	CreatedAt              time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt              time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
}

func (s *Subscription) effectiveInterval() int {
	if s.ScheduleInterval <= 0 {
		return 1
	}
	return s.ScheduleInterval
}

// DisplaySchedule returns a human-friendly schedule label
func (s *Subscription) DisplaySchedule() string {
	interval := s.effectiveInterval()
	if interval == 1 {
		return s.Schedule
	}
	unit := map[string]string{
		"Daily": "Days", "Weekly": "Weeks", "Monthly": "Months",
		"Quarterly": "Quarters", "Annual": "Years",
	}
	if u, ok := unit[s.Schedule]; ok {
		return fmt.Sprintf("Every %d %s", interval, u)
	}
	return s.Schedule
}

// AnnualCost calculates the annual cost based on schedule
func (s *Subscription) AnnualCost() float64 {
	interval := s.effectiveInterval()
	switch s.Schedule {
	case "Annual":
		return s.Cost / float64(interval)
	case "Quarterly":
		return s.Cost * 4 / float64(interval)
	case "Monthly":
		return s.Cost * 12 / float64(interval)
	case "Weekly":
		return s.Cost * 52 / float64(interval)
	case "Daily":
		return s.Cost * 365 / float64(interval)
	default:
		return s.Cost * 12 / float64(interval)
	}
}

// MonthlyCost calculates the monthly cost based on schedule
func (s *Subscription) MonthlyCost() float64 {
	interval := s.effectiveInterval()
	switch s.Schedule {
	case "Annual":
		return s.Cost / (12 * float64(interval))
	case "Quarterly":
		return s.Cost / (3 * float64(interval))
	case "Monthly":
		return s.Cost / float64(interval)
	case "Weekly":
		return s.Cost * 4.33 / float64(interval)
	case "Daily":
		return s.Cost * 30.44 / float64(interval)
	default:
		return s.Cost / float64(interval)
	}
}

// DailyCost calculates the daily cost
func (s *Subscription) DailyCost() float64 {
	return s.MonthlyCost() / 30.44 // Average days per month
}

// IsHighCost determines if this is a high-cost subscription based on the threshold
func (s *Subscription) IsHighCost(threshold float64) bool {
	return s.MonthlyCost() > threshold
}

// BeforeCreate hook to set renewal date for active subscriptions
func (s *Subscription) BeforeCreate(tx *gorm.DB) error {
	if s.Status == "Active" && s.RenewalDate == nil {
		// Set renewal date based on schedule
		s.calculateNextRenewalDate()
	}
	return nil
}

// AfterFind hook to auto-update renewal date if it has passed (Issue #29)
// This ensures renewal dates are automatically updated when subscriptions are loaded
func (s *Subscription) AfterFind(tx *gorm.DB) error {
	// Auto-update renewal date if it has passed and subscription is active
	if s.RenewalDate != nil && s.Status == "Active" && s.ID > 0 {
		now := time.Now()
		if s.RenewalDate.Before(now) || s.RenewalDate.Equal(now) {
			// Renewal date has passed, calculate the next one
			oldRenewalDate := s.RenewalDate
			s.calculateNextRenewalDate()
			
			// Only update if the date actually changed to avoid unnecessary writes
			if s.RenewalDate != nil && !s.RenewalDate.Equal(*oldRenewalDate) {
				// Update only the renewal_date field using UpdateColumn to avoid triggering hooks
				// This prevents infinite recursion and only updates the specific field
				tx.Model(s).UpdateColumn("renewal_date", s.RenewalDate)
			}
		}
	}
	return nil
}

// BeforeUpdate hook to recalculate renewal date when schedule changes, start date changes, or date passes
func (s *Subscription) BeforeUpdate(tx *gorm.DB) error {
	// Get the original values to check for schedule or start date changes
	var original Subscription
	if err := tx.Model(&Subscription{}).Where("id = ?", s.ID).First(&original).Error; err == nil {
		// If schedule changed and status is Active, recalculate renewal date
		// Use start date if available to preserve billing anniversary
		if original.Schedule != s.Schedule && s.Status == "Active" {
			s.calculateNextRenewalDate()
		}

		// If start date changed and status is Active, recalculate renewal date
		// This ensures renewal dates update when start dates are modified
		if s.Status == "Active" {
			startDateChanged := false
			if original.StartDate == nil && s.StartDate != nil {
				// Start date was added
				startDateChanged = true
			} else if original.StartDate != nil && s.StartDate == nil {
				// Start date was removed
				startDateChanged = true
			} else if original.StartDate != nil && s.StartDate != nil {
				// Both exist, check if they're different
				if !original.StartDate.Equal(*s.StartDate) {
					startDateChanged = true
				}
			}

			if startDateChanged {
				s.calculateNextRenewalDate()
			}
		}
	}

	// Calculate if renewal date is nil and status is Active
	if s.RenewalDate == nil && s.Status == "Active" {
		s.calculateNextRenewalDate()
	}

	// Auto-update renewal date if it has passed (Issue #29)
	if s.RenewalDate != nil && s.Status == "Active" {
		now := time.Now()
		if s.RenewalDate.Before(now) || s.RenewalDate.Equal(now) {
			// Renewal date has passed, calculate the next one
			s.calculateNextRenewalDate()
		}
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

	interval := s.effectiveInterval()
	start := carbon.CreateFromStdTime(*s.StartDate)
	now := carbon.Now()

	switch s.Schedule {
	case "Monthly":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddMonthsNoOverflow(interval)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Quarterly":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddMonthsNoOverflow(3 * interval)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Annual":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddYearsNoOverflow(interval)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Weekly":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddWeeks(interval)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	case "Daily":
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddDays(interval)
		}
		renewalDate := current.StdTime()
		s.RenewalDate = &renewalDate

	default:
		current := start.Copy()
		for current.Lte(now) {
			current = current.AddMonthsNoOverflow(interval)
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

	interval := s.effectiveInterval()
	var renewalDate time.Time
	baseDate := *s.StartDate
	now := time.Now()

	switch s.Schedule {
	case "Annual":
		years := interval
		for {
			renewalDate = baseDate.AddDate(years, 0, 0)
			if renewalDate.After(now) {
				break
			}
			years += interval
		}
	case "Quarterly":
		startDay := baseDate.Day()
		startYear := baseDate.Year()
		startMonth := int(baseDate.Month())
		step := 3 * interval
		periods := 1

		for {
			totalMonths := startMonth + (periods * step) - 1
			targetYear := startYear + totalMonths/12
			targetMonth := time.Month((totalMonths % 12) + 1)
			lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, baseDate.Location()).Day()
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
			periods++
		}
	case "Monthly":
		startDay := baseDate.Day()
		startYear := baseDate.Year()
		startMonth := int(baseDate.Month())
		periods := 1

		for {
			totalMonths := startMonth + (periods * interval) - 1
			targetYear := startYear + totalMonths/12
			targetMonth := time.Month((totalMonths % 12) + 1)
			lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, baseDate.Location()).Day()
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
			periods++
		}
	case "Weekly":
		weeks := interval
		for {
			renewalDate = baseDate.AddDate(0, 0, weeks*7)
			if renewalDate.After(now) {
				break
			}
			weeks += interval
		}
	case "Daily":
		days := interval
		for {
			renewalDate = baseDate.AddDate(0, 0, days)
			if renewalDate.After(now) {
				break
			}
			days += interval
		}
	default:
		startDay := baseDate.Day()
		startYear := baseDate.Year()
		startMonth := int(baseDate.Month())
		periods := 1

		for {
			totalMonths := startMonth + (periods * interval) - 1
			targetYear := startYear + totalMonths/12
			targetMonth := time.Month((totalMonths % 12) + 1)
			lastDay := time.Date(targetYear, targetMonth+1, 0, 0, 0, 0, 0, baseDate.Location()).Day()
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
			periods++
		}
	}

	s.RenewalDate = &renewalDate
}

// calculateNextRenewalDateFromNow calculates the next renewal date from current time
func (s *Subscription) calculateNextRenewalDateFromNow() {
	interval := s.effectiveInterval()
	var renewalDate time.Time
	baseDate := time.Now()

	switch s.Schedule {
	case "Annual":
		renewalDate = baseDate.AddDate(interval, 0, 0)
	case "Quarterly":
		renewalDate = baseDate.AddDate(0, 3*interval, 0)
	case "Monthly":
		renewalDate = baseDate.AddDate(0, interval, 0)
	case "Weekly":
		renewalDate = baseDate.AddDate(0, 0, 7*interval)
	case "Daily":
		renewalDate = baseDate.AddDate(0, 0, interval)
	default:
		renewalDate = baseDate.AddDate(0, interval, 0)
	}
	s.RenewalDate = &renewalDate
}

// calculateNextRenewalDateFromNowV2 calculates renewal date from now using Carbon
func (s *Subscription) calculateNextRenewalDateFromNowV2() {
	interval := s.effectiveInterval()
	now := carbon.Now()

	switch s.Schedule {
	case "Annual":
		renewalDate := now.AddYearsNoOverflow(interval).StdTime()
		s.RenewalDate = &renewalDate
	case "Quarterly":
		renewalDate := now.AddMonthsNoOverflow(3 * interval).StdTime()
		s.RenewalDate = &renewalDate
	case "Monthly":
		renewalDate := now.AddMonthsNoOverflow(interval).StdTime()
		s.RenewalDate = &renewalDate
	case "Weekly":
		renewalDate := now.AddWeeks(interval).StdTime()
		s.RenewalDate = &renewalDate
	case "Daily":
		renewalDate := now.AddDays(interval).StdTime()
		s.RenewalDate = &renewalDate
	default:
		renewalDate := now.AddMonthsNoOverflow(interval).StdTime()
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

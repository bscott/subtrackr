package models

import (
	"time"

	"gorm.io/gorm"
)

type Subscription struct {
	ID       uint    `json:"id" gorm:"primaryKey"`
	Name     string  `json:"name" gorm:"not null" validate:"required"`
	Cost     float64 `json:"cost" gorm:"not null" validate:"required,gt=0"`
	Schedule string  `json:"schedule" gorm:"not null" validate:"required,oneof=Monthly Annual Weekly Daily"`
	Status   string  `json:"status" gorm:"not null" validate:"required,oneof=Active Cancelled Paused"`
// (Line removed)
	CategoryID       uint       `json:"category_id" gorm:"not null"`
	Category         Category   `json:"category" gorm:"foreignKey:CategoryID;constraint:OnUpdate:CASCADE,OnDelete:RESTRICT;"`
	PaymentMethod    string     `json:"payment_method" gorm:""`
	Account          string     `json:"account" gorm:""`
	StartDate        *time.Time `json:"start_date" gorm:""`
	RenewalDate      *time.Time `json:"renewal_date" gorm:""`
	CancellationDate *time.Time `json:"cancellation_date" gorm:""`
	URL              string     `json:"url" gorm:""`
	Notes            string     `json:"notes" gorm:""`
	Usage            string     `json:"usage" gorm:"" validate:"omitempty,oneof=High Medium Low"`
	CreatedAt        time.Time  `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt        time.Time  `json:"updated_at" gorm:"autoUpdateTime"`
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
		var renewalDate time.Time
		switch s.Schedule {
		case "Annual":
			renewalDate = time.Now().AddDate(1, 0, 0)
		case "Monthly":
			renewalDate = time.Now().AddDate(0, 1, 0)
		case "Weekly":
			renewalDate = time.Now().AddDate(0, 0, 7)
		case "Daily":
			renewalDate = time.Now().AddDate(0, 0, 1)
		default:
			renewalDate = time.Now().AddDate(0, 1, 0)
		}
		s.RenewalDate = &renewalDate
	}
	return nil
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

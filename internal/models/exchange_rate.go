package models

import (
	"time"
)

// ExchangeRate represents currency exchange rate data
type ExchangeRate struct {
	ID           uint      `json:"id" gorm:"primaryKey"`
	BaseCurrency string    `json:"base_currency" gorm:"size:3;not null"`
	Currency     string    `json:"currency" gorm:"size:3;not null"`
	Rate         float64   `json:"rate" gorm:"not null"`
	Date         time.Time `json:"date" gorm:"not null"`
	CreatedAt    time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// IsStale checks if the exchange rate is older than 24 hours
func (er *ExchangeRate) IsStale() bool {
	return time.Since(er.Date) > 24*time.Hour
}
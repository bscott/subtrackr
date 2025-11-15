package models

import (
	"time"
)

// Settings represents application settings
type Settings struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Key       string    `json:"key" gorm:"uniqueIndex;not null"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt time.Time `json:"updated_at" gorm:"autoUpdateTime"`
}

// SMTPConfig represents SMTP configuration
type SMTPConfig struct {
	Host     string `json:"smtp_host"`
	Port     int    `json:"smtp_port"`
	Username string `json:"smtp_username"`
	Password string `json:"smtp_password"`
	From     string `json:"smtp_from"`
	FromName string `json:"smtp_from_name"`
	To       string `json:"smtp_to"` // Recipient email address for notifications
}

// NotificationSettings represents notification preferences
type NotificationSettings struct {
	RenewalReminders  bool    `json:"renewal_reminders"`
	HighCostAlerts    bool    `json:"high_cost_alerts"`
	HighCostThreshold float64 `json:"high_cost_threshold"`
	ReminderDays      int     `json:"reminder_days"`
}

// APIKey represents an API key for external access
type APIKey struct {
	ID          uint      `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"not null"`
	Key         string    `json:"key" gorm:"uniqueIndex;not null"`
	LastUsed    *time.Time `json:"last_used"`
	UsageCount  int       `json:"usage_count" gorm:"default:0"`
	CreatedAt   time.Time `json:"created_at" gorm:"autoCreateTime"`
	UpdatedAt   time.Time `json:"updated_at" gorm:"autoUpdateTime"`
	IsNew       bool      `json:"is_new" gorm:"-"` // Not stored in DB, just for display
}
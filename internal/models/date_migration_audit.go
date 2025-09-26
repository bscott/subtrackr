package models

import (
	"time"

	"gorm.io/gorm"
)

// DateMigrationLog tracks changes made during date calculation migrations
type DateMigrationLog struct {
	ID             uint      `json:"id" gorm:"primaryKey"`
	SubscriptionID uint      `json:"subscription_id" gorm:"not null"`
	OldVersion     int       `json:"old_version" gorm:"not null"`
	NewVersion     int       `json:"new_version" gorm:"not null"`
	OldRenewalDate *time.Time `json:"old_renewal_date"`
	NewRenewalDate *time.Time `json:"new_renewal_date"`
	MigrationReason string    `json:"migration_reason" gorm:"size:255"`
	MigratedAt     time.Time `json:"migrated_at" gorm:"autoCreateTime"`
}

// DateMigrationSafetyCheck provides utilities for safe date calculation migrations
type DateMigrationSafetyCheck struct {
	db *gorm.DB
}

// NewDateMigrationSafetyCheck creates a new migration safety checker
func NewDateMigrationSafetyCheck(db *gorm.DB) *DateMigrationSafetyCheck {
	return &DateMigrationSafetyCheck{db: db}
}

// MigrateSubscriptionToV2 safely migrates a single subscription to V2 date calculation
func (dmsc *DateMigrationSafetyCheck) MigrateSubscriptionToV2(subscriptionID uint, reason string) error {
	// Load the subscription
	var sub Subscription
	if err := dmsc.db.First(&sub, subscriptionID).Error; err != nil {
		return err
	}

	// Skip if already V2
	if sub.DateCalculationVersion == 2 {
		return nil
	}

	// Store original values for audit
	oldVersion := sub.DateCalculationVersion
	oldRenewalDate := sub.RenewalDate

	// Calculate with V2
	sub.DateCalculationVersion = 2
	sub.calculateNextRenewalDate()

	// Create audit log entry
	auditLog := DateMigrationLog{
		SubscriptionID:  subscriptionID,
		OldVersion:      oldVersion,
		NewVersion:      2,
		OldRenewalDate:  oldRenewalDate,
		NewRenewalDate:  sub.RenewalDate,
		MigrationReason: reason,
	}

	// Save both subscription and audit log in transaction
	return dmsc.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		return tx.Create(&auditLog).Error
	})
}

// CompareCalculationVersions compares V1 and V2 calculations without changing data
func (dmsc *DateMigrationSafetyCheck) CompareCalculationVersions(subscriptionID uint) (V1Date, V2Date *time.Time, err error) {
	var sub Subscription
	if err = dmsc.db.First(&sub, subscriptionID).Error; err != nil {
		return nil, nil, err
	}

	// Calculate V1
	subV1 := sub
	subV1.DateCalculationVersion = 1
	subV1.calculateNextRenewalDate()
	V1Date = subV1.RenewalDate

	// Calculate V2
	subV2 := sub
	subV2.DateCalculationVersion = 2
	subV2.calculateNextRenewalDate()
	V2Date = subV2.RenewalDate

	return V1Date, V2Date, nil
}

// BatchMigrateToV2WithAudit migrates all subscriptions to V2 with comprehensive auditing
func (dmsc *DateMigrationSafetyCheck) BatchMigrateToV2WithAudit(dryRun bool) error {
	var subscriptions []Subscription
	if err := dmsc.db.Where("date_calculation_version = 1").Find(&subscriptions).Error; err != nil {
		return err
	}

	for _, sub := range subscriptions {
		// Compare versions first
		v1Date, v2Date, err := dmsc.CompareCalculationVersions(sub.ID)
		if err != nil {
			continue // Skip on error
		}

		// Log significant differences
		if v1Date != nil && v2Date != nil {
			diff := v2Date.Sub(*v1Date).Abs()
			if diff > 7*24*time.Hour { // More than 7 days difference
				auditLog := DateMigrationLog{
					SubscriptionID:  sub.ID,
					OldVersion:      1,
					NewVersion:      2,
					OldRenewalDate:  v1Date,
					NewRenewalDate:  v2Date,
					MigrationReason: "Batch migration - significant difference detected",
				}
				dmsc.db.Create(&auditLog)
			}
		}

		// Perform actual migration if not dry run
		if !dryRun {
			dmsc.MigrateSubscriptionToV2(sub.ID, "Batch migration to V2")
		}
	}

	return nil
}

// RollbackSubscriptionToV1 rolls back a subscription to V1 calculation (emergency rollback)
func (dmsc *DateMigrationSafetyCheck) RollbackSubscriptionToV1(subscriptionID uint, reason string) error {
	// Load the subscription
	var sub Subscription
	if err := dmsc.db.First(&sub, subscriptionID).Error; err != nil {
		return err
	}

	// Skip if already V1
	if sub.DateCalculationVersion == 1 {
		return nil
	}

	// Find the original audit log to restore previous renewal date
	var auditLog DateMigrationLog
	err := dmsc.db.Where("subscription_id = ? AND new_version = ?", subscriptionID, 2).
		Order("migrated_at DESC").First(&auditLog).Error

	oldRenewalDate := sub.RenewalDate

	if err == nil && auditLog.OldRenewalDate != nil {
		// Restore original renewal date if we have audit record
		sub.RenewalDate = auditLog.OldRenewalDate
	} else {
		// Recalculate with V1 if no audit record
		sub.DateCalculationVersion = 1
		sub.calculateNextRenewalDate()
	}

	sub.DateCalculationVersion = 1

	// Create rollback audit log
	rollbackLog := DateMigrationLog{
		SubscriptionID:  subscriptionID,
		OldVersion:      2,
		NewVersion:      1,
		OldRenewalDate:  oldRenewalDate,
		NewRenewalDate:  sub.RenewalDate,
		MigrationReason: "ROLLBACK: " + reason,
	}

	// Save both subscription and audit log in transaction
	return dmsc.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&sub).Error; err != nil {
			return err
		}
		return tx.Create(&rollbackLog).Error
	})
}

// GetMigrationStats returns statistics about date calculation migrations
func (dmsc *DateMigrationSafetyCheck) GetMigrationStats() (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Count subscriptions by version
	var v1Count, v2Count int64
	dmsc.db.Model(&Subscription{}).Where("date_calculation_version = 1").Count(&v1Count)
	dmsc.db.Model(&Subscription{}).Where("date_calculation_version = 2").Count(&v2Count)

	// Count audit logs
	var auditCount int64
	dmsc.db.Model(&DateMigrationLog{}).Count(&auditCount)

	// Count rollbacks
	var rollbackCount int64
	dmsc.db.Model(&DateMigrationLog{}).Where("migration_reason LIKE 'ROLLBACK:%'").Count(&rollbackCount)

	stats["v1_subscriptions"] = v1Count
	stats["v2_subscriptions"] = v2Count
	stats["total_migrations"] = auditCount
	stats["rollbacks"] = rollbackCount

	return stats, nil
}
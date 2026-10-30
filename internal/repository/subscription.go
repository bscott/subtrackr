package repository

import (
	"fmt"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/sortorder"
	"time"

	"gorm.io/gorm"
)

type SubscriptionRepository struct {
	db              *gorm.DB
	hasLegacyColumn *bool
}

func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

func (r *SubscriptionRepository) checkLegacyColumn() bool {
	if r.hasLegacyColumn != nil {
		return *r.hasLegacyColumn
	}

	var exists bool
	r.db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('subscriptions') WHERE name='category'").Scan(&exists)
	r.hasLegacyColumn = &exists
	return exists
}

// categoryColumnValue is what subscriptions.category_id stores for a CategoryID.
// CategoryID 0 means uncategorized and is stored as NULL, which the foreign key
// to categories allows; 0 itself references no row and would be rejected.
func categoryColumnValue(categoryID uint) interface{} {
	if categoryID == 0 {
		return nil
	}
	return categoryID
}

// legacyCategoryName returns the value for the old NOT NULL category text column.
// It reports false when categoryID has no row, leaving the insert or update to
// fail on the foreign key.
func (r *SubscriptionRepository) legacyCategoryName(categoryID uint) (string, bool) {
	if categoryID == 0 {
		return "", true
	}
	var category models.Category
	if err := r.db.First(&category, categoryID).Error; err != nil {
		return "", false
	}
	return category.Name, true
}

// validateCategory also protects legacy tables whose original DDL lacks a FK.
func (r *SubscriptionRepository) validateCategory(id uint) error {
	if id == 0 {
		return nil
	}
	var count int64
	if err := r.db.Model(&models.Category{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("category %d not found", id)
	}
	return nil
}

func (r *SubscriptionRepository) Create(subscription *models.Subscription) (*models.Subscription, error) {
	if err := r.validateCategory(subscription.CategoryID); err != nil {
		return nil, err
	}
	subscription.NormalizeToCancel()
	// Check if the old category column exists (for legacy schema support)
	columnExists := r.checkLegacyColumn()

	if columnExists {
		// For legacy schema, we need to populate the old category column
		if categoryName, ok := r.legacyCategoryName(subscription.CategoryID); ok {
			// Use transaction for thread safety
			err := r.db.Transaction(func(tx *gorm.DB) error {
				result := tx.Exec(`
					INSERT INTO subscriptions (
						name, label, cost, schedule, schedule_interval, share_count, status, category_id, category, original_currency,
						payment_method, autopay, account, start_date, renewal_date,
						cancellation_date, cancellation_notice_days, url, icon_url, notes, usage, reminder_enabled,
						to_cancel, to_cancel_reminder_days, last_to_cancel_reminder_sent, last_to_cancel_reminder_anchor,
						date_calculation_version, created_at, updated_at
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					subscription.Name, subscription.Label, subscription.Cost, subscription.Schedule, subscription.ScheduleInterval, subscription.ShareCount,
					subscription.Status, categoryColumnValue(subscription.CategoryID), categoryName, subscription.OriginalCurrency,
					subscription.PaymentMethod, subscription.Autopay, subscription.Account,
					subscription.StartDate, subscription.RenewalDate,
					subscription.CancellationDate, models.ClampCancellationNoticeDays(subscription.CancellationNoticeDays), subscription.URL, subscription.IconURL,
					subscription.Notes, subscription.Usage, subscription.ReminderEnabled,
					subscription.ToCancel, subscription.ToCancelReminderDays, subscription.LastToCancelReminderSent, subscription.LastToCancelReminderAnchor,
					subscription.DateCalculationVersion,
					time.Now(), time.Now())

				if result.Error != nil {
					return result.Error
				}

				// Get the last inserted ID within the transaction
				var lastID int64
				if err := tx.Raw("SELECT last_insert_rowid()").Scan(&lastID).Error; err != nil {
					return err
				}
				subscription.ID = uint(lastID)
				return nil
			})

			if err != nil {
				return nil, err
			}

			return subscription, nil
		}
	}

	// Normal creation for migrated schema. An uncategorized subscription leaves
	// category_id out so it is stored as NULL; a Category without an ID is still
	// saved by GORM, which fills in the reference.
	tx := r.db
	if subscription.CategoryID == 0 && subscription.Category.ID == 0 && subscription.Category.Name == "" {
		tx = tx.Omit("category_id")
	}
	if err := tx.Create(subscription).Error; err != nil {
		return nil, err
	}
	return subscription, nil
}

func (r *SubscriptionRepository) GetAll() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Preload("Tags").Order("created_at DESC").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

// GetAllSorted returns all subscriptions sorted by the ordered list of criteria.
func (r *SubscriptionRepository) GetAllSorted(rules []sortorder.Rule) ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	query := r.db.Preload("Category").Preload("Tags")

	// Validate and set sort column
	validSortColumns := map[string]string{
		"name":         "subscriptions.name",
		"cost":         "subscriptions.cost",
		"status":       "subscriptions.status",
		"renewal_date": "subscriptions.renewal_date",
		"schedule":     "subscriptions.schedule",
		"category":     "categories.name",
		"created_at":   "subscriptions.created_at",
	}

	rules = sortorder.Normalize(rules)
	if len(rules) == 0 {
		rules = []sortorder.Rule{{Field: "created_at", Direction: "desc"}}
	}

	orderColumns := make([]string, 0, len(rules))
	needsCategoryJoin := false
	for _, rule := range rules {
		sortColumn, ok := validSortColumns[rule.Field]
		if !ok {
			continue
		}
		orderColumns = append(orderColumns, sortColumn+" "+strings.ToUpper(rule.Direction))
		if rule.Field == "category" {
			needsCategoryJoin = true
		}
	}
	if len(orderColumns) == 0 {
		orderColumns = append(orderColumns, "subscriptions.created_at DESC")
	}

	if needsCategoryJoin {
		query = query.Joins("LEFT JOIN categories ON subscriptions.category_id = categories.id")
	}

	if err := query.Order(strings.Join(orderColumns, ", ")).Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetByID(id uint) (*models.Subscription, error) {
	var subscription models.Subscription
	if err := r.db.Preload("Category").Preload("Tags").First(&subscription, id).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (r *SubscriptionRepository) Update(id uint, subscription *models.Subscription) (*models.Subscription, error) {
	if err := r.validateCategory(subscription.CategoryID); err != nil {
		return nil, err
	}
	// First, get the existing subscription
	var existing models.Subscription
	if err := r.db.First(&existing, id).Error; err != nil {
		return nil, err
	}

	// Check if the old category column exists
	columnExists := r.checkLegacyColumn()

	// Remember what the to-cancel reminder state was earned against before the
	// fields below change.
	previous := existing

	// Update the existing subscription with new values
	existing.Name = subscription.Name
	existing.Label = subscription.Label
	existing.Cost = subscription.Cost
	existing.Schedule = subscription.Schedule
	existing.ScheduleInterval = subscription.ScheduleInterval
	existing.ShareCount = subscription.ShareCount
	existing.Status = subscription.Status
	existing.CategoryID = subscription.CategoryID
	existing.OriginalCurrency = subscription.OriginalCurrency
	existing.PaymentMethod = subscription.PaymentMethod
	existing.Autopay = subscription.Autopay
	existing.Account = subscription.Account
	existing.StartDate = subscription.StartDate
	existing.LastReminderSent = subscription.LastReminderSent
	existing.LastReminderRenewalDate = subscription.LastReminderRenewalDate
	existing.LastReminderWindow = subscription.LastReminderWindow
	existing.LastCancellationReminderSent = subscription.LastCancellationReminderSent
	existing.LastCancellationReminderDate = subscription.LastCancellationReminderDate
	existing.LastCancellationReminderWindow = subscription.LastCancellationReminderWindow
	existing.RenewalDate = subscription.RenewalDate
	existing.CancellationDate = subscription.CancellationDate
	existing.CancellationNoticeDays = subscription.CancellationNoticeDays
	existing.URL = subscription.URL
	existing.IconURL = subscription.IconURL
	existing.Notes = subscription.Notes
	existing.Usage = subscription.Usage
	existing.ReminderEnabled = subscription.ReminderEnabled
	existing.ToCancel = subscription.ToCancel
	existing.ToCancelReminderDays = subscription.ToCancelReminderDays
	existing.NormalizeToCancel()

	// The delivered-reminder state always comes from the row just read, not from
	// the caller: only the scheduler records a delivery, so a form that loaded the
	// subscription before a delivery cannot erase it. Relevant edits clear it here.
	if toCancelReminderChanged(&previous, &existing) {
		existing.LastToCancelReminderSent = nil
		existing.LastToCancelReminderAnchor = nil
	}

	if columnExists {
		// For legacy schema, we need to update the old category column too
		if categoryName, ok := r.legacyCategoryName(subscription.CategoryID); ok {
			// We need to manually set the category name for legacy schema
			updates := map[string]interface{}{
				"name":                              existing.Name,
				"label":                             existing.Label,
				"cost":                              existing.Cost,
				"schedule":                          existing.Schedule,
				"schedule_interval":                 existing.ScheduleInterval,
				"share_count":                       existing.ShareCount,
				"status":                            existing.Status,
				"category_id":                       categoryColumnValue(existing.CategoryID),
				"category":                          categoryName,
				"original_currency":                 existing.OriginalCurrency,
				"payment_method":                    existing.PaymentMethod,
				"autopay":                           existing.Autopay,
				"account":                           existing.Account,
				"start_date":                        existing.StartDate,
				"renewal_date":                      existing.RenewalDate,
				"cancellation_date":                 existing.CancellationDate,
				"cancellation_notice_days":          models.ClampCancellationNoticeDays(existing.CancellationNoticeDays),
				"url":                               existing.URL,
				"icon_url":                          existing.IconURL,
				"notes":                             existing.Notes,
				"usage":                             existing.Usage,
				"last_reminder_sent":                existing.LastReminderSent,
				"last_reminder_renewal_date":        existing.LastReminderRenewalDate,
				"last_reminder_window":              existing.LastReminderWindow,
				"reminder_enabled":                  existing.ReminderEnabled,
				"last_cancellation_reminder_sent":   existing.LastCancellationReminderSent,
				"last_cancellation_reminder_date":   existing.LastCancellationReminderDate,
				"last_cancellation_reminder_window": existing.LastCancellationReminderWindow,
				"to_cancel":                         existing.ToCancel,
				"to_cancel_reminder_days":           existing.ToCancelReminderDays,
				"last_to_cancel_reminder_sent":      existing.LastToCancelReminderSent,
				"last_to_cancel_reminder_anchor":    existing.LastToCancelReminderAnchor,
				"updated_at":                        time.Now(),
			}
			if err := r.db.Model(&existing).Where("id = ?", id).Updates(updates).Error; err != nil {
				return nil, err
			}
			return r.GetByID(id)
		}
	}

	// The existing record already has the correct ID from the First() query above
	// Use Save which will update only the record with matching primary key
	// This also properly triggers the BeforeUpdate hook
	// Save would write CategoryID 0 literally, so an uncategorized subscription
	// sets category_id to NULL separately, in the same transaction.
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if existing.CategoryID != 0 {
			return tx.Save(&existing).Error
		}
		if err := tx.Omit("category_id").Save(&existing).Error; err != nil {
			return err
		}
		return tx.Model(&models.Subscription{}).Where("id = ?", id).UpdateColumn("category_id", nil).Error
	})
	if err != nil {
		return nil, err
	}

	// Reload to get any changes from hooks
	return r.GetByID(id)
}

// toCancelReminderChanged reports whether an edit changed anything a delivered
// to-cancel reminder was based on, so the next run should consider it afresh.
func toCancelReminderChanged(before, after *models.Subscription) bool {
	if before.ToCancel != after.ToCancel || before.ToCancelReminderDays != after.ToCancelReminderDays {
		return true
	}
	beforeDeadline, afterDeadline := before.ToCancelDeadline(), after.ToCancelDeadline()
	if beforeDeadline == nil || afterDeadline == nil {
		return beforeDeadline != afterDeadline
	}
	return !beforeDeadline.Equal(*afterDeadline)
}

// The Mark*Sent methods record a delivered reminder without touching any other
// column, and only while the row still matches what the reminder was sent for.
// A pass can take a while when providers respond slowly, so a user may have
// edited the subscription since it was read. They report false when an edit got
// there first: the state stays unmarked and the next run decides afresh.
//
// Dates are compared with julianday because the same instant can be stored in
// more than one text format; NULL matches NULL through IS.

// MarkRenewalReminderSent records a delivered renewal reminder.
func (r *SubscriptionRepository) MarkRenewalReminderSent(sub *models.Subscription, window int, sentAt time.Time) (bool, error) {
	result := r.db.Model(&models.Subscription{}).
		Where("id = ? AND status = ? AND reminder_enabled = ? AND to_cancel = ? AND cancellation_notice_days = ? AND julianday(renewal_date) IS julianday(?)",
			sub.ID, "Active", true, false, sub.CancellationNoticeDays, sub.RenewalDate).
		UpdateColumns(map[string]interface{}{
			"last_reminder_sent":         sentAt,
			"last_reminder_renewal_date": sub.RenewalDate,
			"last_reminder_window":       window,
		})
	return result.RowsAffected > 0, result.Error
}

// MarkCancellationReminderSent records a delivered cancellation reminder.
func (r *SubscriptionRepository) MarkCancellationReminderSent(sub *models.Subscription, window int, sentAt time.Time) (bool, error) {
	result := r.db.Model(&models.Subscription{}).
		Where("id = ? AND status = ? AND reminder_enabled = ? AND julianday(cancellation_date) IS julianday(?)",
			sub.ID, "Cancelled", true, sub.CancellationDate).
		UpdateColumns(map[string]interface{}{
			"last_cancellation_reminder_sent":   sentAt,
			"last_cancellation_reminder_date":   sub.CancellationDate,
			"last_cancellation_reminder_window": window,
		})
	return result.RowsAffected > 0, result.Error
}

// MarkToCancelReminderSent records a delivered to-cancel reminder for deadline.
func (r *SubscriptionRepository) MarkToCancelReminderSent(sub *models.Subscription, deadline, sentAt time.Time) (bool, error) {
	result := r.db.Model(&models.Subscription{}).
		Where("id = ? AND to_cancel = ? AND status IN (?, ?) AND to_cancel_reminder_days = ? AND cancellation_notice_days = ? AND julianday(renewal_date) IS julianday(?)",
			sub.ID, true, "Active", "Trial", sub.ToCancelReminderDays, sub.CancellationNoticeDays, sub.RenewalDate).
		UpdateColumns(map[string]interface{}{
			"last_to_cancel_reminder_sent":   sentAt,
			"last_to_cancel_reminder_anchor": deadline,
		})
	return result.RowsAffected > 0, result.Error
}

// Delete removes a subscription and its tag links together. The tags themselves
// stay: other subscriptions may share them. The links are removed explicitly
// because the join table GORM creates has no ON DELETE CASCADE.
func (r *SubscriptionRepository) Delete(id uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM subscription_tags WHERE subscription_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&models.Subscription{}, id).Error
	})
}

func (r *SubscriptionRepository) GetActiveSubscriptions() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Preload("Tags").Where("status = ?", "Active").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

// GetToCancelSubscriptions returns subscriptions opted in to to-cancel reminders
// in a status that can still be cancelled.
func (r *SubscriptionRepository) GetToCancelSubscriptions() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Preload("Tags").Where("to_cancel = ? AND status IN (?, ?)", true, "Active", "Trial").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetCancelledSubscriptions() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Preload("Tags").Where("status = ?", "Cancelled").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetUpcomingRenewals(days int) ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	endDate := time.Now().AddDate(0, 0, days)

	if err := r.db.Where("status = ? AND renewal_date IS NOT NULL AND renewal_date BETWEEN ? AND ?",
		"Active", time.Now(), endDate).Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetUpcomingCancellations(days int) ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	endDate := time.Now().AddDate(0, 0, days)

	if err := r.db.Where("status = ? AND cancellation_date IS NOT NULL AND cancellation_date BETWEEN ? AND ?",
		"Cancelled", time.Now(), endDate).Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

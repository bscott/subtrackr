package repository

import (
	"subtrackr/internal/models"
	"time"

	"gorm.io/gorm"
)

type SubscriptionRepository struct {
	db *gorm.DB
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

func (r *SubscriptionRepository) Create(subscription *models.Subscription) (*models.Subscription, error) {
	// Check if the old category column exists (for legacy schema support)
	columnExists := r.checkLegacyColumn()
	
	if columnExists && subscription.CategoryID > 0 {
		// For legacy schema, we need to populate the old category column
		var category models.Category
		if err := r.db.First(&category, subscription.CategoryID).Error; err == nil {
			// Use transaction for thread safety
			err := r.db.Transaction(func(tx *gorm.DB) error {
				result := tx.Exec(`
					INSERT INTO subscriptions (
						name, cost, schedule, status, category_id, category, original_currency,
						payment_method, account, start_date, renewal_date,
						cancellation_date, url, notes, usage, created_at, updated_at
					) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					subscription.Name, subscription.Cost, subscription.Schedule,
					subscription.Status, subscription.CategoryID, category.Name, subscription.OriginalCurrency,
					subscription.PaymentMethod, subscription.Account,
					subscription.StartDate, subscription.RenewalDate,
					subscription.CancellationDate, subscription.URL,
					subscription.Notes, subscription.Usage,
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
	
	// Normal creation for migrated schema
	if err := r.db.Create(subscription).Error; err != nil {
		return nil, err
	}
	return subscription, nil
}

func (r *SubscriptionRepository) GetAll() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Order("created_at DESC").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetByID(id uint) (*models.Subscription, error) {
	var subscription models.Subscription
	if err := r.db.Preload("Category").First(&subscription, id).Error; err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (r *SubscriptionRepository) Update(id uint, subscription *models.Subscription) (*models.Subscription, error) {
	// First, get the existing subscription
	var existing models.Subscription
	if err := r.db.First(&existing, id).Error; err != nil {
		return nil, err
	}

	// Check if the old category column exists
	columnExists := r.checkLegacyColumn()

	// Update the existing subscription with new values
	existing.Name = subscription.Name
	existing.Cost = subscription.Cost
	existing.Schedule = subscription.Schedule
	existing.Status = subscription.Status
	existing.CategoryID = subscription.CategoryID
	existing.OriginalCurrency = subscription.OriginalCurrency
	existing.PaymentMethod = subscription.PaymentMethod
	existing.Account = subscription.Account
	existing.StartDate = subscription.StartDate
	existing.RenewalDate = subscription.RenewalDate
	existing.CancellationDate = subscription.CancellationDate
	existing.URL = subscription.URL
	existing.Notes = subscription.Notes
	existing.Usage = subscription.Usage

	if columnExists && subscription.CategoryID > 0 {
		// For legacy schema, we need to update the old category column too
		var category models.Category
		if err := r.db.First(&category, subscription.CategoryID).Error; err == nil {
			// We need to manually set the category name for legacy schema
			updates := map[string]interface{}{
				"name":               existing.Name,
				"cost":               existing.Cost,
				"schedule":           existing.Schedule,
				"status":             existing.Status,
				"category_id":        existing.CategoryID,
				"category":           category.Name,
				"original_currency":  existing.OriginalCurrency,
				"payment_method":     existing.PaymentMethod,
				"account":            existing.Account,
				"start_date":         existing.StartDate,
				"renewal_date":       existing.RenewalDate,
				"cancellation_date":  existing.CancellationDate,
				"url":                existing.URL,
				"notes":              existing.Notes,
				"usage":              existing.Usage,
				"updated_at":         time.Now(),
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
	if err := r.db.Save(&existing).Error; err != nil {
		return nil, err
	}

	// Reload to get any changes from hooks
	return r.GetByID(id)
}

func (r *SubscriptionRepository) Delete(id uint) error {
	return r.db.Delete(&models.Subscription{}, id).Error
}

func (r *SubscriptionRepository) Count() int64 {
	var count int64
	r.db.Model(&models.Subscription{}).Count(&count)
	return count
}

func (r *SubscriptionRepository) GetActiveSubscriptions() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Where("status = ?", "Active").Find(&subscriptions).Error; err != nil {
		return nil, err
	}
	return subscriptions, nil
}

func (r *SubscriptionRepository) GetCancelledSubscriptions() ([]models.Subscription, error) {
	var subscriptions []models.Subscription
	if err := r.db.Preload("Category").Where("status = ?", "Cancelled").Find(&subscriptions).Error; err != nil {
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

func (r *SubscriptionRepository) GetCategoryStats() ([]models.CategoryStat, error) {
	var stats []models.CategoryStat
	if err := r.db.Table("subscriptions").
		Select("categories.name as category, SUM(CASE WHEN subscriptions.schedule = 'Annual' THEN subscriptions.cost/12 WHEN subscriptions.schedule = 'Monthly' THEN subscriptions.cost WHEN subscriptions.schedule = 'Weekly' THEN subscriptions.cost*4.33 WHEN subscriptions.schedule = 'Daily' THEN subscriptions.cost*30.44 ELSE subscriptions.cost END) as amount, COUNT(*) as count").
		Joins("left join categories on subscriptions.category_id = categories.id").
		Where("subscriptions.status = ?", "Active").
		Group("categories.name").
		Scan(&stats).Error; err != nil {
		return nil, err
	}
	return stats, nil
}

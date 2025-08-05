package repository

import (
	"subtrackr/internal/models"
	"time"

	"gorm.io/gorm"
)

type SubscriptionRepository struct {
	db *gorm.DB
}

func NewSubscriptionRepository(db *gorm.DB) *SubscriptionRepository {
	return &SubscriptionRepository{db: db}
}

func (r *SubscriptionRepository) Create(subscription *models.Subscription) (*models.Subscription, error) {
	// Check if the old category column exists (for legacy schema support)
	var columnExists bool
	r.db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('subscriptions') WHERE name='category'").Scan(&columnExists)
	
	if columnExists && subscription.CategoryID > 0 {
		// For legacy schema, we need to populate the old category column
		var category models.Category
		if err := r.db.First(&category, subscription.CategoryID).Error; err == nil {
			// Use raw SQL to insert with the old category column
			result := r.db.Exec(`
				INSERT INTO subscriptions (
					name, cost, schedule, status, category_id, category,
					payment_method, account, start_date, renewal_date, 
					cancellation_date, url, notes, usage, created_at, updated_at
				) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				subscription.Name, subscription.Cost, subscription.Schedule, 
				subscription.Status, subscription.CategoryID, category.Name,
				subscription.PaymentMethod, subscription.Account, 
				subscription.StartDate, subscription.RenewalDate,
				subscription.CancellationDate, subscription.URL, 
				subscription.Notes, subscription.Usage,
				time.Now(), time.Now())
			
			if result.Error != nil {
				return nil, result.Error
			}
			
			// Get the last inserted ID
			var lastID int64
			r.db.Raw("SELECT last_insert_rowid()").Scan(&lastID)
			subscription.ID = uint(lastID)
			
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
	// Check if the old category column exists
	var columnExists bool
	r.db.Raw("SELECT COUNT(*) > 0 FROM pragma_table_info('subscriptions') WHERE name='category'").Scan(&columnExists)
	
	if columnExists && subscription.CategoryID > 0 {
		// For legacy schema, we need to update the old category column too
		var category models.Category
		if err := r.db.First(&category, subscription.CategoryID).Error; err == nil {
			// Update with the category name
			updates := map[string]interface{}{
				"name":               subscription.Name,
				"cost":               subscription.Cost,
				"schedule":           subscription.Schedule,
				"status":             subscription.Status,
				"category_id":        subscription.CategoryID,
				"category":           category.Name,
				"payment_method":     subscription.PaymentMethod,
				"account":            subscription.Account,
				"start_date":         subscription.StartDate,
				"renewal_date":       subscription.RenewalDate,
				"cancellation_date":  subscription.CancellationDate,
				"url":                subscription.URL,
				"notes":              subscription.Notes,
				"usage":              subscription.Usage,
				"updated_at":         time.Now(),
			}
			if err := r.db.Model(&models.Subscription{}).Where("id = ?", id).Updates(updates).Error; err != nil {
				return nil, err
			}
			return r.GetByID(id)
		}
	}
	
	// Normal update for migrated schema
	if err := r.db.Model(&models.Subscription{}).Where("id = ?", id).Updates(subscription).Error; err != nil {
		return nil, err
	}
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

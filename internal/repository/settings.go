package repository

import (
	"subtrackr/internal/models"
	"time"

	"gorm.io/gorm"
)

type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

// Set stores or updates a setting
func (r *SettingsRepository) Set(key, value string) error {
	var setting models.Settings
	
	// Try to find existing setting
	err := r.db.Where("key = ?", key).First(&setting).Error
	if err == gorm.ErrRecordNotFound {
		// Create new setting
		setting = models.Settings{
			Key:   key,
			Value: value,
		}
		return r.db.Create(&setting).Error
	} else if err != nil {
		return err
	}
	
	// Update existing setting
	setting.Value = value
	return r.db.Save(&setting).Error
}

// Get retrieves a setting value
func (r *SettingsRepository) Get(key string) (string, error) {
	var setting models.Settings
	err := r.db.Where("key = ?", key).First(&setting).Error
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

// Delete removes a setting
func (r *SettingsRepository) Delete(key string) error {
	return r.db.Where("key = ?", key).Delete(&models.Settings{}).Error
}

// GetAll retrieves all settings
func (r *SettingsRepository) GetAll() ([]models.Settings, error) {
	var settings []models.Settings
	err := r.db.Find(&settings).Error
	return settings, err
}

// CreateAPIKey creates a new API key
func (r *SettingsRepository) CreateAPIKey(apiKey *models.APIKey) (*models.APIKey, error) {
	if err := r.db.Create(apiKey).Error; err != nil {
		return nil, err
	}
	return apiKey, nil
}

// GetAllAPIKeys retrieves all API keys
func (r *SettingsRepository) GetAllAPIKeys() ([]models.APIKey, error) {
	var keys []models.APIKey
	err := r.db.Order("created_at DESC").Find(&keys).Error
	return keys, err
}

// GetAPIKeyByKey retrieves an API key by its key value
func (r *SettingsRepository) GetAPIKeyByKey(key string) (*models.APIKey, error) {
	var apiKey models.APIKey
	err := r.db.Where("key = ?", key).First(&apiKey).Error
	if err != nil {
		return nil, err
	}
	return &apiKey, nil
}

// DeleteAPIKey deletes an API key
func (r *SettingsRepository) DeleteAPIKey(id uint) error {
	return r.db.Delete(&models.APIKey{}, id).Error
}

// UpdateAPIKeyUsage updates the usage stats for an API key
func (r *SettingsRepository) UpdateAPIKeyUsage(id uint) error {
	now := time.Now()
	return r.db.Model(&models.APIKey{}).Where("id = ?", id).Updates(map[string]interface{}{
		"last_used": now,
		"usage_count": gorm.Expr("usage_count + ?", 1),
	}).Error
}
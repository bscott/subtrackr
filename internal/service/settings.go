package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

type SettingsService struct {
	repo *repository.SettingsRepository
}

func NewSettingsService(repo *repository.SettingsRepository) *SettingsService {
	return &SettingsService{repo: repo}
}

// SaveSMTPConfig saves SMTP configuration
func (s *SettingsService) SaveSMTPConfig(config *models.SMTPConfig) error {
	// Convert to JSON
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	
	return s.repo.Set("smtp_config", string(data))
}

// GetSMTPConfig retrieves SMTP configuration
func (s *SettingsService) GetSMTPConfig() (*models.SMTPConfig, error) {
	data, err := s.repo.Get("smtp_config")
	if err != nil {
		return nil, err
	}
	
	var config models.SMTPConfig
	err = json.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, err
	}
	
	return &config, nil
}

// SetBoolSetting saves a boolean setting
func (s *SettingsService) SetBoolSetting(key string, value bool) error {
	return s.repo.Set(key, fmt.Sprintf("%t", value))
}

// GetBoolSetting retrieves a boolean setting
func (s *SettingsService) GetBoolSetting(key string, defaultValue bool) (bool, error) {
	value, err := s.repo.Get(key)
	if err != nil {
		return defaultValue, err
	}
	
	return value == "true", nil
}

// GetBoolSettingWithDefault retrieves a boolean setting with default
func (s *SettingsService) GetBoolSettingWithDefault(key string, defaultValue bool) bool {
	value, err := s.GetBoolSetting(key, defaultValue)
	if err != nil {
		return defaultValue
	}
	return value
}

// SetIntSetting saves an integer setting
func (s *SettingsService) SetIntSetting(key string, value int) error {
	return s.repo.Set(key, strconv.Itoa(value))
}

// GetIntSetting retrieves an integer setting
func (s *SettingsService) GetIntSetting(key string, defaultValue int) (int, error) {
	value, err := s.repo.Get(key)
	if err != nil {
		return defaultValue, err
	}
	
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return defaultValue, err
	}
	
	return intValue, nil
}

// GetIntSettingWithDefault retrieves an integer setting with default
func (s *SettingsService) GetIntSettingWithDefault(key string, defaultValue int) int {
	value, err := s.GetIntSetting(key, defaultValue)
	if err != nil {
		return defaultValue
	}
	return value
}

// CreateAPIKey creates a new API key
func (s *SettingsService) CreateAPIKey(name, key string) (*models.APIKey, error) {
	apiKey := &models.APIKey{
		Name: name,
		Key:  key,
	}
	return s.repo.CreateAPIKey(apiKey)
}

// GetAllAPIKeys retrieves all API keys
func (s *SettingsService) GetAllAPIKeys() ([]models.APIKey, error) {
	return s.repo.GetAllAPIKeys()
}

// DeleteAPIKey deletes an API key
func (s *SettingsService) DeleteAPIKey(id uint) error {
	return s.repo.DeleteAPIKey(id)
}

// ValidateAPIKey checks if an API key is valid and updates usage
func (s *SettingsService) ValidateAPIKey(key string) (*models.APIKey, error) {
	apiKey, err := s.repo.GetAPIKeyByKey(key)
	if err != nil {
		return nil, err
	}
	
	// Update usage stats
	err = s.repo.UpdateAPIKeyUsage(apiKey.ID)
	if err != nil {
		return nil, err
	}
	
	return apiKey, nil
}

// SetCurrency saves the currency preference
func (s *SettingsService) SetCurrency(currency string) error {
	// Validate currency
	if currency != "USD" && currency != "EUR" && currency != "PLN" {
		return fmt.Errorf("invalid currency: %s", currency)
	}
	return s.repo.Set("currency", currency)
}

// GetCurrency retrieves the currency preference
func (s *SettingsService) GetCurrency() string {
	currency, err := s.repo.Get("currency")
	if err != nil || currency == "" {
		return "USD" // Default to USD
	}
	return currency
}

// GetCurrencySymbol returns the symbol for the current currency
func (s *SettingsService) GetCurrencySymbol() string {
	currency := s.GetCurrency()
	switch currency {
	case "EUR":
		return "€"
	case "PLN":
		return "zł"
	default:
		return "$"
	}
}

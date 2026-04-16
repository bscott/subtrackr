package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"time"

	"golang.org/x/crypto/bcrypt"
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

// SetFloatSetting saves a float setting
func (s *SettingsService) SetFloatSetting(key string, value float64) error {
	return s.repo.Set(key, fmt.Sprintf("%.2f", value))
}

// GetFloatSetting retrieves a float setting
func (s *SettingsService) GetFloatSetting(key string, defaultValue float64) (float64, error) {
	value, err := s.repo.Get(key)
	if err != nil {
		return defaultValue, err
	}

	floatValue, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return defaultValue, err
	}

	return floatValue, nil
}

// GetTheme retrieves the current theme setting
func (s *SettingsService) GetTheme() (string, error) {
	theme, err := s.repo.Get("theme")
	if err != nil {
		return "default", err
	}
	return theme, nil
}

// SetTheme saves the theme preference
func (s *SettingsService) SetTheme(theme string) error {
	return s.repo.Set("theme", theme)
}

// GetFloatSettingWithDefault retrieves a float setting with default
func (s *SettingsService) GetFloatSettingWithDefault(key string, defaultValue float64) float64 {
	value, err := s.GetFloatSetting(key, defaultValue)
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
	// Validate against known currencies
	if _, ok := currencyInfoMap[currency]; !ok {
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

// CurrencySymbolForCode returns the symbol for a given currency code
func CurrencySymbolForCode(currency string) string {
	return GetCurrencyInfo(currency).Symbol
}

// GetCurrencySymbol returns the symbol for the current currency
func (s *SettingsService) GetCurrencySymbol() string {
	return CurrencySymbolForCode(s.GetCurrency())
}

// SetDateFormat saves the date format preference
func (s *SettingsService) SetDateFormat(format string) error {
	switch format {
	case "MM/DD/YYYY", "DD/MM/YYYY", "YYYY-MM-DD":
		return s.repo.Set("date_format", format)
	default:
		return fmt.Errorf("invalid date format: %s", format)
	}
}

// GetDateFormat retrieves the date format preference
func (s *SettingsService) GetDateFormat() string {
	format, err := s.repo.Get("date_format")
	if err != nil || format == "" {
		return "MM/DD/YYYY"
	}
	return format
}

// GetGoDateFormat returns the Go time format string for the current date format
func (s *SettingsService) GetGoDateFormat() string {
	return DateFormatToGo(s.GetDateFormat())
}

// GetGoDateFormatLong returns the long Go time format string for emails/notifications
func (s *SettingsService) GetGoDateFormatLong() string {
	return DateFormatToGoLong(s.GetDateFormat())
}

// DateFormatToGo converts a date format key to a short Go time format string
func DateFormatToGo(format string) string {
	switch format {
	case "DD/MM/YYYY":
		return "02/01/2006"
	case "YYYY-MM-DD":
		return "2006-01-02"
	default:
		return "01/02/2006"
	}
}

// DateFormatToGoLong converts a date format key to a long Go time format string
func DateFormatToGoLong(format string) string {
	switch format {
	case "DD/MM/YYYY":
		return "2 January 2006"
	case "YYYY-MM-DD":
		return "2006-01-02"
	default:
		return "January 2, 2006"
	}
}

// SetDarkMode saves the dark mode preference
func (s *SettingsService) SetDarkMode(enabled bool) error {
	return s.SetBoolSetting("dark_mode", enabled)
}

// IsDarkModeEnabled returns whether dark mode is enabled
func (s *SettingsService) IsDarkModeEnabled() bool {
	return s.GetBoolSettingWithDefault("dark_mode", false)
}

// Auth-related methods

// IsAuthEnabled returns whether authentication is enabled
func (s *SettingsService) IsAuthEnabled() bool {
	return s.GetBoolSettingWithDefault("auth_enabled", false)
}

// SetAuthEnabled enables or disables authentication
func (s *SettingsService) SetAuthEnabled(enabled bool) error {
	return s.SetBoolSetting("auth_enabled", enabled)
}

// GetAuthUsername returns the configured admin username
func (s *SettingsService) GetAuthUsername() (string, error) {
	return s.repo.Get("auth_username")
}

// SetAuthUsername sets the admin username
func (s *SettingsService) SetAuthUsername(username string) error {
	return s.repo.Set("auth_username", username)
}

// HashPassword hashes a password using bcrypt
func (s *SettingsService) HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// SetAuthPassword hashes and stores the admin password
func (s *SettingsService) SetAuthPassword(password string) error {
	hash, err := s.HashPassword(password)
	if err != nil {
		return err
	}
	return s.repo.Set("auth_password_hash", hash)
}

// ValidatePassword checks if a password matches the stored hash
func (s *SettingsService) ValidatePassword(password string) error {
	hash, err := s.repo.Get("auth_password_hash")
	if err != nil {
		return fmt.Errorf("no password configured")
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GetOrGenerateSessionSecret returns the session secret, generating one if it doesn't exist
func (s *SettingsService) GetOrGenerateSessionSecret() (string, error) {
	secret, err := s.repo.Get("auth_session_secret")
	if err == nil && secret != "" {
		return secret, nil
	}

	// Generate a new 64-byte random secret
	bytes := make([]byte, 64)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	secret = base64.URLEncoding.EncodeToString(bytes)

	// Save it
	if err := s.repo.Set("auth_session_secret", secret); err != nil {
		return "", err
	}

	return secret, nil
}

// SetupAuth sets up authentication with username and password
func (s *SettingsService) SetupAuth(username, password string) error {
	// Set username
	if err := s.SetAuthUsername(username); err != nil {
		return err
	}

	// Set password
	if err := s.SetAuthPassword(password); err != nil {
		return err
	}

	// Generate session secret
	if _, err := s.GetOrGenerateSessionSecret(); err != nil {
		return err
	}

	// Enable auth
	return s.SetAuthEnabled(true)
}

// DisableAuth disables authentication and removes credentials
func (s *SettingsService) DisableAuth() error {
	// Disable auth first
	if err := s.SetAuthEnabled(false); err != nil {
		return err
	}

	// Optionally clear credentials (commented out to allow re-enabling without re-entering)
	// s.repo.Delete("auth_username")
	// s.repo.Delete("auth_password_hash")

	return nil
}

// GenerateResetToken generates a password reset token
func (s *SettingsService) GenerateResetToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(bytes)

	// Store token with 1-hour expiry
	if err := s.repo.Set("auth_reset_token", token); err != nil {
		return "", err
	}

	expiry := time.Now().Add(1 * time.Hour).Format(time.RFC3339)
	if err := s.repo.Set("auth_reset_token_expiry", expiry); err != nil {
		return "", err
	}

	return token, nil
}

// ValidateResetToken checks if a reset token is valid
func (s *SettingsService) ValidateResetToken(token string) error {
	storedToken, err := s.repo.Get("auth_reset_token")
	if err != nil || subtle.ConstantTimeCompare([]byte(storedToken), []byte(token)) != 1 {
		return fmt.Errorf("invalid token")
	}

	expiryStr, err := s.repo.Get("auth_reset_token_expiry")
	if err != nil {
		return fmt.Errorf("token expired")
	}

	expiry, err := time.Parse(time.RFC3339, expiryStr)
	if err != nil || time.Now().After(expiry) {
		return fmt.Errorf("token expired")
	}

	return nil
}

// ClearResetToken removes the reset token after use
func (s *SettingsService) ClearResetToken() error {
	s.repo.Delete("auth_reset_token")
	s.repo.Delete("auth_reset_token_expiry")
	return nil
}

// GetBaseURL returns the configured base URL for external links, or empty string if not set
func (s *SettingsService) GetBaseURL() string {
	baseURL, err := s.repo.Get("base_url")
	if err != nil {
		return ""
	}
	return baseURL
}

// SetBaseURL saves the base URL setting
func (s *SettingsService) SetBaseURL(baseURL string) error {
	return s.repo.Set("base_url", baseURL)
}

// iCal Subscription methods

// IsICalSubscriptionEnabled returns whether iCal subscription is enabled
func (s *SettingsService) IsICalSubscriptionEnabled() bool {
	return s.GetBoolSettingWithDefault("ical_subscription_enabled", false)
}

// SetICalSubscriptionEnabled enables or disables iCal subscription
func (s *SettingsService) SetICalSubscriptionEnabled(enabled bool) error {
	return s.SetBoolSetting("ical_subscription_enabled", enabled)
}

// GetOrGenerateICalToken returns the iCal token, generating one if it doesn't exist
func (s *SettingsService) GetOrGenerateICalToken() (string, error) {
	token, err := s.repo.Get("ical_subscription_token")
	if err == nil && token != "" {
		return token, nil
	}

	// Generate a new 32-byte random token
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token = base64.URLEncoding.EncodeToString(bytes)

	if err := s.repo.Set("ical_subscription_token", token); err != nil {
		return "", err
	}

	return token, nil
}

// RegenerateICalToken replaces the iCal token with a new one
func (s *SettingsService) RegenerateICalToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	token := base64.URLEncoding.EncodeToString(bytes)

	if err := s.repo.Set("ical_subscription_token", token); err != nil {
		return "", err
	}

	return token, nil
}

// ValidateICalToken checks if a given token matches the stored iCal token
func (s *SettingsService) ValidateICalToken(token string) bool {
	storedToken, err := s.repo.Get("ical_subscription_token")
	if err != nil || storedToken == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(storedToken), []byte(token)) == 1
}

// SavePushoverConfig saves Pushover configuration
func (s *SettingsService) SavePushoverConfig(config *models.PushoverConfig) error {
	// Convert to JSON
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	
	return s.repo.Set("pushover_config", string(data))
}

// GetPushoverConfig retrieves Pushover configuration
func (s *SettingsService) GetPushoverConfig() (*models.PushoverConfig, error) {
	data, err := s.repo.Get("pushover_config")
	if err != nil {
		return nil, err
	}

	var config models.PushoverConfig
	err = json.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

// SaveWebhookConfig saves Webhook configuration
func (s *SettingsService) SaveWebhookConfig(config *models.WebhookConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return s.repo.Set("webhook_config", string(data))
}

// GetWebhookConfig retrieves Webhook configuration
func (s *SettingsService) GetWebhookConfig() (*models.WebhookConfig, error) {
	data, err := s.repo.Get("webhook_config")
	if err != nil {
		return nil, err
	}
	var config models.WebhookConfig
	err = json.Unmarshal([]byte(data), &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

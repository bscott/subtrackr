package repository

import (
	"subtrackr/internal/models"
	"time"

	"gorm.io/gorm"
)

type ExchangeRateRepository struct {
	db *gorm.DB
}

func NewExchangeRateRepository(db *gorm.DB) *ExchangeRateRepository {
	return &ExchangeRateRepository{db: db}
}

// GetRate retrieves the exchange rate for a specific currency pair
func (r *ExchangeRateRepository) GetRate(baseCurrency, targetCurrency string) (*models.ExchangeRate, error) {
	if baseCurrency == targetCurrency {
		// Return rate of 1.0 for same currency
		return &models.ExchangeRate{
			BaseCurrency: baseCurrency,
			Currency:     targetCurrency,
			Rate:         1.0,
			Date:         time.Now(),
		}, nil
	}

	var rate models.ExchangeRate
	err := r.db.Where("base_currency = ? AND currency = ?", baseCurrency, targetCurrency).
		Order("date DESC").
		First(&rate).Error

	if err != nil {
		return nil, err
	}

	return &rate, nil
}

// SaveRates saves multiple exchange rates
func (r *ExchangeRateRepository) SaveRates(rates []models.ExchangeRate) error {
	return r.db.Create(&rates).Error
}

// GetLatestRates retrieves the latest exchange rates for a base currency
func (r *ExchangeRateRepository) GetLatestRates(baseCurrency string) ([]models.ExchangeRate, error) {
	var rates []models.ExchangeRate

	// Get the latest rate for each target currency
	subQuery := r.db.Model(&models.ExchangeRate{}).
		Select("currency, MAX(date) as latest_date").
		Where("base_currency = ?", baseCurrency).
		Group("currency")

	err := r.db.Joins("JOIN (?) as latest ON exchange_rates.currency = latest.currency AND exchange_rates.date = latest.latest_date", subQuery).
		Where("base_currency = ?", baseCurrency).
		Find(&rates).Error

	return rates, err
}

// DeleteStaleRates removes exchange rates older than the specified duration
func (r *ExchangeRateRepository) DeleteStaleRates(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)
	return r.db.Where("date < ?", cutoff).Delete(&models.ExchangeRate{}).Error
}
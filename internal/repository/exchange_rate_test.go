package repository

import (
	"testing"
	"time"

	"subtrackr/internal/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupExchangeRateTestDB(t *testing.T) *ExchangeRateRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ExchangeRate{}))
	return NewExchangeRateRepository(db)
}

func TestExchangeRateRepository_ReplaceRatesDropsOldRowsForBaseOnly(t *testing.T) {
	repo := setupExchangeRateTestDB(t)
	newer := time.Now()
	older := newer.Add(-20 * time.Hour)
	require.NoError(t, repo.SaveRates([]models.ExchangeRate{
		{BaseCurrency: "EUR", Currency: "USD", Rate: 1, Date: newer},
		{BaseCurrency: "EUR", Currency: "BDT", Rate: 130, Date: newer},
		{BaseCurrency: "USD", Currency: "SEK", Rate: 10, Date: newer},
	}))

	// The replacement is older than the rows it replaces, as when switching
	// from an hourly source to a daily one; it must still win.
	require.NoError(t, repo.ReplaceRates("EUR", []models.ExchangeRate{
		{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: older},
	}))

	usd, err := repo.GetRate("EUR", "USD")
	require.NoError(t, err)
	assert.Equal(t, 1.2, usd.Rate)
	_, err = repo.GetRate("EUR", "BDT")
	assert.Error(t, err, "rows the replacement omits are gone")
	other, err := repo.GetRate("USD", "SEK")
	require.NoError(t, err, "rows for another base are untouched")
	assert.Equal(t, 10.0, other.Rate)
}

func TestExchangeRateRepository_ReplaceRatesIsAtomic(t *testing.T) {
	repo := setupExchangeRateTestDB(t)
	require.NoError(t, repo.SaveRates([]models.ExchangeRate{{BaseCurrency: "EUR", Currency: "USD", Rate: 1, Date: time.Now()}}))
	require.NoError(t, repo.db.Exec(`CREATE TRIGGER reject_sek BEFORE INSERT ON exchange_rates
		WHEN NEW.currency = 'SEK' BEGIN SELECT RAISE(ABORT, 'rejected'); END`).Error)

	err := repo.ReplaceRates("EUR", []models.ExchangeRate{
		{BaseCurrency: "EUR", Currency: "GBP", Rate: 0.8, Date: time.Now()},
		{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: time.Now()},
	})

	require.Error(t, err)
	usd, err := repo.GetRate("EUR", "USD")
	require.NoError(t, err, "a failed replacement keeps the old rates")
	assert.Equal(t, 1.0, usd.Rate)
	_, err = repo.GetRate("EUR", "GBP")
	assert.Error(t, err, "and writes none of the new ones")
}

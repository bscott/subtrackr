package service

import (
	"path/filepath"
	"testing"
	"time"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type statsTestServices struct {
	subscriptions *SubscriptionService
	currency      *CurrencyService
	rates         *repository.ExchangeRateRepository
	categories    *CategoryService
}

func newStatsTestServices(t *testing.T) statsTestServices {
	t.Helper()
	t.Setenv("FIXER_API_KEY", "")

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "stats.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.Category{},
		&models.Tag{},
		&models.Subscription{},
		&models.ExchangeRate{},
		&models.Settings{},
	))

	categoryService := NewCategoryService(repository.NewCategoryRepository(db))
	settingsRepo := repository.NewSettingsRepository(db)
	rateRepo := repository.NewExchangeRateRepository(db)

	return statsTestServices{
		subscriptions: NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService),
		currency:      NewCurrencyService(rateRepo, settingsRepo),
		rates:         rateRepo,
		categories:    categoryService,
	}
}

func createStatsCategory(t *testing.T, services statsTestServices, name string) *models.Category {
	t.Helper()
	category, err := services.categories.Create(&models.Category{Name: name})
	require.NoError(t, err)
	return category
}

func createStatsSubscription(t *testing.T, services statsTestServices, subscription models.Subscription) {
	t.Helper()
	_, err := services.subscriptions.Create(&subscription)
	require.NoError(t, err)
}

func seedUSDToSEKRate(t *testing.T, services statsTestServices) {
	t.Helper()
	rateDate := time.Now().Add(-time.Hour)
	require.NoError(t, services.rates.SaveRates([]models.ExchangeRate{
		{BaseCurrency: "EUR", Currency: "USD", Rate: 1.2, Date: rateDate},
		{BaseCurrency: "EUR", Currency: "SEK", Rate: 12, Date: rateDate},
	}))
}

func seedMixedCurrencyStats(t *testing.T, services statsTestServices) {
	t.Helper()
	localCategory := createStatsCategory(t, services, "Local")
	foreignCategory := createStatsCategory(t, services, "Foreign")

	createStatsSubscription(t, services, models.Subscription{
		Name:             "Local active",
		Cost:             20,
		OriginalCurrency: "SEK",
		Schedule:         "Monthly",
		Status:           "Active",
		CategoryID:       localCategory.ID,
	})
	createStatsSubscription(t, services, models.Subscription{
		Name:             "Foreign active",
		Cost:             3,
		OriginalCurrency: "USD",
		Schedule:         "Monthly",
		Status:           "Active",
		CategoryID:       foreignCategory.ID,
	})
	createStatsSubscription(t, services, models.Subscription{
		Name:             "Foreign cancelled",
		Cost:             120,
		OriginalCurrency: "USD",
		Schedule:         "Annual",
		Status:           "Cancelled",
		CategoryID:       foreignCategory.ID,
	})
}

func TestSubscriptionService_GetStats_ConvertsEveryAggregateWhenRatesExist(t *testing.T) {
	services := newStatsTestServices(t)
	seedMixedCurrencyStats(t, services)
	seedUSDToSEKRate(t, services)

	stats, err := services.subscriptions.GetStats(services.currency, "SEK")

	require.NoError(t, err)
	assert.True(t, stats.ConversionComplete)
	assert.InDelta(t, 50, stats.TotalMonthlySpend, 0.001)
	assert.InDelta(t, 600, stats.TotalAnnualSpend, 0.001)
	assert.InDelta(t, 100, stats.MonthlySaved, 0.001)
	assert.InDelta(t, 1200, stats.TotalSaved, 0.001)
	assert.InDelta(t, 20, stats.CategorySpending["Local"], 0.001)
	assert.InDelta(t, 30, stats.CategorySpending["Foreign"], 0.001)
	assert.Equal(t, 2, stats.ActiveSubscriptions)
	assert.Equal(t, 1, stats.CancelledSubscriptions)
}

func TestSubscriptionService_GetStats_GroupsEveryAggregateWhenARateIsMissing(t *testing.T) {
	services := newStatsTestServices(t)
	seedMixedCurrencyStats(t, services)

	stats, err := services.subscriptions.GetStats(services.currency, "SEK")

	require.NoError(t, err)
	assert.False(t, stats.ConversionComplete)
	assert.Zero(t, stats.TotalMonthlySpend)
	assert.Zero(t, stats.TotalAnnualSpend)
	assert.Zero(t, stats.MonthlySaved)
	assert.Zero(t, stats.TotalSaved)
	assert.Empty(t, stats.CategorySpending)

	assert.InDelta(t, 20, stats.TotalsByCurrency["SEK"].TotalMonthlySpend, 0.001)
	assert.InDelta(t, 240, stats.TotalsByCurrency["SEK"].TotalAnnualSpend, 0.001)
	assert.InDelta(t, 3, stats.TotalsByCurrency["USD"].TotalMonthlySpend, 0.001)
	assert.InDelta(t, 36, stats.TotalsByCurrency["USD"].TotalAnnualSpend, 0.001)
	assert.InDelta(t, 10, stats.TotalsByCurrency["USD"].MonthlySaved, 0.001)
	assert.InDelta(t, 120, stats.TotalsByCurrency["USD"].TotalSaved, 0.001)
	assert.InDelta(t, 20, stats.CategorySpendingByCurrency["Local"]["SEK"], 0.001)
	assert.InDelta(t, 3, stats.CategorySpendingByCurrency["Foreign"]["USD"], 0.001)
}

func TestSubscriptionService_GetStats_UsesScheduleIntervalForCategorySpending(t *testing.T) {
	services := newStatsTestServices(t)
	category := createStatsCategory(t, services, "Every other month")
	createStatsSubscription(t, services, models.Subscription{
		Name:             "Interval subscription",
		Cost:             20,
		OriginalCurrency: "SEK",
		Schedule:         "Monthly",
		ScheduleInterval: 2,
		Status:           "Active",
		CategoryID:       category.ID,
	})

	stats, err := services.subscriptions.GetStats(services.currency, "SEK")

	require.NoError(t, err)
	assert.True(t, stats.ConversionComplete)
	assert.InDelta(t, 10, stats.TotalMonthlySpend, 0.001)
	assert.InDelta(t, 120, stats.TotalAnnualSpend, 0.001)
	assert.InDelta(t, 10, stats.CategorySpending["Every other month"], 0.001)
}

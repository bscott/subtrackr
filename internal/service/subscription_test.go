package service

import (
	"testing"
	"time"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestService(t *testing.T) (*SubscriptionService, *gorm.DB) {
	// Create an in-memory SQLite database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate the schema
	err = db.AutoMigrate(&models.Subscription{})
	require.NoError(t, err)

	repo := repository.NewSubscriptionRepository(db)
	service := NewSubscriptionService(repo)

	return service, db
}

func TestSubscriptionService_Create(t *testing.T) {
	service, _ := setupTestService(t)

	subscription := &models.Subscription{
		Name:     "Netflix",
		Cost:     15.99,
		Schedule: "Monthly",
		Status:   "Active",
		Category: "Entertainment",
	}

	result, err := service.Create(subscription)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotZero(t, result.ID)
	assert.Equal(t, "Netflix", result.Name)
	assert.Equal(t, 15.99, result.Cost)
}

func TestSubscriptionService_GetAll(t *testing.T) {
	service, _ := setupTestService(t)

	// Create test data
	subs := []models.Subscription{
		{Name: "Netflix", Cost: 15.99, Schedule: "Monthly", Status: "Active", Category: "Entertainment"},
		{Name: "Spotify", Cost: 9.99, Schedule: "Monthly", Status: "Active", Category: "Entertainment"},
		{Name: "AWS", Cost: 100.00, Schedule: "Monthly", Status: "Active", Category: "Storage"},
	}

	for _, sub := range subs {
		_, err := service.Create(&sub)
		assert.NoError(t, err)
	}

	// Get all subscriptions
	result, err := service.GetAll()
	assert.NoError(t, err)
	assert.Len(t, result, 3)
}

func TestSubscriptionService_GetByID(t *testing.T) {
	service, _ := setupTestService(t)

	// Create a subscription
	sub := &models.Subscription{
		Name:     "Netflix",
		Cost:     15.99,
		Schedule: "Monthly",
		Status:   "Active",
		Category: "Entertainment",
	}

	created, err := service.Create(sub)
	assert.NoError(t, err)

	// Get by ID
	result, err := service.GetByID(created.ID)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, created.ID, result.ID)
	assert.Equal(t, "Netflix", result.Name)
}

func TestSubscriptionService_GetByID_NotFound(t *testing.T) {
	service, _ := setupTestService(t)

	// Try to get non-existent subscription
	result, err := service.GetByID(999)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, gorm.ErrRecordNotFound, err)
}

func TestSubscriptionService_Update(t *testing.T) {
	service, _ := setupTestService(t)

	// Create a subscription
	original := &models.Subscription{
		Name:     "Netflix",
		Cost:     15.99,
		Schedule: "Monthly",
		Status:   "Active",
		Category: "Entertainment",
	}

	created, err := service.Create(original)
	assert.NoError(t, err)

	// Update it
	updateData := &models.Subscription{
		Cost:   19.99,
		Status: "Paused",
	}

	result, err := service.Update(created.ID, updateData)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 19.99, result.Cost)
	assert.Equal(t, "Paused", result.Status)
	assert.Equal(t, "Netflix", result.Name) // Name should not change
}

func TestSubscriptionService_Delete(t *testing.T) {
	service, _ := setupTestService(t)

	// Create a subscription
	sub := &models.Subscription{
		Name:     "Netflix",
		Cost:     15.99,
		Schedule: "Monthly",
		Status:   "Active",
		Category: "Entertainment",
	}

	created, err := service.Create(sub)
	assert.NoError(t, err)

	// Delete it
	err = service.Delete(created.ID)
	assert.NoError(t, err)

	// Verify it's deleted
	result, err := service.GetByID(created.ID)
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestSubscriptionService_Count(t *testing.T) {
	service, _ := setupTestService(t)

	// Initially should be 0
	count := service.Count()
	assert.Equal(t, int64(0), count)

	// Create some subscriptions
	for i := 0; i < 5; i++ {
		_, err := service.Create(&models.Subscription{
			Name:     "Service",
			Cost:     10.00,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Other",
		})
		assert.NoError(t, err)
	}

	// Count should now be 5
	count = service.Count()
	assert.Equal(t, int64(5), count)
}

func TestSubscriptionService_GetStats(t *testing.T) {
	service, _ := setupTestService(t)

	// Create test data
	now := time.Now()
	futureDate := now.AddDate(0, 0, 5) // 5 days from now
	pastDate := now.AddDate(0, 0, -30)  // 30 days ago

	testSubs := []models.Subscription{
		{
			Name:        "Netflix",
			Cost:        15.99,
			Schedule:    "Monthly",
			Status:      "Active",
			Category:    "Entertainment",
			RenewalDate: &futureDate,
		},
		{
			Name:     "Spotify",
			Cost:     9.99,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Entertainment",
		},
		{
			Name:     "AWS",
			Cost:     1200.00,
			Schedule: "Annual",
			Status:   "Active",
			Category: "Storage",
		},
		{
			Name:             "Adobe CC",
			Cost:             54.99,
			Schedule:         "Monthly",
			Status:           "Cancelled",
			Category:         "Productivity",
			CancellationDate: &pastDate,
		},
	}

	for _, sub := range testSubs {
		_, err := service.Create(&sub)
		assert.NoError(t, err)
	}

	// Get stats
	stats, err := service.GetStats()
	assert.NoError(t, err)
	assert.NotNil(t, stats)

	// Verify counts
	assert.Equal(t, 3, stats.ActiveSubscriptions)
	assert.Equal(t, 1, stats.CancelledSubscriptions)
	assert.Equal(t, 1, stats.UpcomingRenewals) // Netflix has renewal in 5 days

	// Verify monthly spend
	// Netflix: 15.99 + Spotify: 9.99 + AWS: 1200/12 = 100
	expectedMonthly := 15.99 + 9.99 + 100.00
	assert.InDelta(t, expectedMonthly, stats.TotalMonthlySpend, 0.01)

	// Verify annual spend
	// Netflix: 15.99*12 + Spotify: 9.99*12 + AWS: 1200
	expectedAnnual := (15.99 * 12) + (9.99 * 12) + 1200.00
	assert.InDelta(t, expectedAnnual, stats.TotalAnnualSpend, 0.01)

	// Verify savings
	// Adobe CC: 54.99 * 12
	assert.InDelta(t, 659.88, stats.TotalSaved, 0.01)
	assert.InDelta(t, 54.99, stats.MonthlySaved, 0.01)

	// Verify category spending
	assert.Equal(t, 2, len(stats.CategorySpending))
	assert.InDelta(t, 25.98, stats.CategorySpending["Entertainment"], 0.01)
	assert.InDelta(t, 100.00, stats.CategorySpending["Storage"], 0.01)
}

func TestSubscriptionService_GetStats_Empty(t *testing.T) {
	service, _ := setupTestService(t)

	// Get stats with no data
	stats, err := service.GetStats()
	assert.NoError(t, err)
	assert.NotNil(t, stats)

	assert.Equal(t, 0, stats.ActiveSubscriptions)
	assert.Equal(t, 0, stats.CancelledSubscriptions)
	assert.Equal(t, 0, stats.UpcomingRenewals)
	assert.Equal(t, 0.0, stats.TotalMonthlySpend)
	assert.Equal(t, 0.0, stats.TotalAnnualSpend)
	assert.Equal(t, 0.0, stats.TotalSaved)
	assert.Equal(t, 0.0, stats.MonthlySaved)
	assert.Empty(t, stats.CategorySpending)
}
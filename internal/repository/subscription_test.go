package repository

import (
	"regexp"
	"testing"
	"time"

	"subtrackr/internal/models"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	sqlDB, mock, err := sqlmock.New()
	require.NoError(t, err)

	// Configure GORM to use the mock database
	gormDB, err := gorm.Open(sqlite.Dialector{
		DriverName: "sqlite3",
		Conn:       sqlDB,
	}, &gorm.Config{})
	require.NoError(t, err)

	return gormDB, mock
}

func TestSubscriptionRepository_Create(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	subscription := &models.Subscription{
		Name:     "Netflix",
		Cost:     15.99,
		Schedule: "Monthly",
		Status:   "Active",
		Category: "Entertainment",
	}

	// Mock the INSERT query
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO "subscriptions"`)).
		WithArgs(
			sqlmock.AnyArg(), // name
			sqlmock.AnyArg(), // cost
			sqlmock.AnyArg(), // schedule
			sqlmock.AnyArg(), // status
			sqlmock.AnyArg(), // category
			sqlmock.AnyArg(), // payment_method
			sqlmock.AnyArg(), // account
			sqlmock.AnyArg(), // start_date
			sqlmock.AnyArg(), // renewal_date
			sqlmock.AnyArg(), // cancellation_date
			sqlmock.AnyArg(), // url
			sqlmock.AnyArg(), // notes
			sqlmock.AnyArg(), // usage
			sqlmock.AnyArg(), // created_at
			sqlmock.AnyArg(), // updated_at
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result, err := repo.Create(subscription)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "Netflix", result.Name)
	assert.Equal(t, 15.99, result.Cost)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetAll(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category", 
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Netflix", 15.99, "Monthly", "Active", "Entertainment", 
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now()).
		AddRow(2, "Spotify", 9.99, "Monthly", "Active", "Entertainment", 
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" ORDER BY created_at DESC`)).
		WillReturnRows(rows)

	subscriptions, err := repo.GetAll()
	assert.NoError(t, err)
	assert.Len(t, subscriptions, 2)
	assert.Equal(t, "Netflix", subscriptions[0].Name)
	assert.Equal(t, "Spotify", subscriptions[1].Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetByID(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category",
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Netflix", 15.99, "Monthly", "Active", "Entertainment",
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE "subscriptions"."id" = ? ORDER BY "subscriptions"."id" LIMIT ?`)).
		WithArgs(1, 1).
		WillReturnRows(rows)

	subscription, err := repo.GetByID(1)
	assert.NoError(t, err)
	assert.NotNil(t, subscription)
	assert.Equal(t, uint(1), subscription.ID)
	assert.Equal(t, "Netflix", subscription.Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetByID_NotFound(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE "subscriptions"."id" = ? ORDER BY "subscriptions"."id" LIMIT ?`)).
		WithArgs(999, 1).
		WillReturnError(gorm.ErrRecordNotFound)

	subscription, err := repo.GetByID(999)
	assert.Error(t, err)
	assert.Nil(t, subscription)
	assert.Equal(t, gorm.ErrRecordNotFound, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_Update(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	updateData := &models.Subscription{
		Cost:   19.99,
		Status: "Cancelled",
	}

	// Mock the UPDATE query
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "subscriptions" SET`)).
		WithArgs(
			sqlmock.AnyArg(), // cost
			sqlmock.AnyArg(), // status
			sqlmock.AnyArg(), // updated_at
			sqlmock.AnyArg(), // id
		).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	// Mock the SELECT query for GetByID
	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category",
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Netflix", 19.99, "Monthly", "Cancelled", "Entertainment",
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE "subscriptions"."id" = ? ORDER BY "subscriptions"."id" LIMIT ?`)).
		WithArgs(1, 1).
		WillReturnRows(rows)

	result, err := repo.Update(1, updateData)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 19.99, result.Cost)
	assert.Equal(t, "Cancelled", result.Status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_Delete(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`DELETE FROM "subscriptions" WHERE "subscriptions"."id" = ?`)).
		WithArgs(1).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.Delete(1)
	assert.NoError(t, err)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_Count(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{"count"}).AddRow(5)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT count(*) FROM "subscriptions"`)).
		WillReturnRows(rows)

	count := repo.Count()
	assert.Equal(t, int64(5), count)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// Test the repository with a real in-memory database
func TestSubscriptionRepository_Integration(t *testing.T) {
	// Create an in-memory SQLite database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate the schema
	err = db.AutoMigrate(&models.Subscription{})
	require.NoError(t, err)

	repo := NewSubscriptionRepository(db)

	t.Run("Create and Get", func(t *testing.T) {
		subscription := &models.Subscription{
			Name:     "Test Service",
			Cost:     29.99,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Productivity",
		}

		// Create
		created, err := repo.Create(subscription)
		assert.NoError(t, err)
		assert.NotZero(t, created.ID)

		// Get by ID
		retrieved, err := repo.GetByID(created.ID)
		assert.NoError(t, err)
		assert.Equal(t, created.Name, retrieved.Name)
		assert.Equal(t, created.Cost, retrieved.Cost)
	})

	t.Run("GetAll", func(t *testing.T) {
		// Create multiple subscriptions
		subs := []models.Subscription{
			{Name: "Service1", Cost: 10.00, Schedule: "Monthly", Status: "Active", Category: "Other"},
			{Name: "Service2", Cost: 20.00, Schedule: "Annual", Status: "Active", Category: "Other"},
			{Name: "Service3", Cost: 30.00, Schedule: "Monthly", Status: "Cancelled", Category: "Other"},
		}

		for _, sub := range subs {
			_, err := repo.Create(&sub)
			assert.NoError(t, err)
		}

		// Get all
		all, err := repo.GetAll()
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(all), 3)
	})

	t.Run("Update", func(t *testing.T) {
		// Create a subscription
		original := &models.Subscription{
			Name:     "Update Test",
			Cost:     15.00,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Other",
		}
		created, err := repo.Create(original)
		assert.NoError(t, err)

		// Update it
		updateData := &models.Subscription{
			Cost:   25.00,
			Status: "Paused",
		}
		updated, err := repo.Update(created.ID, updateData)
		assert.NoError(t, err)
		assert.Equal(t, 25.00, updated.Cost)
		assert.Equal(t, "Paused", updated.Status)
		assert.Equal(t, "Update Test", updated.Name) // Name should remain unchanged
	})

	t.Run("Delete", func(t *testing.T) {
		// Create a subscription
		sub := &models.Subscription{
			Name:     "Delete Test",
			Cost:     10.00,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Other",
		}
		created, err := repo.Create(sub)
		assert.NoError(t, err)

		// Delete it
		err = repo.Delete(created.ID)
		assert.NoError(t, err)

		// Try to get it - should fail
		_, err = repo.GetByID(created.ID)
		assert.Error(t, err)
		assert.Equal(t, gorm.ErrRecordNotFound, err)
	})
}

func TestSubscriptionRepository_GetActiveSubscriptions(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category",
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Netflix", 15.99, "Monthly", "Active", "Entertainment",
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now()).
		AddRow(2, "Spotify", 9.99, "Monthly", "Active", "Entertainment",
			"", "", nil, nil, nil, "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE status = ?`)).
		WithArgs("Active").
		WillReturnRows(rows)

	subscriptions, err := repo.GetActiveSubscriptions()
	assert.NoError(t, err)
	assert.Len(t, subscriptions, 2)
	assert.Equal(t, "Active", subscriptions[0].Status)
	assert.Equal(t, "Active", subscriptions[1].Status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetCancelledSubscriptions(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category",
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Adobe CC", 54.99, "Monthly", "Cancelled", "Productivity",
			"", "", nil, nil, time.Now(), "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE status = ?`)).
		WithArgs("Cancelled").
		WillReturnRows(rows)

	subscriptions, err := repo.GetCancelledSubscriptions()
	assert.NoError(t, err)
	assert.Len(t, subscriptions, 1)
	assert.Equal(t, "Cancelled", subscriptions[0].Status)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetUpcomingRenewals(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	renewalDate := time.Now().AddDate(0, 0, 3) // 3 days from now
	rows := sqlmock.NewRows([]string{
		"id", "name", "cost", "schedule", "status", "category",
		"payment_method", "account", "start_date", "renewal_date", 
		"cancellation_date", "url", "notes", "usage", "created_at", "updated_at",
	}).
		AddRow(1, "Netflix", 15.99, "Monthly", "Active", "Entertainment",
			"", "", nil, renewalDate, nil, "", "", "", time.Now(), time.Now())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT * FROM "subscriptions" WHERE status = ? AND renewal_date IS NOT NULL AND renewal_date BETWEEN ? AND ?`)).
		WithArgs("Active", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	subscriptions, err := repo.GetUpcomingRenewals(7)
	assert.NoError(t, err)
	assert.Len(t, subscriptions, 1)
	assert.Equal(t, "Netflix", subscriptions[0].Name)

	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSubscriptionRepository_GetCategoryStats(t *testing.T) {
	db, mock := setupTestDB(t)
	repo := NewSubscriptionRepository(db)

	rows := sqlmock.NewRows([]string{"category", "amount", "count"}).
		AddRow("Entertainment", 25.98, 2).
		AddRow("Productivity", 89.99, 3).
		AddRow("Storage", 9.99, 1)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT category, SUM(CASE WHEN schedule = 'Monthly' THEN cost ELSE cost/12 END) as amount, COUNT(*) as count FROM "subscriptions" WHERE status = ? GROUP BY "category"`)).
		WithArgs("Active").
		WillReturnRows(rows)

	stats, err := repo.GetCategoryStats()
	assert.NoError(t, err)
	assert.Len(t, stats, 3)
	assert.Equal(t, "Entertainment", stats[0].Category)
	assert.Equal(t, 25.98, stats[0].Amount)
	assert.Equal(t, 2, stats[0].Count)

	assert.NoError(t, mock.ExpectationsWereMet())
}

// Test the additional methods with integration tests
func TestSubscriptionRepository_AdditionalIntegration(t *testing.T) {
	// Create an in-memory SQLite database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Auto-migrate the schema
	err = db.AutoMigrate(&models.Subscription{})
	require.NoError(t, err)

	repo := NewSubscriptionRepository(db)

	// Create test data
	testDate := time.Now().AddDate(0, 0, 5)
	testSubs := []models.Subscription{
		{Name: "Netflix", Cost: 15.99, Schedule: "Monthly", Status: "Active", Category: "Entertainment", RenewalDate: &testDate},
		{Name: "Spotify", Cost: 9.99, Schedule: "Monthly", Status: "Active", Category: "Entertainment"},
		{Name: "Adobe CC", Cost: 54.99, Schedule: "Monthly", Status: "Cancelled", Category: "Productivity"},
		{Name: "AWS", Cost: 1200.00, Schedule: "Annual", Status: "Active", Category: "Storage"},
	}

	for _, sub := range testSubs {
		_, err := repo.Create(&sub)
		assert.NoError(t, err)
	}

	t.Run("GetActiveSubscriptions", func(t *testing.T) {
		active, err := repo.GetActiveSubscriptions()
		assert.NoError(t, err)
		assert.Len(t, active, 3)
		for _, sub := range active {
			assert.Equal(t, "Active", sub.Status)
		}
	})

	t.Run("GetCancelledSubscriptions", func(t *testing.T) {
		cancelled, err := repo.GetCancelledSubscriptions()
		assert.NoError(t, err)
		assert.Len(t, cancelled, 1)
		assert.Equal(t, "Adobe CC", cancelled[0].Name)
	})

	t.Run("GetUpcomingRenewals", func(t *testing.T) {
		upcoming, err := repo.GetUpcomingRenewals(7)
		assert.NoError(t, err)
		assert.Len(t, upcoming, 1)
		assert.Equal(t, "Netflix", upcoming[0].Name)
	})

	t.Run("GetCategoryStats", func(t *testing.T) {
		stats, err := repo.GetCategoryStats()
		assert.NoError(t, err)
		assert.NotEmpty(t, stats)
		
		// Find Entertainment category
		var entertainmentStat *models.CategoryStat
		for _, stat := range stats {
			if stat.Category == "Entertainment" {
				entertainmentStat = &stat
				break
			}
		}
		
		assert.NotNil(t, entertainmentStat)
		assert.Equal(t, 2, entertainmentStat.Count)
		// Netflix (15.99) + Spotify (9.99) = 25.98
		assert.InDelta(t, 25.98, entertainmentStat.Amount, 0.01)
	})
}
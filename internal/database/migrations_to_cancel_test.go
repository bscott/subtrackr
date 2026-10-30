package database

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

// A database from before #137 gains the fields without rewriting its rows or
// opting anyone in, keeps AUTOINCREMENT, and can run the migrations again.
func TestMigrationsAddToCancelFields(t *testing.T) {
	db, err := Initialize(filepath.Join(t.TempDir(), "upgrade.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(&models.Category{}, &models.Tag{}))
	require.NoError(t, db.Exec(`CREATE TABLE subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		cost REAL NOT NULL,
		schedule TEXT NOT NULL,
		status TEXT NOT NULL,
		category_id INTEGER,
		payment_method TEXT, account TEXT, start_date DATETIME, renewal_date DATETIME, cancellation_date DATETIME,
		url TEXT, notes TEXT, usage TEXT, created_at DATETIME, updated_at DATETIME
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscriptions (id, name, cost, schedule, status, notes, renewal_date)
		VALUES (5, 'Existing', 1, 'Monthly', 'Paused', 'keep me', '2030-01-02 00:00:00+00:00')`).Error)

	require.NoError(t, RunMigrations(db))
	require.NoError(t, RunMigrations(db), "migrations can run again")

	for _, column := range []string{"to_cancel", "to_cancel_reminder_days", "last_to_cancel_reminder_sent", "last_to_cancel_reminder_anchor"} {
		var count int64
		require.NoError(t, db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = ?", column).Scan(&count).Error)
		assert.EqualValues(t, 1, count, column)
	}

	var ddl string
	require.NoError(t, db.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'subscriptions'").Scan(&ddl).Error)
	assert.Contains(t, strings.ToUpper(ddl), "AUTOINCREMENT")

	repo := repository.NewSubscriptionRepository(db)
	existing, err := repo.GetByID(5)
	require.NoError(t, err)
	assert.Equal(t, "keep me", existing.Notes)
	assert.False(t, existing.ToCancel, "nobody is opted in by the upgrade")
	assert.Equal(t, models.DefaultToCancelReminderDays, existing.ToCancelReminderDays)
	assert.Nil(t, existing.LastToCancelReminderSent)
	assert.Nil(t, existing.LastToCancelReminderAnchor)

	// The columns work on the upgraded table, and a deleted row's ID is not reused.
	renewal := time.Now().AddDate(0, 0, 2)
	created, err := repo.Create(&models.Subscription{Name: "New", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal, ToCancel: true, ToCancelReminderDays: 14})
	require.NoError(t, err)
	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	assert.True(t, stored.ToCancel)
	assert.Equal(t, 14, stored.ToCancelReminderDays)
	marked, err := repo.MarkToCancelReminderSent(stored, *stored.ToCancelDeadline(), time.Now())
	require.NoError(t, err)
	assert.True(t, marked)
	require.NoError(t, repo.Delete(created.ID))
	again, err := repo.Create(&models.Subscription{Name: "Next", Cost: 1, Schedule: "Monthly", Status: "Paused"})
	require.NoError(t, err)
	assert.Greater(t, again.ID, created.ID)
}

// The old category text column sends writes through hand-built SQL, which must
// carry these fields too.
func TestToCancelFieldsOnLegacyCategoryColumnSchema(t *testing.T) {
	db, err := Initialize(filepath.Join(t.TempDir(), "legacy.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	require.NoError(t, RunMigrations(db))
	require.NoError(t, db.Exec("ALTER TABLE subscriptions ADD COLUMN category TEXT").Error)

	repo := repository.NewSubscriptionRepository(db)
	renewal := time.Now().AddDate(0, 0, 2)
	stamp := time.Now()
	created, err := repo.Create(&models.Subscription{Name: "Legacy", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal,
		ToCancel: true, ToCancelReminderDays: 14, LastToCancelReminderAnchor: &stamp, LastToCancelReminderSent: &stamp})
	require.NoError(t, err)
	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	assert.True(t, stored.ToCancel)
	assert.Equal(t, 14, stored.ToCancelReminderDays)
	assert.NotNil(t, stored.LastToCancelReminderAnchor)

	stored.Notes = "unrelated"
	updated, err := repo.Update(stored.ID, stored)
	require.NoError(t, err)
	assert.True(t, updated.ToCancel)
	assert.NotNil(t, updated.LastToCancelReminderAnchor, "unrelated edits keep the delivered state")

	updated.ToCancelReminderDays = 5
	updated, err = repo.Update(updated.ID, updated)
	require.NoError(t, err)
	assert.Equal(t, 5, updated.ToCancelReminderDays)
	assert.Nil(t, updated.LastToCancelReminderAnchor, "relevant edits clear it")

	updated.Status = "Cancelled"
	updated, err = repo.Update(updated.ID, updated)
	require.NoError(t, err)
	assert.False(t, updated.ToCancel)
}

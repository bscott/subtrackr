package database

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

// Older releases declared category_id NOT NULL and could store 0 or the ID of a
// deleted category while foreign keys were not enforced.
func TestMigrationsMarkInvalidCategoriesUncategorized(t *testing.T) {
	for name, table := range map[string]string{
		"unquoted columns": `CREATE TABLE subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			cost REAL NOT NULL,
			schedule TEXT NOT NULL,
			status TEXT NOT NULL,
			category_id INTEGER NOT NULL,
			payment_method TEXT, account TEXT, start_date DATETIME, renewal_date DATETIME, cancellation_date DATETIME,
			url TEXT, notes TEXT, usage TEXT, created_at DATETIME, updated_at DATETIME
		)`,
		"GORM quoted columns with foreign key": "CREATE TABLE `subscriptions` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`cost` real NOT NULL," +
			"`schedule` text NOT NULL,`status` text NOT NULL,`category_id` integer NOT NULL," +
			"`payment_method` text,`account` text,`start_date` datetime,`renewal_date` datetime,`cancellation_date` datetime," +
			"`url` text,`notes` text,`usage` text,`created_at` datetime,`updated_at` datetime," +
			"CONSTRAINT `fk_subscriptions_category` FOREIGN KEY (`category_id`) REFERENCES `categories`(`id`) ON DELETE SET NULL ON UPDATE CASCADE)",
	} {
		t.Run(name, func(t *testing.T) { testUpgradeFromNotNullCategory(t, table) })
	}
}

func testUpgradeFromNotNullCategory(t *testing.T, subscriptionsTable string) {
	db, err := Initialize(filepath.Join(t.TempDir(), "upgrade.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(&models.Category{}, &models.Tag{}))
	require.NoError(t, db.Exec(`PRAGMA foreign_keys = OFF`).Error)
	require.NoError(t, db.Exec(subscriptionsTable).Error)
	require.NoError(t, db.Exec(`INSERT INTO categories (id, name) VALUES (7, 'Video')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscriptions (id, name, cost, schedule, status, category_id) VALUES
		(1, 'Valid', 1, 'Monthly', 'Paused', 7),
		(2, 'Zero', 1, 'Monthly', 'Paused', 0),
		(3, 'Dangling', 1, 'Monthly', 'Paused', 99)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscriptions (id, name, cost, schedule, status, category_id) VALUES (100, 'Deleted', 1, 'Monthly', 'Paused', 7)`).Error)
	require.NoError(t, db.Exec(`DELETE FROM subscriptions WHERE id = 100`).Error)
	require.NoError(t, db.Exec(`PRAGMA foreign_keys = ON`).Error)
	require.NoError(t, db.Exec(`INSERT INTO tags (id, name) VALUES (5, 'kept')`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE subscription_tags (
		subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (subscription_id, tag_id)
	)`).Error)
	require.NoError(t, db.Exec(`INSERT INTO subscription_tags (subscription_id, tag_id) VALUES (2, 5)`).Error)

	require.NoError(t, RunMigrations(db))
	require.NoError(t, RunMigrations(db), "migrations can run again")

	var foreignKeys int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error)
	assert.Equal(t, 1, foreignKeys, "enforcement is back on after the table rebuild")

	var notNull int64
	require.NoError(t, db.Raw(`SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'category_id' AND "notnull" = 1`).Scan(&notNull).Error)
	assert.Zero(t, notNull)

	var nulls []string
	require.NoError(t, db.Raw("SELECT name FROM subscriptions WHERE category_id IS NULL ORDER BY id").Scan(&nulls).Error)
	assert.Equal(t, []string{"Zero", "Dangling"}, nulls)

	repo := repository.NewSubscriptionRepository(db)
	retagged, err := repo.GetByID(2)
	require.NoError(t, err)
	require.Len(t, retagged.Tags, 1, "tag links survive the table rebuild")

	valid, err := repo.GetByID(1)
	require.NoError(t, err)
	assert.EqualValues(t, 7, valid.CategoryID)

	// The repaired rows can be written again.
	for _, id := range []uint{2, 3} {
		sub, err := repo.GetByID(id)
		require.NoError(t, err)
		assert.Zero(t, sub.CategoryID)
		sub.Notes = "edited"
		_, err = repo.Update(id, sub)
		require.NoError(t, err)
	}

	// A new uncategorized row works without reusing previously issued IDs.
	created, err := repo.Create(&models.Subscription{Name: "New", Cost: 1, Schedule: "Monthly", Status: "Paused"})
	require.NoError(t, err)
	assert.Greater(t, created.ID, uint(100))

	// Invalid references are rejected even when legacy DDL has no FK.
	_, err = repo.Create(&models.Subscription{Name: "Bad", Cost: 1, Schedule: "Monthly", Status: "Paused", CategoryID: 99})
	require.ErrorContains(t, err, "category 99 not found")
	created.CategoryID = 99
	_, err = repo.Update(created.ID, created)
	require.ErrorContains(t, err, "category 99 not found")
}

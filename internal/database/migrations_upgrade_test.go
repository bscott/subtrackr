package database

import (
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

// These schemas were captured by running the release code, not written by hand.
// The v0.6.10 model declared reminder_enabled DEFAULT true. v0.7.0 removed that,
// so GORM rebuilds the table on upgrade from a v0.6.10 database.
var (
	//go:embed testdata/v0.6.10_fresh_install.sql
	v0610FreshInstallSchema string
	//go:embed testdata/v0.3.0_upgraded_to_v0.6.10.sql
	v030UpgradedSchema string
)

const (
	legacyUnquotedSubscriptions = `CREATE TABLE subscriptions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		cost REAL NOT NULL,
		schedule TEXT NOT NULL,
		status TEXT NOT NULL,
		category_id INTEGER NOT NULL,
		payment_method TEXT, account TEXT, start_date DATETIME, renewal_date DATETIME, cancellation_date DATETIME,
		url TEXT, notes TEXT, usage TEXT, created_at DATETIME, updated_at DATETIME
	);`
	legacyQuotedSubscriptions = "CREATE TABLE `subscriptions` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,`cost` real NOT NULL," +
		"`schedule` text NOT NULL,`status` text NOT NULL,`category_id` integer NOT NULL," +
		"`payment_method` text,`account` text,`start_date` datetime,`renewal_date` datetime,`cancellation_date` datetime," +
		"`url` text,`notes` text,`usage` text,`created_at` datetime,`updated_at` datetime," +
		"CONSTRAINT `fk_subscriptions_category` FOREIGN KEY (`category_id`) REFERENCES `categories`(`id`) ON DELETE SET NULL ON UPDATE CASCADE);"
	legacySubscriptionTags = `CREATE TABLE subscription_tags (
		subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (subscription_id, tag_id)
	);`

	deletedSubscriptionID = 40
	deletedTagID          = 9
	deletedCategoryID     = 12
)

// A user's own objects, including every shape that makes a rebuild harder: a
// UNIQUE index GORM reads as a column change, a view and a trigger on another
// table that name the dropped table, and text that looks like a bind parameter.
const customObjects = `
	CREATE TABLE audit_log (id INTEGER PRIMARY KEY AUTOINCREMENT, note TEXT);
	CREATE UNIQUE INDEX idx_user_name_cost ON subscriptions (name, cost);
	CREATE UNIQUE INDEX idx_user_name ON subscriptions (name);
	CREATE INDEX idx_user_active ON subscriptions (renewal_date) WHERE status = 'Active';
	CREATE INDEX idx_user_lower_name ON subscriptions (lower(name));
	CREATE INDEX idx_user_tag ON subscription_tags (tag_id);
	CREATE VIEW v_user_active AS SELECT id, name FROM subscriptions WHERE status = 'Active';
	CREATE TRIGGER trg_user_note AFTER UPDATE OF notes ON subscriptions BEGIN
		INSERT INTO audit_log (note) VALUES ('who? @admin ' || NEW.id);
	END;
	CREATE TRIGGER trg_user_case AFTER DELETE ON SUBSCRIPTIONS BEGIN
		INSERT INTO audit_log (note) VALUES ('deleted ' || OLD.id);
	END;
	CREATE TRIGGER trg_user_tag AFTER INSERT ON subscription_tags BEGIN
		INSERT INTO audit_log (note) VALUES ('tagged');
	END;
	CREATE TRIGGER trg_user_category AFTER UPDATE ON categories BEGIN
		UPDATE subscriptions SET notes = notes WHERE category_id = NEW.id;
	END;
`

type upgradeScenario struct {
	name   string
	schema string
	// gormRebuilds says whether GORM can rebuild this schema. It cannot parse the
	// unquoted columns the older releases added, so those tables stay as they are.
	gormRebuilds bool
}

func upgradeScenarios() []upgradeScenario {
	return []upgradeScenario{
		{name: "v0.6.10 fresh install", schema: v0610FreshInstallSchema, gormRebuilds: true},
		{name: "v0.3.0 upgraded to v0.6.10", schema: v030UpgradedSchema},
		{name: "legacy unquoted NOT NULL category", schema: legacyUnquotedSubscriptions + legacySubscriptionTags},
		{name: "legacy quoted NOT NULL category", schema: legacyQuotedSubscriptions + legacySubscriptionTags},
	}
}

func openUpgradeDB(t *testing.T, path string) (*gorm.DB, *sql.DB) {
	t.Helper()
	db, err := Initialize(path)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	// One connection: a pragma left behind is visible, and any query that escapes
	// the migration's pinned connection deadlocks instead of passing unnoticed.
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	return db, sqlDB
}

// seedInstalled builds an installed database: the schema, real data, IDs deleted
// above the current maximum, and the user's own indexes, triggers and view.
func seedInstalled(t *testing.T, db *gorm.DB, sqlDB *sql.DB, schema string) {
	t.Helper()
	if !strings.Contains(schema, "CREATE TABLE `categories`") {
		require.NoError(t, db.AutoMigrate(&models.Category{}, &models.Settings{}, &models.APIKey{}, &models.ExchangeRate{}, &models.Tag{}))
	}
	_, err := sqlDB.Exec(schema)
	require.NoError(t, err)
	seedData(t, sqlDB)
	_, err = sqlDB.Exec(customObjects)
	require.NoError(t, err)
}

func seedData(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	_, err := sqlDB.Exec(fmt.Sprintf(`
		INSERT INTO categories (id, name) VALUES (7, 'Video'), (%[1]d, 'Removed');
		DELETE FROM categories WHERE id = %[1]d;
		INSERT INTO settings (id, key, value) VALUES (3, 'currency', 'EUR'), (4, 'smtp_host', 'mail.example.test');
		INSERT INTO api_keys (id, name, key, usage_count) VALUES (2, 'cli', 'sk-test', 11);
		INSERT INTO exchange_rates (id, base_currency, currency, rate, date) VALUES (6, 'USD', 'EUR', 0.9, '2026-01-02 00:00:00+00:00');
		INSERT INTO tags (id, name) VALUES (5, 'kept'), (%[2]d, 'removed');
		DELETE FROM tags WHERE id = %[2]d;
	`, deletedCategoryID, deletedTagID))
	require.NoError(t, err)

	columns := map[string]bool{}
	for _, row := range tableRows(t, sqlDB, "pragma_table_info('subscriptions')") {
		columns[row["name"].(string)] = true
	}
	for _, row := range []map[string]any{
		{"id": 1, "name": "ReminderOff", "reminder_enabled": 0, "autopay": 0, "label": "family", "original_currency": "EUR", "share_count": 3,
			"cancellation_notice_days": 14, "schedule_interval": 2, "last_reminder_window": 5, "icon_url": "https://example.test/i.png",
			"url": "https://example.test", "payment_method": "card", "account": "me", "usage": "High", "notes": "keep me",
			"start_date": "2024-02-03 00:00:00+00:00", "renewal_date": "2030-03-04 00:00:00+00:00", "last_reminder_sent": "2029-01-01 00:00:00+00:00"},
		{"id": 2, "name": "ReminderOn", "reminder_enabled": 1, "autopay": 1, "notes": "also keep"},
		{"id": 3, "name": "ReminderDefaulted"},
		{"id": deletedSubscriptionID, "name": "Removed"},
	} {
		row["cost"], row["schedule"], row["status"], row["category_id"] = 9.5, "Monthly", "Paused", 7
		var names, marks []string
		var values []any
		for name, value := range row {
			if columns[name] {
				names, marks, values = append(names, "`"+name+"`"), append(marks, "?"), append(values, value)
			}
		}
		_, err := sqlDB.Exec("INSERT INTO subscriptions ("+strings.Join(names, ",")+") VALUES ("+strings.Join(marks, ",")+")", values...)
		require.NoError(t, err)
	}
	_, err = sqlDB.Exec(fmt.Sprintf(`
		DELETE FROM subscriptions WHERE id = %d;
		INSERT INTO subscription_tags (subscription_id, tag_id) VALUES (1, 5), (3, 5);
	`, deletedSubscriptionID))
	require.NoError(t, err)
}

func tableRows(t *testing.T, sqlDB *sql.DB, source string) []map[string]any {
	t.Helper()
	rows, err := sqlDB.Query("SELECT * FROM " + source)
	require.NoError(t, err)
	defer rows.Close()
	columns, err := rows.Columns()
	require.NoError(t, err)
	var result []map[string]any
	for rows.Next() {
		values, targets := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			targets[i] = &values[i]
		}
		require.NoError(t, rows.Scan(targets...))
		row := map[string]any{}
		for i, column := range columns {
			row[column] = values[i]
		}
		result = append(result, row)
	}
	require.NoError(t, rows.Err())
	return result
}

// installedState is everything an upgrade must carry forward.
type installedState struct {
	rows      map[string][]map[string]any
	objects   map[string]string
	sequences map[string]int64
}

func captureInstalled(t *testing.T, sqlDB *sql.DB) installedState {
	t.Helper()
	state := installedState{rows: map[string][]map[string]any{}, objects: map[string]string{}, sequences: map[string]int64{}}
	for _, table := range []string{"categories", "settings", "api_keys", "exchange_rates", "tags", "subscriptions", "subscription_tags", "audit_log"} {
		state.rows[table] = tableRows(t, sqlDB, table)
	}
	for _, row := range tableRows(t, sqlDB, "sqlite_master WHERE type IN ('index', 'trigger', 'view') AND sql IS NOT NULL") {
		state.objects[row["name"].(string)] = row["sql"].(string)
	}
	for _, row := range tableRows(t, sqlDB, "sqlite_sequence") {
		state.sequences[row["name"].(string)] = row["seq"].(int64)
	}
	return state
}

// assertCarriedForward allows new columns but no change to anything that existed.
func assertCarriedForward(t *testing.T, sqlDB *sql.DB, before installedState) {
	t.Helper()
	after := captureInstalled(t, sqlDB)
	for table, rows := range before.rows {
		require.Len(t, after.rows[table], len(rows), table)
		for i, row := range rows {
			for column, value := range row {
				assert.Equal(t, value, after.rows[table][i][column], "%s row %d column %s", table, i, column)
			}
		}
	}
	assert.Equal(t, before.objects, after.objects, "indexes, triggers and views")
	assert.Equal(t, before.sequences, after.sequences, "AUTOINCREMENT high-water marks")
}

func reminderDefault(t *testing.T, sqlDB *sql.DB) sql.NullString {
	t.Helper()
	var dflt sql.NullString
	require.NoError(t, sqlDB.QueryRow("SELECT dflt_value FROM pragma_table_info('subscriptions') WHERE name = 'reminder_enabled'").Scan(&dflt))
	return dflt
}

// assertConnectionHealthy needs the single-connection pool: it reads the same
// connection the migration ran on.
func assertConnectionHealthy(t *testing.T, sqlDB *sql.DB) {
	t.Helper()
	var foreignKeys, legacyAlter int
	require.NoError(t, sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys))
	require.NoError(t, sqlDB.QueryRow("PRAGMA legacy_alter_table").Scan(&legacyAlter))
	assert.Equal(t, 1, foreignKeys, "foreign keys enforced again")
	assert.Equal(t, 0, legacyAlter, "legacy_alter_table restored")
	_, err := sqlDB.Exec("INSERT INTO subscription_tags (subscription_id, tag_id) VALUES (999, 999)")
	assert.ErrorContains(t, err, "FOREIGN KEY constraint failed")

	assert.Empty(t, tableRows(t, sqlDB, "pragma_foreign_key_check"), "no orphaned rows")
	assert.Empty(t, tableRows(t, sqlDB, "sqlite_master WHERE name LIKE '%__temp' OR name LIKE '%__rebuild'"), "no leftover rebuild tables")
	var integrity string
	require.NoError(t, sqlDB.QueryRow("PRAGMA integrity_check").Scan(&integrity))
	assert.Equal(t, "ok", integrity)

	references := map[string]bool{}
	for _, row := range tableRows(t, sqlDB, "pragma_foreign_key_list('subscription_tags')") {
		references[row["table"].(string)] = true
	}
	assert.Equal(t, map[string]bool{"subscriptions": true, "tags": true}, references)
}

// assertApplicationWorks uses the database the way the running app does.
func assertApplicationWorks(t *testing.T, db *gorm.DB, sqlDB *sql.DB) {
	t.Helper()
	repo := repository.NewSubscriptionRepository(db)

	off, err := repo.GetByID(1)
	require.NoError(t, err)
	var hasReminderColumnBefore bool
	if off.Label == "family" { // Only schemas that had the column could seed these values.
		hasReminderColumnBefore = true
		assert.False(t, off.ReminderEnabled, "an explicit false survives the upgrade")
		assert.Equal(t, "EUR", off.OriginalCurrency)
		assert.Equal(t, 3, off.ShareCount)
		assert.Equal(t, 14, off.CancellationNoticeDays)
		assert.Equal(t, 2, off.ScheduleInterval)
		assert.Equal(t, 5, off.LastReminderWindow)
		require.NotNil(t, off.Autopay)
		assert.False(t, *off.Autopay)
	}
	assert.Equal(t, "keep me", off.Notes)
	assert.EqualValues(t, 7, off.CategoryID)
	assert.Equal(t, "Video", off.Category.Name)
	require.Len(t, off.Tags, 1, "tag links survive")
	assert.Equal(t, "kept", off.Tags[0].Name)
	assert.False(t, off.ToCancel, "nobody is opted in by the upgrade")

	defaulted, err := repo.GetByID(3)
	require.NoError(t, err)
	assert.True(t, defaulted.ReminderEnabled, "a row that took the old default stays on")
	require.Len(t, defaulted.Tags, 1)

	if hasReminderColumnBefore {
		off.Notes = "edited"
		updated, err := repo.Update(1, off)
		require.NoError(t, err)
		assert.False(t, updated.ReminderEnabled, "an edit keeps false")
	}

	// The original bug must stay fixed whichever way the column default went.
	created, err := repo.Create(&models.Subscription{Name: "NewOff", Cost: 1, Schedule: "Monthly", Status: "Paused", ReminderEnabled: false})
	require.NoError(t, err)
	assert.Greater(t, created.ID, uint(deletedSubscriptionID), "a deleted ID is not reused")
	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	assert.False(t, stored.ReminderEnabled, "explicit false is stored as false")

	require.NoError(t, repo.Delete(created.ID))
	again, err := repo.Create(&models.Subscription{Name: "NewOn", Cost: 1, Schedule: "Monthly", Status: "Active", ReminderEnabled: true})
	require.NoError(t, err)
	assert.Greater(t, again.ID, created.ID)
	require.NoError(t, repo.Delete(again.ID))

	tag := models.Tag{Name: "after-upgrade"}
	require.NoError(t, db.Create(&tag).Error)
	assert.Greater(t, tag.ID, uint(deletedTagID))
	category := models.Category{Name: "after-upgrade"}
	require.NoError(t, db.Create(&category).Error)
	assert.Greater(t, category.ID, uint(deletedCategoryID))

	// The restored objects work rather than merely exist.
	var audited int
	_, err = sqlDB.Exec("UPDATE subscriptions SET notes = 'fires trigger' WHERE id = 2")
	require.NoError(t, err)
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM audit_log WHERE note = 'who? @admin 2'").Scan(&audited))
	assert.Equal(t, 1, audited, "UPDATE trigger fires")
	_, err = sqlDB.Exec("INSERT INTO subscriptions (name, cost, schedule, status) SELECT name, cost, schedule, status FROM subscriptions WHERE id = 2")
	assert.ErrorContains(t, err, "UNIQUE constraint failed", "unique index enforced")
	var viewRows int
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM v_user_active").Scan(&viewRows))
	assert.Zero(t, viewRows)
}

func TestUpgradeCarriesEverythingForward(t *testing.T) {
	for _, scenario := range upgradeScenarios() {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "installed.db")
			db, sqlDB := openUpgradeDB(t, path)
			seedInstalled(t, db, sqlDB, scenario.schema)
			before := captureInstalled(t, sqlDB)
			if scenario.gormRebuilds {
				require.Equal(t, "true", reminderDefault(t, sqlDB).String, "the fixture must carry the default whose removal causes the rebuild")
			}

			require.NoError(t, RunMigrations(db))
			assertCarriedForward(t, sqlDB, before)
			assertConnectionHealthy(t, sqlDB)
			if scenario.gormRebuilds {
				assert.False(t, reminderDefault(t, sqlDB).Valid, "the table really was rebuilt")
			}

			require.NoError(t, RunMigrations(db), "the upgrade can run twice")
			assertCarriedForward(t, sqlDB, before)
			assertConnectionHealthy(t, sqlDB)

			// A restart reads the result back through a new connection.
			require.NoError(t, sqlDB.Close())
			db, sqlDB = openUpgradeDB(t, path)
			require.NoError(t, RunMigrations(db), "the upgrade survives a restart")
			assertCarriedForward(t, sqlDB, before)
			assertApplicationWorks(t, db, sqlDB)
			assertConnectionHealthy(t, sqlDB)
		})
	}
}

// Without any custom objects the v0.6.10 schema rebuilds once and then settles.
func TestUpgradeRebuildsOnlyOnce(t *testing.T) {
	db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
	_, err := sqlDB.Exec(v0610FreshInstallSchema)
	require.NoError(t, err)
	seedData(t, sqlDB)

	require.NoError(t, RunMigrations(db))
	var settled, again int
	require.NoError(t, sqlDB.QueryRow("PRAGMA schema_version").Scan(&settled))
	require.NoError(t, RunMigrations(db))
	require.NoError(t, sqlDB.QueryRow("PRAGMA schema_version").Scan(&again))
	assert.Equal(t, settled, again, "a stable schema is not rewritten at every start")
}

func TestFreshInstall(t *testing.T) {
	db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "new.db"))
	require.NoError(t, RunMigrations(db))

	var version int
	require.NoError(t, sqlDB.QueryRow("PRAGMA schema_version").Scan(&version))
	require.NoError(t, RunMigrations(db), "migrations can run again")
	var again int
	require.NoError(t, sqlDB.QueryRow("PRAGMA schema_version").Scan(&again))
	assert.Equal(t, version, again, "nothing to change the second time")

	assert.False(t, reminderDefault(t, sqlDB).Valid)
	var foreignKeys, legacyAlter int
	require.NoError(t, sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys))
	require.NoError(t, sqlDB.QueryRow("PRAGMA legacy_alter_table").Scan(&legacyAlter))
	assert.Equal(t, 1, foreignKeys)
	assert.Zero(t, legacyAlter)

	repo := repository.NewSubscriptionRepository(db)
	off, err := repo.Create(&models.Subscription{Name: "Off", Cost: 1, Schedule: "Monthly", Status: "Paused", ReminderEnabled: false})
	require.NoError(t, err)
	stored, err := repo.GetByID(off.ID)
	require.NoError(t, err)
	assert.False(t, stored.ReminderEnabled)
	require.NoError(t, repo.Delete(off.ID))
	next, err := repo.Create(&models.Subscription{Name: "Next", Cost: 1, Schedule: "Monthly", Status: "Paused"})
	require.NoError(t, err)
	assert.Greater(t, next.ID, off.ID)
}

// A new database has nothing to fall back on, so it must not start half-built.
func TestFreshInstallFailureStopsStartup(t *testing.T) {
	db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "new.db"))
	fired, remove := failStatement(t, db, "CREATE TABLE `subscriptions`", injected)
	require.ErrorContains(t, RunMigrations(db), "create subscriptions table")
	require.Positive(t, fired.Load())
	remove()

	var foreignKeys int
	require.NoError(t, sqlDB.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys))
	assert.Equal(t, 1, foreignKeys)

	require.NoError(t, RunMigrations(db), "the next start retries")
	assert.False(t, reminderDefault(t, sqlDB).Valid)
}

// failStatement makes every statement containing text fail until the returned
// function removes the failure.
func failStatement(t *testing.T, db *gorm.DB, text string, fail func(*gorm.DB)) (fired *atomic.Int64, remove func()) {
	t.Helper()
	fired = &atomic.Int64{}
	const name = "test:fail_statement"
	require.NoError(t, db.Callback().Raw().Before("gorm:raw").Register(name, func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), text) {
			fired.Add(1)
			fail(tx)
		}
	}))
	return fired, func() { require.NoError(t, db.Callback().Raw().Remove(name)) }
}

func injected(tx *gorm.DB) { tx.AddError(errors.New("injected failure")) }

// A failure anywhere in the rebuild or the restore leaves the previous schema
// and data in place, and the app starts on it as it did before this release.
func TestUpgradeFailureRollsBackAndCanBeRetried(t *testing.T) {
	for _, step := range []struct{ name, statement string }{
		{"GORM copy", "INSERT INTO `subscriptions__temp`"},
		{"GORM drop", "DROP TABLE `subscriptions`"},
		{"GORM rename", "ALTER TABLE `subscriptions__temp`"},
		{"restoring an index", "CREATE UNIQUE INDEX idx_user_name_cost"},
		{"restoring a trigger", "CREATE TRIGGER trg_user_note"},
		{"restoring a sequence", "INSERT INTO sqlite_sequence"},
	} {
		t.Run(step.name, func(t *testing.T) {
			db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
			seedInstalled(t, db, sqlDB, v0610FreshInstallSchema)
			before := captureInstalled(t, sqlDB)

			fired, remove := failStatement(t, db, step.statement, injected)
			require.NoError(t, RunMigrations(db), "an existing database keeps starting")
			require.Positive(t, fired.Load(), "the failure must really have been injected")
			remove()

			assert.Equal(t, "true", reminderDefault(t, sqlDB).String, "the rebuild was rolled back")
			assertCarriedForward(t, sqlDB, before)
			assertConnectionHealthy(t, sqlDB)

			require.NoError(t, RunMigrations(db), "the next start retries")
			assert.False(t, reminderDefault(t, sqlDB).Valid, "the retry completes the rebuild")
			assertCarriedForward(t, sqlDB, before)
			assertApplicationWorks(t, db, sqlDB)
			assertConnectionHealthy(t, sqlDB)
		})
	}
}

// Allowing uncategorized subscriptions is required, so its failure stops startup.
func TestRequiredRebuildFailureStopsStartupWithDataIntact(t *testing.T) {
	for _, step := range []struct{ name, statement string }{
		{"rebuild", "DROP TABLE subscriptions"},
		{"restoring a trigger", "CREATE TRIGGER trg_user_note"},
		{"restoring a sequence", "INSERT INTO sqlite_sequence"},
	} {
		t.Run(step.name, func(t *testing.T) {
			db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
			seedInstalled(t, db, sqlDB, legacyUnquotedSubscriptions+legacySubscriptionTags)
			before := captureInstalled(t, sqlDB)

			fired, remove := failStatement(t, db, step.statement, injected)
			err := RunMigrations(db)
			require.ErrorContains(t, err, "make subscriptions.category_id nullable")
			require.Positive(t, fired.Load())
			remove()

			var notNull int
			require.NoError(t, sqlDB.QueryRow(`SELECT "notnull" FROM pragma_table_info('subscriptions') WHERE name = 'category_id'`).Scan(&notNull))
			assert.Equal(t, 1, notNull, "the rebuild was rolled back")
			assertCarriedForward(t, sqlDB, before)
			assertConnectionHealthy(t, sqlDB)

			require.NoError(t, RunMigrations(db), "the next start retries")
			require.NoError(t, sqlDB.QueryRow(`SELECT "notnull" FROM pragma_table_info('subscriptions') WHERE name = 'category_id'`).Scan(&notNull))
			assert.Zero(t, notNull)
			assertCarriedForward(t, sqlDB, before)
			assertApplicationWorks(t, db, sqlDB)
		})
	}
}

// The first AutoMigrate gets the same protection, and its failures were always
// fatal. A column default that drifts from the model rebuilds categories.
func TestBaseTableRebuildPreservesMetadata(t *testing.T) {
	drifted := strings.Replace(v0610FreshInstallSchema,
		"CREATE TABLE `categories` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL,",
		"CREATE TABLE `categories` (`id` integer PRIMARY KEY AUTOINCREMENT,`name` text NOT NULL DEFAULT 'x',", 1)
	require.NotEqual(t, v0610FreshInstallSchema, drifted)
	categoryDefault := func(sqlDB *sql.DB) sql.NullString {
		var dflt sql.NullString
		require.NoError(t, sqlDB.QueryRow("SELECT dflt_value FROM pragma_table_info('categories') WHERE name = 'name'").Scan(&dflt))
		return dflt
	}

	t.Run("success", func(t *testing.T) {
		db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
		seedInstalled(t, db, sqlDB, drifted)
		before := captureInstalled(t, sqlDB)
		require.NoError(t, RunMigrations(db))
		assert.False(t, categoryDefault(sqlDB).Valid, "categories really was rebuilt")
		assertCarriedForward(t, sqlDB, before)
		assertApplicationWorks(t, db, sqlDB)
		assertConnectionHealthy(t, sqlDB)
	})

	t.Run("failure", func(t *testing.T) {
		db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
		seedInstalled(t, db, sqlDB, drifted)
		before := captureInstalled(t, sqlDB)
		fired, remove := failStatement(t, db, "CREATE TRIGGER trg_user_category", injected)
		require.ErrorContains(t, RunMigrations(db), "restore schema metadata")
		require.Positive(t, fired.Load())
		remove()
		assert.True(t, categoryDefault(sqlDB).Valid, "the rebuild was rolled back")
		assertCarriedForward(t, sqlDB, before)
		assertConnectionHealthy(t, sqlDB)
	})
}

// GORM's copy statement leaves out a column whose name its regular expression
// does not match. The rebuild reports success, so only the count check sees it.
func TestRebuildThatWouldLoseAColumnIsRolledBack(t *testing.T) {
	db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "installed.db"))
	seedInstalled(t, db, sqlDB, v0610FreshInstallSchema)
	_, err := sqlDB.Exec("ALTER TABLE subscriptions ADD COLUMN [my note] TEXT; UPDATE subscriptions SET [my note] = 'precious' WHERE id = 1;")
	require.NoError(t, err)
	before := captureInstalled(t, sqlDB)

	require.NoError(t, RunMigrations(db))
	assert.Equal(t, "true", reminderDefault(t, sqlDB).String, "the rebuild was rolled back")
	assertCarriedForward(t, sqlDB, before)
	assertApplicationWorks(t, db, sqlDB)
	assertConnectionHealthy(t, sqlDB)
}

// The process dying inside the transaction leaves the previous database, which
// SQLite recovers from the write-ahead log on the next open.
func TestUpgradeSurvivesProcessDeath(t *testing.T) {
	const crashPathEnv = "SUBTRACKR_TEST_CRASH_DB"
	if path := os.Getenv(crashPathEnv); path != "" {
		db, err := Initialize(path)
		require.NoError(t, err)
		failStatement(t, db, "CREATE TRIGGER trg_user_note", func(*gorm.DB) { os.Exit(42) })
		_ = RunMigrations(db)
		os.Exit(0) // Reaching this means the crash point was never hit.
	}

	path := filepath.Join(t.TempDir(), "installed.db")
	db, sqlDB := openUpgradeDB(t, path)
	seedInstalled(t, db, sqlDB, v0610FreshInstallSchema)
	before := captureInstalled(t, sqlDB)
	require.NoError(t, sqlDB.Close())

	cmd := exec.Command(os.Args[0], "-test.run=^TestUpgradeSurvivesProcessDeath$")
	cmd.Env = append(os.Environ(), crashPathEnv+"="+path)
	err := cmd.Run()
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 42, exit.ExitCode(), "the process died at the crash point")

	db, sqlDB = openUpgradeDB(t, path)
	assert.Equal(t, "true", reminderDefault(t, sqlDB).String, "the interrupted rebuild left no trace")
	assertCarriedForward(t, sqlDB, before)

	require.NoError(t, RunMigrations(db))
	assert.False(t, reminderDefault(t, sqlDB).Valid)
	assertCarriedForward(t, sqlDB, before)
	assertApplicationWorks(t, db, sqlDB)
	assertConnectionHealthy(t, sqlDB)
}

func TestUpgradeIgnoresStaleSequenceForNonAutoincrementTable(t *testing.T) {
	db, sqlDB := openUpgradeDB(t, filepath.Join(t.TempDir(), "stale.db"))
	seedInstalled(t, db, sqlDB, v0610FreshInstallSchema)
	_, err := sqlDB.Exec("CREATE TABLE user_plain (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO user_plain VALUES (1, 'kept'); INSERT INTO sqlite_sequence(name, seq) VALUES ('user_plain', 900)")
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))
	require.NoError(t, RunMigrations(db))
	var value string
	require.NoError(t, sqlDB.QueryRow("SELECT value FROM user_plain WHERE id=1").Scan(&value))
	assert.Equal(t, "kept", value)
	assertConnectionHealthy(t, sqlDB)
}

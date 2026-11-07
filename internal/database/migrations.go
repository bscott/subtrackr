package database

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"subtrackr/internal/models"

	"gorm.io/gorm"
)

// RunMigrations executes all database migrations.
//
// Schema changes can rebuild a table (create a copy, drop the original,
// rename). They run on one pinned connection with foreign keys off, as SQLite
// requires, so dropping subscriptions neither fails on nor cascade-deletes
// rows in subscription_tags. legacy_alter_table keeps the rename from rejecting
// a user's view or trigger that names the dropped table. The connection gets its
// previous settings back before it returns to the pool.
func RunMigrations(db *gorm.DB) error {
	return db.Connection(func(conn *gorm.DB) error {
		// A fresh session keeps one statement's error from sticking to the next.
		conn = conn.Session(&gorm.Session{})
		return withPragma(conn, "foreign_keys", 0, func() error {
			return withPragma(conn, "legacy_alter_table", 1, func() error {
				return runMigrations(conn)
			})
		})
	})
}

// withPragma runs fn with a connection setting changed. SQLite ignores some
// settings without an error, so both the change and the restore are read back:
// a pooled connection left without foreign key enforcement must not go unnoticed.
func withPragma(conn *gorm.DB, name string, value int, fn func() error) (err error) {
	var previous int
	if err := conn.Raw("PRAGMA " + name).Scan(&previous).Error; err != nil {
		return err
	}
	set := func(want int) error {
		if err := conn.Exec(fmt.Sprintf("PRAGMA %s = %d", name, want)).Error; err != nil {
			return err
		}
		var got int
		if err := conn.Raw("PRAGMA " + name).Scan(&got).Error; err != nil {
			return err
		}
		if got != want {
			return fmt.Errorf("PRAGMA %s is %d, want %d", name, got, want)
		}
		return nil
	}
	if err := set(value); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, set(previous)) }()
	return fn()
}

func runMigrations(db *gorm.DB) error {
	// Auto-migrate non-problematic models first
	err := preservingSchemaMetadata(db, func(tx *gorm.DB) error {
		return tx.AutoMigrate(&models.Category{}, &models.Settings{}, &models.APIKey{}, &models.ExchangeRate{}, &models.Tag{})
	})
	if err != nil {
		return err
	}

	// Run specific migrations
	migrations := []func(*gorm.DB) error{
		migrateCategoriesToDynamic,
		migrateCurrencyFields,
		migrateDateCalculationVersioning,
		migrateSubscriptionIcons,
		migrateReminderTracking,
		migrateCancellationReminderTracking,
		migrateScheduleInterval,
		migrateReminderEnabled,
		migrateAutopay,
		migrateSubscriptionLabel,
		migrateShareCount,
		migrateReminderWindows,
		migrateCancellationNoticeDays,
		migrateToCancel,
	}

	for _, migration := range migrations {
		if err := migration(db); err != nil {
			return err
		}
	}

	if err := autoMigrateSubscriptions(db); err != nil {
		return err
	}

	if err := migrateUncategorizedSubscriptions(db); err != nil {
		return err
	}

	// Ensure the subscription_tags join table exists (separate from Subscription/Tag tables
	// so the many2many relation works regardless of column-order quirks on legacy schemas).
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS subscription_tags (
			subscription_id INTEGER NOT NULL,
			tag_id INTEGER NOT NULL,
			PRIMARY KEY (subscription_id, tag_id),
			FOREIGN KEY (subscription_id) REFERENCES subscriptions(id) ON DELETE CASCADE,
			FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
		)
	`).Error; err != nil {
		log.Printf("Warning: Could not create subscription_tags table: %v", err)
	}

	return nil
}

// autoMigrateSubscriptions runs after the migrations above have added every column
// the application needs, so an existing database works without it: GORM cannot
// parse the DDL of some older tables, and those have always carried on unchanged.
// A failure therefore rolls back and logs instead of stopping startup. A new
// database has no table yet, and needs this step to succeed.
func autoMigrateSubscriptions(db *gorm.DB) error {
	err := preservingSchemaMetadata(db, func(tx *gorm.DB) error {
		return tx.AutoMigrate(&models.Subscription{})
	})
	if err == nil {
		return nil
	}
	if !db.Migrator().HasTable(&models.Subscription{}) {
		return fmt.Errorf("create subscriptions table: %w", err)
	}
	log.Printf("Note: subscriptions schema sync rolled back, database left as it was: %v", err)
	return nil
}

// schemaMetadata is what a table rebuild can lose without reporting an error.
type schemaMetadata struct {
	objects   []schemaObject
	sequences []tableSequence
	contents  []tableContents
}

// schemaObject is an index or trigger with its original DDL.
type schemaObject struct {
	Type string
	Name string
	SQL  string
}

// tableSequence is an AUTOINCREMENT high-water mark, which outlives deleted IDs.
type tableSequence struct {
	Name          string
	Seq           int64
	AutoIncrement bool
}

// tableContents holds a table's row and per-column non-null counts, and the query
// that produced them.
type tableContents struct {
	table  string
	query  string
	counts string
}

// preservingSchemaMetadata runs change, which may rebuild tables, in a transaction
// that also puts back what a rebuild loses: GORM's rebuild and DROP TABLE discard a
// table's indexes, triggers and sqlite_sequence row. Any failure, including one
// while restoring, rolls everything back. The caller has foreign keys off.
func preservingSchemaMetadata(db *gorm.DB, change func(tx *gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		before, err := captureSchemaMetadata(tx)
		if err != nil {
			return fmt.Errorf("capture schema metadata: %w", err)
		}
		if err := change(tx); err != nil {
			return err
		}
		if err := before.restore(tx); err != nil {
			return fmt.Errorf("restore schema metadata: %w", err)
		}
		return nil
	})
}

func captureSchemaMetadata(tx *gorm.DB) (*schemaMetadata, error) {
	var metadata schemaMetadata
	// Original creation order, which is what decides the firing order of triggers.
	if err := tx.Raw("SELECT type, name, sql FROM sqlite_master WHERE type IN ('index', 'trigger') AND sql IS NOT NULL ORDER BY rowid").Scan(&metadata.objects).Error; err != nil {
		return nil, err
	}

	var hasSequences int64
	if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'sqlite_sequence'").Scan(&hasSequences).Error; err != nil {
		return nil, err
	}
	if hasSequences > 0 {
		if err := tx.Raw("SELECT s.name, MAX(s.seq) AS seq, COALESCE(INSTR(UPPER(m.sql), 'AUTOINCREMENT') > 0, 0) AS auto_increment FROM sqlite_sequence s LEFT JOIN sqlite_master m ON m.type = 'table' AND m.name = s.name GROUP BY s.name").Scan(&metadata.sequences).Error; err != nil {
			return nil, err
		}
	}

	var tables []string
	// A virtual table's module may not be compiled in, and a rebuild never touches one.
	if err := tx.Raw("SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\\_%' ESCAPE '\\' AND sql NOT LIKE 'CREATE VIRTUAL%'").Scan(&tables).Error; err != nil {
		return nil, err
	}
	for _, table := range tables {
		var columns []string
		if err := tx.Raw("SELECT name FROM pragma_table_info(?)", table).Scan(&columns).Error; err != nil {
			return nil, err
		}
		contents := tableContents{table: table, query: "SELECT COUNT(*)"}
		for _, column := range columns {
			// Backticks: SQLite reads a double-quoted unknown name as a string.
			contents.query += " || ',' || COUNT(" + quoteIdentifier(column) + ")"
		}
		contents.query += " FROM " + quoteIdentifier(table)
		if err := tx.Raw(contents.query).Scan(&contents.counts).Error; err != nil {
			return nil, err
		}
		metadata.contents = append(metadata.contents, contents)
	}
	return &metadata, nil
}

func (m *schemaMetadata) restore(tx *gorm.DB) error {
	for _, object := range m.objects {
		var exists int64
		if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?", object.Type, object.Name).Scan(&exists).Error; err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if err := tx.Exec(object.SQL).Error; err != nil {
			return fmt.Errorf("recreate %s %s: %w", object.Type, object.Name, err)
		}
	}

	for _, sequence := range m.sequences {
		if err := restoreSequence(tx, sequence); err != nil {
			return fmt.Errorf("sequence for %s: %w", sequence.Name, err)
		}
	}

	// GORM builds its copy statement from a regular expression, and leaves out any
	// column it does not match: the rebuild succeeds and the values are gone.
	for _, contents := range m.contents {
		var counts string
		if err := tx.Raw(contents.query).Scan(&counts).Error; err != nil {
			return fmt.Errorf("verify %s: %w", contents.table, err)
		}
		if counts != contents.counts {
			return fmt.Errorf("verify %s: row and column counts changed from %s to %s", contents.table, contents.counts, counts)
		}
	}
	return nil
}

func restoreSequence(tx *gorm.DB, sequence tableSequence) error {
	// Manually edited databases can contain stale sequence entries for tables
	// that never used AUTOINCREMENT. Only enforce what existed before migration.
	if !sequence.AutoIncrement {
		return nil
	}
	var tableSQL string
	if err := tx.Raw("SELECT COALESCE(MAX(sql), '') FROM sqlite_master WHERE type = 'table' AND name = ?", sequence.Name).Scan(&tableSQL).Error; err != nil {
		return err
	}
	if tableSQL == "" {
		return nil // The row outlived its table before this migration began.
	}
	if !strings.Contains(strings.ToUpper(tableSQL), "AUTOINCREMENT") {
		return errors.New("table lost AUTOINCREMENT")
	}
	// An emptied table has no row left to update, so the row is replaced.
	var current int64
	if err := tx.Raw("SELECT COALESCE(MAX(seq), -1) FROM sqlite_sequence WHERE name = ?", sequence.Name).Scan(&current).Error; err != nil {
		return err
	}
	if current >= sequence.Seq {
		return nil
	}
	if err := tx.Exec("DELETE FROM sqlite_sequence WHERE name = ?", sequence.Name).Error; err != nil {
		return err
	}
	return tx.Exec("INSERT INTO sqlite_sequence (name, seq) VALUES (?, ?)", sequence.Name, sequence.Seq).Error
}

func quoteIdentifier(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

// migrateCategoriesToDynamic handles the v0.3.0 migration from string categories to category IDs
func migrateCategoriesToDynamic(db *gorm.DB) error {
	// Check if migration is needed by looking for the old category column
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='category'").Scan(&count)

	if count == 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Converting categories to dynamic system...")

	// First ensure default categories exist
	defaultCategories := []string{"Entertainment", "Productivity", "Storage", "Software", "Fitness", "Education", "Food", "Travel", "Business", "Other"}
	var categories []models.Category
	db.Find(&categories)

	if len(categories) == 0 {
		for _, name := range defaultCategories {
			db.Create(&models.Category{Name: name})
		}
		db.Find(&categories) // Reload categories
	}

	// Create category map
	categoryMap := make(map[string]uint)
	for _, cat := range categories {
		categoryMap[cat.Name] = cat.ID
	}

	// Get all subscriptions that need migration
	type OldSubscription struct {
		ID       uint
		Category string
	}

	var oldSubs []OldSubscription
	db.Table("subscriptions").Select("id, category").Scan(&oldSubs)

	// Update each subscription with the appropriate category_id
	for _, sub := range oldSubs {
		if sub.Category != "" {
			if catID, exists := categoryMap[sub.Category]; exists {
				db.Table("subscriptions").Where("id = ?", sub.ID).Update("category_id", catID)
			} else {
				// If category doesn't exist, use "Other"
				if otherID, exists := categoryMap["Other"]; exists {
					db.Table("subscriptions").Where("id = ?", sub.ID).Update("category_id", otherID)
				}
			}
		}
	}

	// SQLite limitation: we can't drop the old category column
	// The repository layer now handles both old and new schemas transparently
	// This ensures backward compatibility without data loss

	log.Println("Migration completed: Categories converted to dynamic system")
	return nil
}

// migrateUncategorizedSubscriptions stores uncategorized subscriptions as NULL.
// Older releases declared category_id NOT NULL, which AutoMigrate does not relax.
// Rows written before foreign keys were enforced can hold category_id 0 or the
// ID of a deleted category. Both already read as having no category, but saving
// such a row would now fail the foreign key.
func migrateUncategorizedSubscriptions(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'category_id'").Scan(&count)
	if count == 0 {
		return nil
	}

	var notNull int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'category_id' AND \"notnull\" = 1").Scan(&notNull)
	if notNull > 0 {
		log.Println("Running migration: Allowing uncategorized subscriptions...")
		if err := dropCategoryNotNull(db); err != nil {
			return fmt.Errorf("make subscriptions.category_id nullable: %w", err)
		}
	}

	result := db.Exec(`
		UPDATE subscriptions SET category_id = NULL
		WHERE category_id IS NOT NULL AND category_id NOT IN (SELECT id FROM categories)
	`)
	if result.Error != nil {
		return fmt.Errorf("clear invalid subscription categories: %w", result.Error)
	}
	if result.RowsAffected > 0 {
		log.Printf("Migration completed: %d subscriptions without a valid category marked uncategorized", result.RowsAffected)
	}
	return nil
}

var (
	categoryNotNullPattern = regexp.MustCompile("(?i)([`\"\\[]?\\bcategory_id\\b[`\"\\]]?\\s+[^,]*?)\\s+NOT\\s+NULL")
	subscriptionsDDLName   = regexp.MustCompile("(?i)^\\s*CREATE\\s+TABLE\\s+[`\"\\[]?subscriptions[`\"\\]]?")
)

// dropCategoryNotNull rebuilds subscriptions without NOT NULL on category_id, the
// only way SQLite can change a column constraint. It edits the stored DDL rather
// than using Migrator.AlterColumn, whose parser does not find the unquoted column
// names in tables that pre-date GORM's quoting. The caller has foreign keys off.
func dropCategoryNotNull(db *gorm.DB) error {
	return preservingSchemaMetadata(db, func(tx *gorm.DB) error {
		var tableSQL string
		if err := tx.Raw("SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'subscriptions'").Scan(&tableSQL).Error; err != nil {
			return err
		}
		rebuilt := categoryNotNullPattern.ReplaceAllString(tableSQL, "$1")
		rebuilt = subscriptionsDDLName.ReplaceAllString(rebuilt, "CREATE TABLE subscriptions__rebuild")
		if !strings.Contains(rebuilt, "subscriptions__rebuild") {
			return fmt.Errorf("unexpected table definition: %s", tableSQL)
		}

		for _, statement := range []string{
			rebuilt,
			"INSERT INTO subscriptions__rebuild SELECT * FROM subscriptions",
			"DROP TABLE subscriptions",
			"ALTER TABLE subscriptions__rebuild RENAME TO subscriptions",
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// migrateCurrencyFields adds original_currency field to existing subscriptions
func migrateCurrencyFields(db *gorm.DB) error {
	// Check if original_currency column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='original_currency'").Scan(&count)

	if count > 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Adding currency fields...")

	// Add original_currency column with default 'USD'
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN original_currency TEXT DEFAULT 'USD'").Error; err != nil {
		// Column might already exist, that's okay
		log.Printf("Note: Could not add original_currency column: %v", err)
	}

	// Set USD as default for existing subscriptions
	if err := db.Exec("UPDATE subscriptions SET original_currency = 'USD' WHERE original_currency IS NULL OR original_currency = ''").Error; err != nil {
		log.Printf("Warning: Could not update existing subscriptions with default currency: %v", err)
	}

	log.Println("Migration completed: Currency fields added")
	return nil
}

// migrateDateCalculationVersioning adds date_calculation_version field for versioned date logic
func migrateDateCalculationVersioning(db *gorm.DB) error {
	// Check if date_calculation_version column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='date_calculation_version'").Scan(&count)

	if count > 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Adding date calculation versioning...")

	// Add date_calculation_version column with default 1 (existing logic)
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN date_calculation_version INTEGER DEFAULT 1").Error; err != nil {
		// Column might already exist, that's okay
		log.Printf("Note: Could not add date_calculation_version column: %v", err)
	}

	// Set version 1 for all existing subscriptions (maintain backward compatibility)
	if err := db.Exec("UPDATE subscriptions SET date_calculation_version = 1 WHERE date_calculation_version IS NULL").Error; err != nil {
		log.Printf("Warning: Could not update existing subscriptions with default version: %v", err)
	}

	log.Println("Migration completed: Date calculation versioning added")
	return nil
}

// migrateSubscriptionIcons adds icon_url field to subscriptions table
func migrateSubscriptionIcons(db *gorm.DB) error {
	// Check if icon_url column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='icon_url'").Scan(&count)

	if count > 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Adding subscription icon URLs...")

	// Add icon_url column (nullable, empty string default)
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN icon_url TEXT DEFAULT ''").Error; err != nil {
		// Column might already exist, that's okay
		log.Printf("Note: Could not add icon_url column: %v", err)
	}

	// Set empty string as default for existing subscriptions
	if err := db.Exec("UPDATE subscriptions SET icon_url = '' WHERE icon_url IS NULL").Error; err != nil {
		log.Printf("Warning: Could not update existing subscriptions with default icon_url: %v", err)
	}

	log.Println("Migration completed: Subscription icon URLs added")
	return nil
}

// migrateReminderTracking adds fields to track when reminders were sent
func migrateReminderTracking(db *gorm.DB) error {
	// Check if last_reminder_sent column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='last_reminder_sent'").Scan(&count)

	if count > 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Adding reminder tracking fields...")

	// Add last_reminder_sent column
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_reminder_sent DATETIME").Error; err != nil {
		log.Printf("Note: Could not add last_reminder_sent column: %v", err)
	}

	// Add last_reminder_renewal_date column
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_reminder_renewal_date DATETIME").Error; err != nil {
		log.Printf("Note: Could not add last_reminder_renewal_date column: %v", err)
	}

	log.Println("Migration completed: Reminder tracking fields added")
	return nil
}

// migrateCancellationReminderTracking adds fields to track when cancellation reminders were sent
func migrateCancellationReminderTracking(db *gorm.DB) error {
	// Check if last_cancellation_reminder_sent column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='last_cancellation_reminder_sent'").Scan(&count)

	if count > 0 {
		// Migration already completed
		return nil
	}

	log.Println("Running migration: Adding cancellation reminder tracking fields...")

	// Add last_cancellation_reminder_sent column
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_cancellation_reminder_sent DATETIME").Error; err != nil {
		log.Printf("Note: Could not add last_cancellation_reminder_sent column: %v", err)
	}

	// Add last_cancellation_reminder_date column
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_cancellation_reminder_date DATETIME").Error; err != nil {
		log.Printf("Note: Could not add last_cancellation_reminder_date column: %v", err)
	}

	log.Println("Migration completed: Cancellation reminder tracking fields added")
	return nil
}

func migrateScheduleInterval(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name='schedule_interval'").Scan(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding schedule interval field...")

	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN schedule_interval INTEGER DEFAULT 1").Error; err != nil {
		log.Printf("Note: Could not add schedule_interval column: %v", err)
	}

	if err := db.Exec("UPDATE subscriptions SET schedule_interval = 1 WHERE schedule_interval IS NULL").Error; err != nil {
		log.Printf("Warning: Could not update existing subscriptions with default schedule_interval: %v", err)
	}

	log.Println("Migration completed: Schedule interval field added")
	return nil
}

// migrateSubscriptionLabel adds a free-form sub-label field to distinguish multiple
// subscriptions of the same service (e.g. domain name at a registrar, family member's phone line).
func migrateSubscriptionLabel(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'label'").Count(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding subscription label field...")

	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN label TEXT DEFAULT ''").Error; err != nil {
		log.Printf("Note: Could not add label column: %v", err)
	}

	log.Println("Migration completed: Subscription label field added")
	return nil
}

// migrateShareCount adds the share_count field for split-cost subscriptions.
func migrateShareCount(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'share_count'").Count(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding share_count field...")

	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN share_count INTEGER DEFAULT 1").Error; err != nil {
		log.Printf("Note: Could not add share_count column: %v", err)
	}

	if err := db.Exec("UPDATE subscriptions SET share_count = 1 WHERE share_count IS NULL OR share_count < 1").Error; err != nil {
		log.Printf("Warning: Could not backfill share_count defaults: %v", err)
	}

	log.Println("Migration completed: share_count field added")
	return nil
}

// migrateReminderWindows adds last_reminder_window / last_cancellation_reminder_window fields
// that track which configured notification window has already fired for the current renewal/cancellation date.
// Default -1 means "no window fired yet"; existing rows start fresh.
func migrateReminderWindows(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'last_reminder_window'").Count(&count)
	if count == 0 {
		log.Println("Running migration: Adding last_reminder_window field...")
		if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_reminder_window INTEGER DEFAULT -1").Error; err != nil {
			log.Printf("Note: Could not add last_reminder_window column: %v", err)
		}
	}

	var count2 int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'last_cancellation_reminder_window'").Count(&count2)
	if count2 == 0 {
		log.Println("Running migration: Adding last_cancellation_reminder_window field...")
		if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN last_cancellation_reminder_window INTEGER DEFAULT -1").Error; err != nil {
			log.Printf("Note: Could not add last_cancellation_reminder_window column: %v", err)
		}
	}

	return nil
}

// migrateReminderEnabled adds per-subscription reminder toggle field
func migrateReminderEnabled(db *gorm.DB) error {
	// Check if column already exists
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'reminder_enabled'").Count(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding per-subscription reminder_enabled field...")

	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN reminder_enabled INTEGER DEFAULT 1").Error; err != nil {
		log.Printf("Note: Could not add reminder_enabled column: %v", err)
	}

	// Set all existing subscriptions to enabled
	db.Exec("UPDATE subscriptions SET reminder_enabled = 1 WHERE reminder_enabled IS NULL")

	log.Println("Migration completed: reminder_enabled field added")
	return nil
}

// migrateAutopay adds nullable payment-mode tracking. Existing rows stay unknown.
func migrateAutopay(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'autopay'").Count(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding nullable autopay field...")
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN autopay INTEGER").Error; err != nil {
		log.Printf("Note: Could not add autopay column: %v", err)
	}

	return nil
}

// migrateCancellationNoticeDays adds the per-subscription cancellation notice
// period. Existing rows default to 0 (no notice period), preserving behavior.
func migrateCancellationNoticeDays(db *gorm.DB) error {
	var count int64
	db.Raw("SELECT COUNT(*) FROM pragma_table_info('subscriptions') WHERE name = 'cancellation_notice_days'").Count(&count)

	if count > 0 {
		return nil
	}

	log.Println("Running migration: Adding cancellation_notice_days field...")
	if err := db.Exec("ALTER TABLE subscriptions ADD COLUMN cancellation_notice_days INTEGER DEFAULT 0").Error; err != nil {
		log.Printf("Note: Could not add cancellation_notice_days column: %v", err)
	}
	db.Exec("UPDATE subscriptions SET cancellation_notice_days = 0 WHERE cancellation_notice_days IS NULL")

	return nil
}

// migrateToCancel adds the to-cancel intent and its reminder state. Existing rows
// are not opted in. Every query for these fields needs them, and the AutoMigrate
// that follows ignores its errors, so a failure here stops startup. GORM adds the
// columns itself so AutoMigrate sees no difference and does not rebuild the table.
func migrateToCancel(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Subscription{}) {
		return nil // A new database gets these columns from AutoMigrate.
	}
	for _, field := range []string{"ToCancel", "ToCancelReminderDays", "LastToCancelReminderSent", "LastToCancelReminderAnchor"} {
		if db.Migrator().HasColumn(&models.Subscription{}, field) {
			continue
		}
		log.Printf("Running migration: Adding %s field...", field)
		if err := db.Migrator().AddColumn(&models.Subscription{}, field); err != nil {
			return fmt.Errorf("failed to add %s column: %w", field, err)
		}
	}
	return nil
}

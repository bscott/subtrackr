package database

import (
	"log"
	"subtrackr/internal/models"

	"gorm.io/gorm"
)

// RunMigrations executes all database migrations
func RunMigrations(db *gorm.DB) error {
	// Auto-migrate non-problematic models first
	err := db.AutoMigrate(&models.Category{}, &models.Settings{}, &models.APIKey{}, &models.ExchangeRate{})
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
	}

	for _, migration := range migrations {
		if err := migration(db); err != nil {
			return err
		}
	}

	// Try to auto-migrate subscriptions after the category migration
	// This might fail on existing databases but that's okay
	db.AutoMigrate(&models.Subscription{})

	return nil
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
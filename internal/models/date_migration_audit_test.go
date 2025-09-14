package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupAuditTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Migrate the schema
	err = db.AutoMigrate(&Subscription{}, &DateMigrationLog{})
	if err != nil {
		t.Fatalf("Failed to migrate schema: %v", err)
	}

	return db
}

func TestNewDateMigrationSafetyCheck(t *testing.T) {
	db := setupAuditTestDB(t)

	safety := NewDateMigrationSafetyCheck(db)

	assert.NotNil(t, safety, "SafetyCheck should not be nil")
	assert.Equal(t, db, safety.db, "Database should be set correctly")
}

func TestCompareCalculationVersions(t *testing.T) {
	db := setupAuditTestDB(t)
	safety := NewDateMigrationSafetyCheck(db)

	// Create a test subscription
	startDate := time.Date(2025, 1, 31, 10, 0, 0, 0, time.UTC)
	sub := &Subscription{
		Name:      "Test Subscription",
		Cost:      15.99,
		Schedule:  "Monthly",
		Status:    "Active",
		StartDate: &startDate,
		DateCalculationVersion: 1,
	}

	err := db.Create(sub).Error
	assert.NoError(t, err, "Should create test subscription")

	// Compare V1 vs V2 calculations
	v1Date, v2Date, err := safety.CompareCalculationVersions(sub.ID)
	assert.NoError(t, err, "Should compare calculations successfully")

	assert.NotNil(t, v1Date, "V1 calculation should return a date")
	assert.NotNil(t, v2Date, "V2 calculation should return a date")

	// Both should be in the future
	assert.True(t, v1Date.After(time.Now()), "V1 date should be in future")
	assert.True(t, v2Date.After(time.Now()), "V2 date should be in future")
}

func TestGetMigrationStats(t *testing.T) {
	db := setupAuditTestDB(t)
	safety := NewDateMigrationSafetyCheck(db)

	// Create test subscriptions with different versions
	subs := []Subscription{
		{Name: "V1 Sub 1", Cost: 10, Schedule: "Monthly", Status: "Active", DateCalculationVersion: 1},
		{Name: "V1 Sub 2", Cost: 20, Schedule: "Annual", Status: "Active", DateCalculationVersion: 1},
		{Name: "V2 Sub 1", Cost: 15, Schedule: "Monthly", Status: "Active", DateCalculationVersion: 2},
	}

	for _, sub := range subs {
		err := db.Create(&sub).Error
		assert.NoError(t, err)
	}

	// Create a migration log entry
	log := &DateMigrationLog{
		SubscriptionID:     subs[0].ID,
		OldVersion:        1,
		NewVersion:        2,
		OldRenewalDate:    nil,
		NewRenewalDate:    nil,
		MigrationReason:   "Test migration",
		MigratedAt:        time.Now(),
	}
	err := db.Create(log).Error
	assert.NoError(t, err)

	stats, err := safety.GetMigrationStats()
	assert.NoError(t, err, "Should get migration stats successfully")

	assert.Equal(t, int64(2), stats["v1_subscriptions"], "Should have 2 V1 subscriptions")
	assert.Equal(t, int64(1), stats["v2_subscriptions"], "Should have 1 V2 subscription")
	assert.Equal(t, int64(1), stats["total_migrations"], "Should have 1 migration logged")
}
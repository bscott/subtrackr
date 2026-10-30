package repository

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/sortorder"
)

// setupIntegrityTestDB uses the production connection settings and migrations,
// so foreign keys are enforced the way they are on a running server.
func setupIntegrityTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := database.Initialize(filepath.Join(t.TempDir(), "integrity.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	var foreignKeys int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error)
	require.Equal(t, 1, foreignKeys)
	return db
}

func categoryIDIsNull(t *testing.T, db *gorm.DB, id uint) bool {
	t.Helper()
	var count int64
	require.NoError(t, db.Raw("SELECT COUNT(*) FROM subscriptions WHERE id = ? AND category_id IS NULL", id).Scan(&count).Error)
	return count == 1
}

func newUncategorized(name string) *models.Subscription {
	return &models.Subscription{Name: name, Cost: 5, Schedule: "Monthly", Status: "Active", OriginalCurrency: "USD", ReminderEnabled: true}
}

func TestUncategorizedSubscriptionReadsAsZeroAndStoresNull(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)

	created, err := repo.Create(newUncategorized("No category"))
	require.NoError(t, err)
	assert.Zero(t, created.CategoryID)
	assert.True(t, categoryIDIsNull(t, db, created.ID))

	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.CategoryID)
	assert.Equal(t, models.Category{}, stored.Category)

	// The API keeps its shape: a number, and a category object.
	raw, err := json.Marshal(stored)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.EqualValues(t, 0, decoded["category_id"])
	assert.Equal(t, "", decoded["category"].(map[string]any)["name"])

	all, err := repo.GetAll()
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Zero(t, all[0].CategoryID)

	sorted, err := repo.GetAllSorted([]sortorder.Rule{{Field: "category", Direction: "asc"}})
	require.NoError(t, err)
	require.Len(t, sorted, 1)
	assert.Equal(t, "No category", sorted[0].Name)
}

func TestUpdateMovesSubscriptionOnAndOffACategory(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)
	category, err := NewCategoryRepository(db).Create(&models.Category{Name: "Video"})
	require.NoError(t, err)

	created, err := repo.Create(newUncategorized("Movable"))
	require.NoError(t, err)

	// A routine write, such as a reminder pass, on an uncategorized row.
	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	stored.Notes = "still uncategorized"
	updated, err := repo.Update(created.ID, stored)
	require.NoError(t, err)
	assert.Equal(t, "still uncategorized", updated.Notes)
	assert.True(t, categoryIDIsNull(t, db, created.ID))

	updated.CategoryID = category.ID
	updated, err = repo.Update(created.ID, updated)
	require.NoError(t, err)
	assert.Equal(t, category.ID, updated.CategoryID)
	assert.Equal(t, "Video", updated.Category.Name)

	updated.CategoryID = 0
	updated.Category = models.Category{}
	updated, err = repo.Update(created.ID, updated)
	require.NoError(t, err)
	assert.Zero(t, updated.CategoryID)
	assert.Empty(t, updated.Category.Name)
	assert.True(t, categoryIDIsNull(t, db, created.ID))
}

func TestForeignKeyStillRejectsUnknownCategory(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)

	bad := newUncategorized("Bad category")
	bad.CategoryID = 999
	_, err := repo.Create(bad)
	require.Error(t, err)

	created, err := repo.Create(newUncategorized("Good"))
	require.NoError(t, err)
	created.CategoryID = 999
	_, err = repo.Update(created.ID, created)
	require.Error(t, err)
	assert.True(t, categoryIDIsNull(t, db, created.ID), "the failed update changed nothing")
}

func TestCreateStillSavesANewCategoryWithoutAnID(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)

	sub := newUncategorized("With new category")
	sub.Category = models.Category{Name: "Created inline"}
	created, err := repo.Create(sub)
	require.NoError(t, err)
	assert.NotZero(t, created.CategoryID)
	assert.False(t, categoryIDIsNull(t, db, created.ID))
}

func TestLegacyCategoryColumnSupportsUncategorized(t *testing.T) {
	db := setupIntegrityTestDB(t)
	require.NoError(t, db.Exec("ALTER TABLE subscriptions ADD COLUMN category TEXT NOT NULL DEFAULT 'legacy'").Error)
	repo := NewSubscriptionRepository(db)
	category, err := NewCategoryRepository(db).Create(&models.Category{Name: "Video"})
	require.NoError(t, err)

	created, err := repo.Create(newUncategorized("Legacy"))
	require.NoError(t, err)
	assert.True(t, categoryIDIsNull(t, db, created.ID))

	legacyName := func() string {
		var name string
		require.NoError(t, db.Raw("SELECT category FROM subscriptions WHERE id = ?", created.ID).Scan(&name).Error)
		return name
	}
	assert.Empty(t, legacyName())

	created.CategoryID = category.ID
	updated, err := repo.Update(created.ID, created)
	require.NoError(t, err)
	assert.Equal(t, category.ID, updated.CategoryID)
	assert.Equal(t, "Video", legacyName())

	updated.CategoryID = 0
	_, err = repo.Update(created.ID, updated)
	require.NoError(t, err)
	assert.True(t, categoryIDIsNull(t, db, created.ID))
	assert.Empty(t, legacyName())
}

func TestDeleteTaggedSubscriptionKeepsSharedTags(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)
	tagRepo := NewTagRepository(db)

	first, err := repo.Create(newUncategorized("First"))
	require.NoError(t, err)
	second, err := repo.Create(newUncategorized("Second"))
	require.NoError(t, err)
	tags, err := tagRepo.FindOrCreateByNames([]string{"shared"})
	require.NoError(t, err)
	require.NoError(t, tagRepo.ReplaceSubscriptionTags(first.ID, tags))
	require.NoError(t, tagRepo.ReplaceSubscriptionTags(second.ID, tags))

	require.NoError(t, repo.Delete(first.ID))

	_, err = repo.GetByID(first.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	survivor, err := repo.GetByID(second.ID)
	require.NoError(t, err)
	require.Len(t, survivor.Tags, 1)
	assert.Equal(t, "shared", survivor.Tags[0].Name)
}

func TestDeleteRollsBackTagLinksWhenSubscriptionDeleteFails(t *testing.T) {
	db := setupIntegrityTestDB(t)
	repo := NewSubscriptionRepository(db)
	tagRepo := NewTagRepository(db)

	created, err := repo.Create(newUncategorized("Protected"))
	require.NoError(t, err)
	tags, err := tagRepo.FindOrCreateByNames([]string{"kept"})
	require.NoError(t, err)
	require.NoError(t, tagRepo.ReplaceSubscriptionTags(created.ID, tags))
	require.NoError(t, db.Exec(`CREATE TRIGGER refuse_delete BEFORE DELETE ON subscriptions
		BEGIN SELECT RAISE(ABORT, 'delete refused'); END`).Error)

	require.ErrorContains(t, repo.Delete(created.ID), "delete refused")

	stored, err := repo.GetByID(created.ID)
	require.NoError(t, err)
	require.Len(t, stored.Tags, 1, "the failed delete left the tag link in place")
}

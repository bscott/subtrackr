package service

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
)

func TestDuplicateKeepsCategoryStateWithForeignKeysOn(t *testing.T) {
	db, err := database.Initialize(filepath.Join(t.TempDir(), "duplicate.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	categoryRepo := repository.NewCategoryRepository(db)
	categories := NewCategoryService(categoryRepo)
	subscriptions := NewSubscriptionService(repository.NewSubscriptionRepository(db), categories)
	category, err := categories.Create(&models.Category{Name: "Video"})
	require.NoError(t, err)

	uncategorized, err := subscriptions.Create(&models.Subscription{Name: "Plain", Cost: 3, Schedule: "Monthly", Status: "Active"})
	require.NoError(t, err)
	categorized, err := subscriptions.Create(&models.Subscription{Name: "Filed", Cost: 3, Schedule: "Monthly", Status: "Active", CategoryID: category.ID})
	require.NoError(t, err)

	copyOfPlain, _, err := subscriptions.Duplicate(uncategorized.ID)
	require.NoError(t, err)
	assert.Equal(t, "Plain (Copy)", copyOfPlain.Name)
	assert.Zero(t, copyOfPlain.CategoryID)

	copyOfFiled, _, err := subscriptions.Duplicate(categorized.ID)
	require.NoError(t, err)
	stored, err := subscriptions.GetByID(copyOfFiled.ID)
	require.NoError(t, err)
	assert.Equal(t, category.ID, stored.CategoryID)
	assert.Equal(t, "Video", stored.Category.Name)
}

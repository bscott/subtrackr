package repository

import (
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"subtrackr/internal/models"
	"subtrackr/internal/sortorder"
)

func setupSubscriptionTestDB(t *testing.T) *SubscriptionRepository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	if err := db.AutoMigrate(&models.Subscription{}, &models.Category{}, &models.Tag{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return NewSubscriptionRepository(db)
}

func createTestSubscriptions(t *testing.T, repo *SubscriptionRepository, subscriptions ...models.Subscription) {
	t.Helper()
	for i := range subscriptions {
		if _, err := repo.Create(&subscriptions[i]); err != nil {
			t.Fatalf("failed to seed subscription %q: %v", subscriptions[i].Name, err)
		}
	}
}

func subscriptionNames(subscriptions []models.Subscription) []string {
	names := make([]string, len(subscriptions))
	for i, subscription := range subscriptions {
		names[i] = subscription.Name
	}
	return names
}

func TestGetAllSortedPreservesRulePriority(t *testing.T) {
	repo := setupSubscriptionTestDB(t)
	createTestSubscriptions(t, repo,
		models.Subscription{Name: "Active high", Cost: 20, Schedule: "Monthly", Status: "Active", OriginalCurrency: "USD"},
		models.Subscription{Name: "Active low", Cost: 10, Schedule: "Monthly", Status: "Active", OriginalCurrency: "USD"},
		models.Subscription{Name: "Paused high", Cost: 30, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD"},
		models.Subscription{Name: "Paused low", Cost: 20, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD"},
	)

	subscriptions, err := repo.GetAllSorted([]sortorder.Rule{
		{Field: "status", Direction: "asc"},
		{Field: "cost", Direction: "desc"},
	})
	if err != nil {
		t.Fatalf("GetAllSorted failed: %v", err)
	}

	want := []string{"Active high", "Active low", "Paused high", "Paused low"}
	if got := subscriptionNames(subscriptions); !reflect.DeepEqual(got, want) {
		t.Fatalf("subscription order = %v, want %v", got, want)
	}
}

func TestGetAllSortedEmptyRulesUsesCreatedAtDescending(t *testing.T) {
	repo := setupSubscriptionTestDB(t)
	base := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	createTestSubscriptions(t, repo,
		models.Subscription{Name: "Oldest", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CreatedAt: base},
		models.Subscription{Name: "Newest", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CreatedAt: base.Add(2 * time.Hour)},
		models.Subscription{Name: "Middle", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CreatedAt: base.Add(time.Hour)},
	)

	subscriptions, err := repo.GetAllSorted(nil)
	if err != nil {
		t.Fatalf("GetAllSorted failed: %v", err)
	}

	want := []string{"Newest", "Middle", "Oldest"}
	if got := subscriptionNames(subscriptions); !reflect.DeepEqual(got, want) {
		t.Fatalf("subscription order = %v, want %v", got, want)
	}
}

func TestGetAllSortedCategoryAndName(t *testing.T) {
	repo := setupSubscriptionTestDB(t)
	categoryA := models.Category{Name: "A"}
	categoryB := models.Category{Name: "B"}
	if err := repo.db.Create(&categoryA).Error; err != nil {
		t.Fatalf("failed to create category A: %v", err)
	}
	if err := repo.db.Create(&categoryB).Error; err != nil {
		t.Fatalf("failed to create category B: %v", err)
	}
	createTestSubscriptions(t, repo,
		models.Subscription{Name: "Alpha", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CategoryID: categoryA.ID},
		models.Subscription{Name: "Zulu", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CategoryID: categoryA.ID},
		models.Subscription{Name: "Beta", Cost: 10, Schedule: "Monthly", Status: "Paused", OriginalCurrency: "USD", CategoryID: categoryB.ID},
	)

	subscriptions, err := repo.GetAllSorted([]sortorder.Rule{
		{Field: "category", Direction: "asc"},
		{Field: "name", Direction: "desc"},
	})
	if err != nil {
		t.Fatalf("GetAllSorted failed: %v", err)
	}

	want := []string{"Zulu", "Alpha", "Beta"}
	if got := subscriptionNames(subscriptions); !reflect.DeepEqual(got, want) {
		t.Fatalf("subscription order = %v, want %v", got, want)
	}
}

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
)

// passRecorder is a local channel recording the order of reminder kinds. The
// renewal send panics when asked to, standing in for a misbehaving provider.
type passRecorder struct {
	kinds          []string
	panicOnRenewal bool
}

func (p *passRecorder) Name() string                                 { return "recorder" }
func (p *passRecorder) IsConfigured() bool                           { return true }
func (p *passRecorder) SendHighCostAlert(*models.Subscription) error { return nil }
func (p *passRecorder) SendRenewalReminder(*models.Subscription, int) error {
	p.kinds = append(p.kinds, "renewal")
	if p.panicOnRenewal {
		panic("provider exploded")
	}
	return nil
}
func (p *passRecorder) SendCancellationReminder(*models.Subscription, int) error {
	p.kinds = append(p.kinds, "cancellation")
	return nil
}
func (p *passRecorder) SendToCancelReminder(*models.Subscription, int) error {
	p.kinds = append(p.kinds, "to-cancel")
	return nil
}

func TestRunReminderPasses_RunsAllInOrderAndSurvivesAPanic(t *testing.T) {
	for _, panics := range []bool{false, true} {
		db, err := database.Initialize(filepath.Join(t.TempDir(), "passes.db"))
		require.NoError(t, err)
		require.NoError(t, database.RunMigrations(db))
		sqlDB, err := db.DB()
		require.NoError(t, err)
		t.Cleanup(func() { sqlDB.Close() })

		settings := service.NewSettingsService(repository.NewSettingsRepository(db))
		require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
		require.NoError(t, settings.SetIntSetting("reminder_days", 7))
		require.NoError(t, settings.SetBoolSetting("cancellation_reminders", true))
		require.NoError(t, settings.SetIntSetting("cancellation_reminder_days", 7))
		subs := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), service.NewCategoryService(repository.NewCategoryRepository(db)))

		soon := time.Now().AddDate(0, 0, 3)
		for _, sub := range []*models.Subscription{
			{Name: "Renewing", Status: "Active", RenewalDate: &soon, ReminderEnabled: true},
			{Name: "Ending", Status: "Cancelled", CancellationDate: &soon, ReminderEnabled: true},
			{Name: "Leaving", Status: "Trial", RenewalDate: &soon, ToCancel: true},
		} {
			sub.Cost, sub.Schedule = 1, "Monthly"
			_, err := subs.Create(sub)
			require.NoError(t, err)
		}

		recorder := &passRecorder{panicOnRenewal: panics}
		runReminderPasses(subs, service.NewNotificationDispatcher(settings, recorder))
		assert.Equal(t, []string{"renewal", "cancellation", "to-cancel"}, recorder.kinds)
	}
}

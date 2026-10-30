package service

import (
	"errors"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChannel is an in-memory NotificationChannel.
type fakeChannel struct {
	name       string
	configured bool
	err        error
	calls      []string
}

func (f *fakeChannel) Name() string       { return f.name }
func (f *fakeChannel) IsConfigured() bool { return f.configured }
func (f *fakeChannel) SendHighCostAlert(*models.Subscription) error {
	f.calls = append(f.calls, "high_cost")
	return f.err
}
func (f *fakeChannel) SendRenewalReminder(*models.Subscription, int) error {
	f.calls = append(f.calls, "renewal")
	return f.err
}
func (f *fakeChannel) SendCancellationReminder(*models.Subscription, int) error {
	f.calls = append(f.calls, "cancellation")
	return f.err
}

func TestDispatcher_SkipsUnconfiguredAndReportsOutcome(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
	ok := &fakeChannel{name: "ok", configured: true}
	broken := &fakeChannel{name: "broken", configured: true, err: errors.New("boom")}
	off := &fakeChannel{name: "off"}
	d := NewNotificationDispatcher(settings, ok, broken, off)

	report := d.SendRenewalReminder(&models.Subscription{Name: "S"}, 3)
	assert.True(t, report.AnyDelivered())
	assert.Equal(t, []string{"ok"}, report.Delivered)
	assert.Equal(t, 2, report.Attempted())
	assert.Equal(t, "broken=boom", report.FailureSummary())
	assert.Empty(t, off.calls, "unconfigured channels are never called")
}

func TestDispatcher_NothingConfiguredOrAllFailedIsNotDelivered(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBoolSetting("cancellation_reminders", true))

	none := NewNotificationDispatcher(settings, &fakeChannel{name: "a"}, &fakeChannel{name: "b"})
	report := none.SendCancellationReminder(&models.Subscription{}, 1)
	assert.False(t, report.AnyDelivered())
	assert.Zero(t, report.Attempted())

	failing := NewNotificationDispatcher(settings,
		&fakeChannel{name: "a", configured: true, err: errors.New("x")},
		&fakeChannel{name: "b", configured: true, err: errors.New("y")})
	report = failing.SendCancellationReminder(&models.Subscription{}, 1)
	assert.False(t, report.AnyDelivered())
	assert.Equal(t, "a=x, b=y", report.FailureSummary())
}

func TestDispatcher_DisabledCategoryAttemptsNothing(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBoolSetting("high_cost_alerts", false))
	ch := &fakeChannel{name: "ok", configured: true}
	report := NewNotificationDispatcher(settings, ch).SendHighCostAlert(&models.Subscription{})
	assert.True(t, report.Disabled)
	assert.False(t, report.AnyDelivered())
	assert.Empty(t, ch.calls)

	// An unset toggle reads as disabled, matching the per-channel checks
	// (GetBoolSetting reports an error for missing keys). Otherwise channels
	// would silently skip and the dispatcher would count that as delivered.
	report = NewNotificationDispatcher(newPushTestSettings(t), ch).SendRenewalReminder(&models.Subscription{}, 1)
	assert.True(t, report.Disabled)
	report = NewNotificationDispatcher(newPushTestSettings(t), ch).SendHighCostAlert(&models.Subscription{})
	assert.True(t, report.Disabled)
	assert.Empty(t, ch.calls)

	enabled := newPushTestSettings(t)
	require.NoError(t, enabled.SetBoolSetting("high_cost_alerts", true))
	report = NewNotificationDispatcher(enabled, ch).SendHighCostAlert(&models.Subscription{})
	assert.True(t, report.AnyDelivered())
}

// TestDispatcher_RealUnconfiguredChannelsAreNotDelivery guards the scheduler
// regression where the webhook service returned nil when unconfigured and
// so counted as a successful delivery.
func TestDispatcher_RealUnconfiguredChannelsAreNotDelivery(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
	d := NewNotificationDispatcher(settings,
		NewEmailService(settings), NewPushoverService(settings), NewWebhookService(settings),
		NewTelegramService(settings), NewNtfyService(settings), NewGotifyService(settings))

	report := d.SendRenewalReminder(pushTestSubscription(), 3)
	assert.False(t, report.AnyDelivered())
	assert.Zero(t, report.Attempted())
}

func newReminderRunnerFixture(t *testing.T) (*SettingsService, *SubscriptionService) {
	t.Helper()
	db := setupRenewalReminderTestDB(t)
	settings := NewSettingsService(repository.NewSettingsRepository(db))
	subs := NewSubscriptionService(repository.NewSubscriptionRepository(db), NewCategoryService(repository.NewCategoryRepository(db)))
	return settings, subs
}

func createDueRenewal(t *testing.T, subs *SubscriptionService) uint {
	t.Helper()
	renewal := time.Now().AddDate(0, 0, 3)
	created, err := subs.Create(&models.Subscription{
		Name: "Due Renewal", Cost: 9.99, Schedule: "Monthly", Status: "Active",
		RenewalDate: &renewal, ReminderEnabled: true,
	})
	require.NoError(t, err)
	return created.ID
}

func createDueCancellation(t *testing.T, subs *SubscriptionService) uint {
	t.Helper()
	cancel := time.Now().AddDate(0, 0, 2).Add(time.Hour)
	created, err := subs.Create(&models.Subscription{
		Name: "Due Cancellation", Cost: 9.99, Schedule: "Monthly", Status: "Cancelled",
		CancellationDate: &cancel, ReminderEnabled: true,
	})
	require.NoError(t, err)
	return created.ID
}

func TestRunRenewalReminders_MarksSentOnlyWhenDelivered(t *testing.T) {
	tests := []struct {
		name     string
		statuses func(t *testing.T, settings *SettingsService)
		wantSent bool
	}{
		{"no channel configured", func(t *testing.T, s *SettingsService) {}, false},
		{"every configured channel fails", func(t *testing.T, s *SettingsService) {
			ntfy := newPushSink(t, 401)
			gotify := newPushSink(t, 400)
			require.NoError(t, s.SaveNtfyConfig(&models.NtfyConfig{ServerURL: ntfy.URL, Topic: "t"}))
			require.NoError(t, s.SaveGotifyConfig(&models.GotifyConfig{ServerURL: gotify.URL, AppToken: "tok"}))
		}, false},
		{"one channel delivers", func(t *testing.T, s *SettingsService) {
			ntfy := newPushSink(t, 401)
			gotify := newPushSink(t)
			require.NoError(t, s.SaveNtfyConfig(&models.NtfyConfig{ServerURL: ntfy.URL, Topic: "t"}))
			require.NoError(t, s.SaveGotifyConfig(&models.GotifyConfig{ServerURL: gotify.URL, AppToken: "tok"}))
		}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings, subs := newReminderRunnerFixture(t)
			require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
			require.NoError(t, settings.SetIntSetting("reminder_days", 7))
			tt.statuses(t, settings)
			id := createDueRenewal(t, subs)
			d := NewNotificationDispatcher(settings, NewWebhookService(settings), fastNtfy(settings), fastGotify(settings))

			result := d.RunRenewalReminders(subs)

			stored, err := subs.GetByID(id)
			require.NoError(t, err)
			if tt.wantSent {
				assert.Equal(t, ReminderRunResult{Sent: 1}, result)
				assert.NotNil(t, stored.LastReminderSent)
				assert.NotNil(t, stored.LastReminderRenewalDate)
				assert.NotZero(t, stored.LastReminderWindow)
			} else {
				assert.Equal(t, ReminderRunResult{Failed: 1}, result)
				assert.Nil(t, stored.LastReminderSent, "an undelivered reminder must be retried on the next run")
			}
		})
	}
}

func TestRunCancellationReminders_MarksSentOnlyWhenDelivered(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		settings, subs := newReminderRunnerFixture(t)
		require.NoError(t, settings.SetBoolSetting("cancellation_reminders", true))
		require.NoError(t, settings.SetIntSetting("cancellation_reminder_days", 7))
		status := 503
		if delivered {
			status = 200
		}
		sink := newPushSink(t, status, status, status)
		require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t"}))
		id := createDueCancellation(t, subs)

		result := NewNotificationDispatcher(settings, fastNtfy(settings)).RunCancellationReminders(subs)

		stored, err := subs.GetByID(id)
		require.NoError(t, err)
		if delivered {
			assert.Equal(t, ReminderRunResult{Sent: 1}, result)
			assert.NotNil(t, stored.LastCancellationReminderSent)
			assert.Len(t, sink.all(), 1)
		} else {
			assert.Equal(t, ReminderRunResult{Failed: 1}, result)
			assert.Nil(t, stored.LastCancellationReminderSent)
			assert.Len(t, sink.all(), 3, "transient failures are retried before giving up")
		}
	}
}

func TestRunReminders_DisabledDoesNothing(t *testing.T) {
	settings, subs := newReminderRunnerFixture(t)
	sink := newPushSink(t)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t"}))
	createDueRenewal(t, subs)
	createDueCancellation(t, subs)
	d := NewNotificationDispatcher(settings, fastNtfy(settings))
	assert.Equal(t, ReminderRunResult{}, d.RunRenewalReminders(subs))
	assert.Equal(t, ReminderRunResult{}, d.RunCancellationReminders(subs))
	assert.Empty(t, sink.all())
}

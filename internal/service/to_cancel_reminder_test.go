package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// toCancelChannel is an in-memory channel that records to-cancel reminders. No
// test in this file reaches a real provider.
type toCancelChannel struct {
	fakeChannel
	days   []int
	onSend func()
}

func (f *toCancelChannel) SendToCancelReminder(_ *models.Subscription, daysLeft int) error {
	f.days = append(f.days, daysLeft)
	if f.onSend != nil {
		f.onSend()
	}
	return f.err
}

func newToCancelFixture(t *testing.T) (*gorm.DB, *SettingsService, *SubscriptionService, *toCancelChannel, *NotificationDispatcher) {
	t.Helper()
	db := setupRenewalReminderTestDB(t)
	settings := NewSettingsService(repository.NewSettingsRepository(db))
	subs := NewSubscriptionService(repository.NewSubscriptionRepository(db), NewCategoryService(repository.NewCategoryRepository(db)))
	channel := &toCancelChannel{fakeChannel: fakeChannel{name: "local", configured: true}}
	return db, settings, subs, channel, NewNotificationDispatcher(settings, channel)
}

// createToCancel creates an opted-in subscription renewing in renewsIn days. It
// has renewal reminders switched off, and no test here enables the global toggle
// unless it says so, which proves the flag is an opt-in of its own.
func createToCancel(t *testing.T, subs *SubscriptionService, status string, renewsIn, notice, reminderDays int) *models.Subscription {
	t.Helper()
	renewal := time.Now().AddDate(0, 0, renewsIn)
	created, err := subs.Create(&models.Subscription{
		Name: "To Cancel", Cost: 9.99, Schedule: "Monthly", Status: status, RenewalDate: &renewal,
		CancellationNoticeDays: notice, ToCancel: true, ToCancelReminderDays: reminderDays,
	})
	require.NoError(t, err)
	stored, err := subs.GetByID(created.ID)
	require.NoError(t, err)
	return stored
}

func reload(t *testing.T, subs *SubscriptionService, id uint) *models.Subscription {
	t.Helper()
	stored, err := subs.GetByID(id)
	require.NoError(t, err)
	return stored
}

func TestToCancel_SentOncePerDeadlineWithoutAnyGlobalToggle(t *testing.T) {
	_, settings, subs, channel, d := newToCancelFixture(t)
	require.NoError(t, settings.SetBoolSetting("renewal_reminders", false))
	sub := createToCancel(t, subs, "Active", 10, 0, 14)
	require.False(t, sub.ReminderEnabled)

	now := time.Now()
	assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, now))
	assert.Equal(t, []int{10}, channel.days)

	stored := reload(t, subs, sub.ID)
	require.NotNil(t, stored.LastToCancelReminderAnchor)
	assert.True(t, stored.LastToCancelReminderAnchor.Equal(*stored.RenewalDate))
	require.NotNil(t, stored.LastToCancelReminderSent)
	assert.True(t, stored.LastToCancelReminderSent.Equal(now))

	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, now), "next run does not repeat it")
	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, now.AddDate(0, 0, 5)), "nor a later one in the window")
	assert.Len(t, channel.days, 1)
}

func TestToCancel_WaitsForTheLeadTimeWithControlledNow(t *testing.T) {
	_, _, subs, channel, d := newToCancelFixture(t)
	createToCancel(t, subs, "Trial", 10, 0, 3)
	now := time.Now()

	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, now))
	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, now.AddDate(0, 0, 6)))
	assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, now.AddDate(0, 0, 7)))
	assert.Equal(t, []int{3}, channel.days)
}

func TestToCancel_LateOptInFiresOnTheNextRun(t *testing.T) {
	_, _, subs, channel, d := newToCancelFixture(t)
	renewal := time.Now().AddDate(0, 0, 2)
	created, err := subs.Create(&models.Subscription{Name: "Late", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal})
	require.NoError(t, err)
	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Now()))

	stored := reload(t, subs, created.ID)
	stored.ToCancel = true
	stored.ToCancelReminderDays = 14
	_, err = subs.Update(stored.ID, stored)
	require.NoError(t, err)

	assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, time.Now()))
	assert.Equal(t, []int{2}, channel.days)
}

func TestToCancel_AnchorsOnCancelByAndNeverFallsBackToRenewal(t *testing.T) {
	t.Run("counts down to the cancel-by date", func(t *testing.T) {
		_, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Active", 20, 7, 14)
		assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, time.Now()))
		assert.Equal(t, []int{13}, channel.days)
		stored := reload(t, subs, sub.ID)
		assert.True(t, stored.LastToCancelReminderAnchor.Equal(*stored.CancelByDate()))
	})

	t.Run("stays quiet once the cancel-by date has passed", func(t *testing.T) {
		for _, status := range []string{"Active", "Trial"} {
			_, _, subs, channel, d := newToCancelFixture(t)
			sub := createToCancel(t, subs, status, 5, 10, 14)
			assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Now()), status)
			assert.Empty(t, channel.days)
			assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)
		}
	})

	t.Run("deadline day fires, the day after does not", func(t *testing.T) {
		_, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Trial", 10, 0, 3)
		deadline := *sub.ToCancelDeadline()
		y, m, day := deadline.Date()
		assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Date(y, m, day+1, 0, 0, 1, 0, deadline.Location())))
		assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, time.Date(y, m, day, 23, 59, 0, 0, deadline.Location())))
		assert.Equal(t, []int{0}, channel.days)
	})
}

func TestToCancel_FailureOrNoChannelLeavesItUnsentAndARetryRecovers(t *testing.T) {
	_, settings, subs, channel, d := newToCancelFixture(t)
	sub := createToCancel(t, subs, "Active", 2, 0, 3)
	now := time.Now()

	// Nothing configured.
	none := NewNotificationDispatcher(settings, &toCancelChannel{fakeChannel: fakeChannel{name: "off"}})
	assert.Equal(t, ReminderRunResult{Failed: 1}, none.runToCancelRemindersAt(subs, now))
	assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)

	// Every configured channel fails.
	channel.err = errors.New("provider down")
	assert.Equal(t, ReminderRunResult{Failed: 1}, d.runToCancelRemindersAt(subs, now))
	assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)
	assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderSent)

	// A channel without the capability is not a delivery either.
	unsupported := NewNotificationDispatcher(settings, &fakeChannel{name: "plain", configured: true})
	assert.Equal(t, ReminderRunResult{Failed: 1}, unsupported.runToCancelRemindersAt(subs, now))

	// The next run recovers.
	channel.err = nil
	assert.Equal(t, ReminderRunResult{Sent: 1}, d.runToCancelRemindersAt(subs, now.AddDate(0, 0, 1)))
	assert.Equal(t, []int{2, 1}, channel.days)
	assert.NotNil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)

	// One channel delivering is enough, and the failed one is not retried.
	second := createToCancel(t, subs, "Active", 2, 0, 3)
	broken := &toCancelChannel{fakeChannel: fakeChannel{name: "broken", configured: true, err: errors.New("x")}}
	mixed := NewNotificationDispatcher(settings, broken, channel)
	assert.Equal(t, ReminderRunResult{Sent: 1}, mixed.runToCancelRemindersAt(subs, now))
	assert.NotNil(t, reload(t, subs, second.ID).LastToCancelReminderAnchor)
	assert.Equal(t, ReminderRunResult{}, mixed.runToCancelRemindersAt(subs, now))
	assert.Len(t, broken.days, 1)
}

func TestToCancel_RealChannelFailureThenRecoveryThroughLocalSink(t *testing.T) {
	_, settings, subs, _, _ := newToCancelFixture(t)
	sub := createToCancel(t, subs, "Active", 2, 0, 3)
	sink := newPushSink(t, 503, 503, 503)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t"}))
	d := NewNotificationDispatcher(settings, fastNtfy(settings))

	assert.Equal(t, ReminderRunResult{Failed: 1}, d.RunToCancelReminders(subs))
	assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)
	assert.Len(t, sink.all(), 3)

	assert.Equal(t, ReminderRunResult{Sent: 1}, d.RunToCancelReminders(subs))
	assert.Equal(t, ReminderRunResult{}, d.RunToCancelReminders(subs))
	assert.Len(t, sink.all(), 4)
}

func TestToCancel_RenewalCycleReArms(t *testing.T) {
	t.Run("the deadline moving with the renewal date", func(t *testing.T) {
		db, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Active", 2, 0, 3)
		require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)

		// Written directly, as the AfterFind roll-forward does: the delivered state
		// stays in place and only the deadline differs.
		next := time.Now().AddDate(0, 0, 3)
		require.NoError(t, db.Model(&models.Subscription{}).Where("id = ?", sub.ID).UpdateColumn("renewal_date", next).Error)
		assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
		assert.Len(t, channel.days, 2)
	})

	t.Run("the real roll-forward of a past renewal date", func(t *testing.T) {
		db, _, subs, channel, d := newToCancelFixture(t)
		// Weekly from 8 days ago: the renewal 1 day ago rolls forward to 6 days ahead.
		start := time.Now().AddDate(0, 0, -8)
		past := time.Now().AddDate(0, 0, -1)
		created, err := subs.Create(&models.Subscription{Name: "Weekly", Cost: 1, Schedule: "Weekly", Status: "Active", StartDate: &start,
			RenewalDate: &past, ToCancel: true, ToCancelReminderDays: 14})
		require.NoError(t, err)
		require.NoError(t, db.Exec("UPDATE subscriptions SET last_to_cancel_reminder_sent = ?, last_to_cancel_reminder_anchor = ? WHERE id = ?", past, past, created.ID).Error)

		assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
		require.Len(t, channel.days, 1)
		assert.Equal(t, 6, channel.days[0])
		stored := reload(t, subs, created.ID)
		assert.True(t, stored.LastToCancelReminderAnchor.Equal(*stored.RenewalDate))
	})
}

func TestToCancel_RelevantEditsResetDedupe(t *testing.T) {
	edits := []struct {
		name    string
		edit    func(*models.Subscription)
		wantNil bool
	}{
		{"lead time", func(s *models.Subscription) { s.ToCancelReminderDays = 14 }, true},
		{"renewal date", func(s *models.Subscription) { d := s.RenewalDate.AddDate(0, 0, 1); s.RenewalDate = &d }, true},
		{"notice period", func(s *models.Subscription) { s.CancellationNoticeDays = 1 }, true},
		{"opting out", func(s *models.Subscription) { s.ToCancel = false }, true},
		{"status to Cancelled", func(s *models.Subscription) { s.Status = "Cancelled" }, true},
		{"an unrelated field", func(s *models.Subscription) { s.Notes = "edited" }, false},
		{"status to Trial", func(s *models.Subscription) { s.Status = "Trial" }, false},
	}
	for _, tt := range edits {
		t.Run(tt.name, func(t *testing.T) {
			_, _, subs, _, d := newToCancelFixture(t)
			sub := createToCancel(t, subs, "Active", 2, 0, 3)
			require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)

			stored := reload(t, subs, sub.ID)
			tt.edit(stored)
			updated, err := subs.Update(sub.ID, stored)
			require.NoError(t, err)
			if tt.wantNil {
				assert.Nil(t, updated.LastToCancelReminderAnchor)
				assert.Nil(t, updated.LastToCancelReminderSent)
			} else {
				assert.NotNil(t, updated.LastToCancelReminderAnchor)
				assert.NotNil(t, updated.LastToCancelReminderSent)
			}
		})
	}

	t.Run("an edit made after opting back in fires again", func(t *testing.T) {
		_, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Active", 2, 0, 3)
		require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
		for _, optedIn := range []bool{false, true} {
			stored := reload(t, subs, sub.ID)
			stored.ToCancel = optedIn
			_, err := subs.Update(sub.ID, stored)
			require.NoError(t, err)
		}
		assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
		assert.Len(t, channel.days, 2)
	})

	t.Run("a form loaded before the delivery cannot erase it", func(t *testing.T) {
		_, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Active", 2, 0, 3)
		loadedBeforeDelivery := reload(t, subs, sub.ID)
		require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)

		loadedBeforeDelivery.Notes = "saved late"
		_, err := subs.Update(sub.ID, loadedBeforeDelivery)
		require.NoError(t, err)
		assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Now()))
		assert.Len(t, channel.days, 1)
	})
}

func TestToCancel_CancelledClearsAndPausedHolds(t *testing.T) {
	_, _, subs, channel, d := newToCancelFixture(t)
	cancelled := createToCancel(t, subs, "Active", 2, 0, 3)
	cancelled.Status = "Cancelled"
	updated, err := subs.Update(cancelled.ID, cancelled)
	require.NoError(t, err)
	assert.False(t, updated.ToCancel)

	created := createToCancel(t, subs, "Cancelled", 2, 0, 3)
	assert.False(t, created.ToCancel, "a create cannot combine them either")

	paused := createToCancel(t, subs, "Paused", 2, 0, 3)
	assert.True(t, paused.ToCancel)
	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Now()))
	assert.Empty(t, channel.days)

	paused.Status = "Active"
	_, err = subs.Update(paused.ID, paused)
	require.NoError(t, err)
	assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent, "resuming picks the intent back up")
}

func TestToCancel_SuppressesRenewalMessagesOnlyWhileOptedIn(t *testing.T) {
	_, settings, subs, channel, d := newToCancelFixture(t)
	require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
	require.NoError(t, settings.SetIntSetting("reminder_days", 7))
	renewal := time.Now().AddDate(0, 0, 3)
	created, err := subs.Create(&models.Subscription{Name: "Both", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal,
		ReminderEnabled: true, ToCancel: true})
	require.NoError(t, err)

	assert.Equal(t, ReminderRunResult{}, d.RunRenewalReminders(subs))
	assert.Empty(t, channel.calls, "no renewal message while marked to cancel")

	assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
	assert.Nil(t, reload(t, subs, created.ID).LastReminderSent, "the renewal state is its own")

	stored := reload(t, subs, created.ID)
	stored.ToCancel = false
	_, err = subs.Update(created.ID, stored)
	require.NoError(t, err)
	assert.Equal(t, ReminderRunResult{Sent: 1}, d.RunRenewalReminders(subs))
	assert.Equal(t, []string{"renewal"}, channel.calls, "renewal reminders resume after opting out")
}

// Each case edits the subscription while a pass is mid-send, as a user can while
// providers respond slowly, and checks that the edit survives and whether the
// delivery may still be recorded.
func TestReminderRunners_DoNotOverwriteEditsMadeWhileSending(t *testing.T) {
	edit := func(t *testing.T, subs *SubscriptionService, id uint, change func(*models.Subscription)) {
		t.Helper()
		stored := reload(t, subs, id)
		change(stored)
		_, err := subs.Update(id, stored)
		require.NoError(t, err)
	}
	notes := func(s *models.Subscription) { s.Notes = "edited mid-send" }
	moveRenewal := func(s *models.Subscription) { d := s.RenewalDate.AddDate(0, 0, 9); s.RenewalDate = &d }

	t.Run("to-cancel: unrelated edit is kept and the delivery recorded", func(t *testing.T) {
		_, _, subs, channel, d := newToCancelFixture(t)
		sub := createToCancel(t, subs, "Active", 2, 0, 3)
		channel.onSend = func() { edit(t, subs, sub.ID, notes) }
		assert.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
		stored := reload(t, subs, sub.ID)
		assert.Equal(t, "edited mid-send", stored.Notes)
		assert.NotNil(t, stored.LastToCancelReminderAnchor)
	})

	for name, change := range map[string]func(*models.Subscription){
		"deadline moved": moveRenewal,
		"opted out":      func(s *models.Subscription) { s.ToCancel = false },
		"lead time":      func(s *models.Subscription) { s.ToCancelReminderDays = 14 },
		"notice period":  func(s *models.Subscription) { s.CancellationNoticeDays = 1 },
		"cancelled":      func(s *models.Subscription) { s.Status = "Cancelled" },
	} {
		t.Run("to-cancel: stale send is not recorded after "+name, func(t *testing.T) {
			_, _, subs, channel, d := newToCancelFixture(t)
			sub := createToCancel(t, subs, "Active", 2, 0, 3)
			channel.onSend = func() { edit(t, subs, sub.ID, change) }
			d.runToCancelRemindersAt(subs, time.Now())
			assert.Nil(t, reload(t, subs, sub.ID).LastToCancelReminderAnchor)
		})
	}

	renewalSub := func(t *testing.T, subs *SubscriptionService) uint {
		renewal := time.Now().AddDate(0, 0, 3)
		created, err := subs.Create(&models.Subscription{Name: "R", Cost: 1, Schedule: "Monthly", Status: "Active", RenewalDate: &renewal, ReminderEnabled: true})
		require.NoError(t, err)
		return created.ID
	}
	renewalFixture := func(t *testing.T, change func(*models.Subscription)) (*SubscriptionService, uint) {
		settings, subs := newReminderRunnerFixture(t)
		require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
		require.NoError(t, settings.SetIntSetting("reminder_days", 7))
		id := renewalSub(t, subs)
		channel := &editingChannel{fakeChannel: fakeChannel{name: "e", configured: true}, onSend: func() { edit(t, subs, id, change) }}
		assert.Equal(t, 1, NewNotificationDispatcher(settings, channel).RunRenewalReminders(subs).Sent)
		return subs, id
	}

	t.Run("renewal: unrelated edit is kept and the delivery recorded", func(t *testing.T) {
		subs, id := renewalFixture(t, notes)
		stored := reload(t, subs, id)
		assert.Equal(t, "edited mid-send", stored.Notes)
		assert.NotNil(t, stored.LastReminderSent)
		assert.True(t, stored.LastReminderRenewalDate.Equal(*stored.RenewalDate))
		assert.NotEqual(t, -1, stored.LastReminderWindow)
	})

	t.Run("renewal: stale send is not recorded after the date moved", func(t *testing.T) {
		subs, id := renewalFixture(t, moveRenewal)
		assert.Nil(t, reload(t, subs, id).LastReminderSent)
	})

	t.Run("renewal: stale send is not recorded after marking to cancel", func(t *testing.T) {
		subs, id := renewalFixture(t, func(s *models.Subscription) { s.ToCancel = true })
		assert.Nil(t, reload(t, subs, id).LastReminderSent)
	})

	cancellationFixture := func(t *testing.T, change func(*models.Subscription)) (*SubscriptionService, uint) {
		settings, subs := newReminderRunnerFixture(t)
		require.NoError(t, settings.SetBoolSetting("cancellation_reminders", true))
		require.NoError(t, settings.SetIntSetting("cancellation_reminder_days", 7))
		id := createDueCancellation(t, subs)
		channel := &editingChannel{fakeChannel: fakeChannel{name: "e", configured: true}, onSend: func() { edit(t, subs, id, change) }}
		assert.Equal(t, 1, NewNotificationDispatcher(settings, channel).RunCancellationReminders(subs).Sent)
		return subs, id
	}

	t.Run("cancellation: unrelated edit is kept and the delivery recorded", func(t *testing.T) {
		subs, id := cancellationFixture(t, notes)
		stored := reload(t, subs, id)
		assert.Equal(t, "edited mid-send", stored.Notes)
		assert.NotNil(t, stored.LastCancellationReminderSent)
	})

	t.Run("cancellation: stale send is not recorded after the date moved", func(t *testing.T) {
		subs, id := cancellationFixture(t, func(s *models.Subscription) { d := s.CancellationDate.AddDate(0, 0, 3); s.CancellationDate = &d })
		assert.Nil(t, reload(t, subs, id).LastCancellationReminderSent)
	})

	t.Run("cancellation: stale send is not recorded after reactivating", func(t *testing.T) {
		subs, id := cancellationFixture(t, func(s *models.Subscription) { s.Status = "Active" })
		assert.Nil(t, reload(t, subs, id).LastCancellationReminderSent)
	})
}

// editingChannel runs onSend during a renewal or cancellation reminder.
type editingChannel struct {
	fakeChannel
	onSend func()
}

func (e *editingChannel) SendRenewalReminder(*models.Subscription, int) error {
	e.onSend()
	return e.err
}

func (e *editingChannel) SendCancellationReminder(*models.Subscription, int) error {
	e.onSend()
	return e.err
}

// A date stored in a different text format must still match, or the delivery
// would never be recorded and the reminder would repeat every day.
func TestMarkToCancelReminderSent_MatchesDatesAcrossStorageFormats(t *testing.T) {
	db, _, subs, channel, d := newToCancelFixture(t)
	sub := createToCancel(t, subs, "Active", 2, 0, 3)
	formatted := sub.RenewalDate.UTC().Format("2006-01-02T15:04:05.999999999Z")
	require.NoError(t, db.Exec("UPDATE subscriptions SET renewal_date = ? WHERE id = ?", formatted, sub.ID).Error)

	require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)
	assert.Equal(t, ReminderRunResult{}, d.runToCancelRemindersAt(subs, time.Now()))
	assert.Len(t, channel.days, 1)
}

func TestMarkRenewalAndCancellation_MatchDatesAcrossStorageFormats(t *testing.T) {
	const isoFormat = "2006-01-02T15:04:05.999999999Z"

	t.Run("renewal", func(t *testing.T) {
		db, settings, subs, channel, d := newToCancelFixture(t)
		require.NoError(t, settings.SetBoolSetting("renewal_reminders", true))
		require.NoError(t, settings.SetIntSetting("reminder_days", 7))
		id := createDueRenewal(t, subs)
		stored := reload(t, subs, id)
		require.NoError(t, db.Exec("UPDATE subscriptions SET renewal_date = ? WHERE id = ?", stored.RenewalDate.UTC().Format(isoFormat), id).Error)

		assert.Equal(t, ReminderRunResult{Sent: 1}, d.RunRenewalReminders(subs))
		assert.Equal(t, ReminderRunResult{}, d.RunRenewalReminders(subs))
		assert.Equal(t, []string{"renewal"}, channel.calls)
	})

	t.Run("cancellation", func(t *testing.T) {
		db, settings, subs, channel, d := newToCancelFixture(t)
		require.NoError(t, settings.SetBoolSetting("cancellation_reminders", true))
		require.NoError(t, settings.SetIntSetting("cancellation_reminder_days", 7))
		id := createDueCancellation(t, subs)
		stored := reload(t, subs, id)
		require.NoError(t, db.Exec("UPDATE subscriptions SET cancellation_date = ? WHERE id = ?", stored.CancellationDate.UTC().Format(isoFormat), id).Error)

		assert.Equal(t, ReminderRunResult{Sent: 1}, d.RunCancellationReminders(subs))
		assert.Equal(t, ReminderRunResult{}, d.RunCancellationReminders(subs))
		assert.Equal(t, []string{"cancellation"}, channel.calls)
	})
}

func TestDuplicate_KeepsIntentButNotDeliveredState(t *testing.T) {
	_, _, subs, _, d := newToCancelFixture(t)
	sub := createToCancel(t, subs, "Active", 2, 0, 14)
	require.Equal(t, 1, d.runToCancelRemindersAt(subs, time.Now()).Sent)

	dup, _, err := subs.Duplicate(sub.ID)
	require.NoError(t, err)
	assert.True(t, dup.ToCancel)
	assert.Equal(t, 14, dup.ToCancelReminderDays)
	assert.Nil(t, dup.LastToCancelReminderAnchor)
	assert.Nil(t, dup.LastToCancelReminderSent)
}

// formSink stands in for Pushover and Telegram, recording form-encoded posts.
type formSink struct {
	*httptest.Server
	mu    sync.Mutex
	forms []url.Values
}

func newFormSink(t *testing.T, response string) *formSink {
	t.Helper()
	sink := &formSink{}
	sink.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(raw))
		sink.mu.Lock()
		sink.forms = append(sink.forms, form)
		sink.mu.Unlock()
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(sink.Close)
	return sink
}

func (s *formSink) all() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]url.Values(nil), s.forms...)
}

func toCancelMessageSubscription() *models.Subscription {
	renewal := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	return &models.Subscription{
		ID: 7, Name: "Stream <b>", Cost: 12.5, OriginalCurrency: "EUR", Schedule: "Monthly", Status: "Active",
		RenewalDate: &renewal, CancellationNoticeDays: 7, ToCancel: true, ToCancelReminderDays: 14,
		URL: "https://me:hunter2@example.com/account?token=s3cret#frag", Notes: "private-note", Account: "private-account",
		PaymentMethod: "private-card", Category: models.Category{Name: "Video"},
	}
}

// TestToCancel_AllSixChannels sends through the real channel implementations
// with every global toggle off. Five talk to local sinks. Email requires TLS, so
// its body is checked through the renderer and its delivery attempt through a
// closed local port.
func TestToCancel_AllSixChannels(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetCurrency("USD"))
	require.NoError(t, settings.SetDateFormat("DD/MM/YYYY"))
	for _, key := range []string{"renewal_reminders", "cancellation_reminders", "high_cost_alerts"} {
		require.NoError(t, settings.SetBoolSetting(key, false))
	}

	ntfySink, gotifySink, webhookSink := newPushSink(t), newPushSink(t), newPushSink(t)
	pushoverSink := newFormSink(t, `{"status":1}`)
	telegramSink := newFormSink(t, `{"ok":true}`)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: ntfySink.URL, Topic: "t"}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: gotifySink.URL, AppToken: "tok"}))
	require.NoError(t, settings.SaveWebhookConfig(&models.WebhookConfig{URL: webhookSink.URL + "/hook"}))
	require.NoError(t, settings.SavePushoverConfig(&models.PushoverConfig{UserKey: "u", AppToken: "a"}))
	require.NoError(t, settings.SaveTelegramConfig(&models.TelegramConfig{BotToken: "bot-secret", ChatID: "1"}))
	require.NoError(t, settings.SaveSMTPConfig(&models.SMTPConfig{Host: "127.0.0.1", Port: 1, From: "a@example.com", To: "b@example.com"}))

	pushover := NewPushoverService(settings)
	pushover.apiURL = pushoverSink.URL
	telegram := NewTelegramService(settings)
	telegram.apiBaseURL = telegramSink.URL
	email := NewEmailService(settings)
	d := NewNotificationDispatcher(settings, email, pushover, NewWebhookService(settings), telegram, fastNtfy(settings), fastGotify(settings))

	sub := toCancelMessageSubscription()
	report := d.SendToCancelReminder(sub, 2)
	assert.False(t, report.Disabled)
	assert.Equal(t, []string{"pushover", "webhook", "telegram", "ntfy", "gotify"}, report.Delivered)
	require.Len(t, report.Failed, 1, "email was attempted and could not connect")
	assert.Equal(t, "email", report.Failed[0].Channel)

	const lead = "You planned to cancel Stream <b>. Cancel by 13 November 2026 (2 days left) to stop it renewing on 20 November 2026."
	plain := func(message string) {
		t.Helper()
		assert.Contains(t, message, lead)
		assert.Contains(t, message, "Cancel By: 13 November 2026")
		assert.Contains(t, message, "Renewal Date: 20 November 2026")
		assert.Contains(t, message, "€12.50 Monthly")
		assert.Contains(t, message, "URL: https://example.com/account")
		assertNoSecrets(t, message)
	}
	for _, sink := range []*pushSink{ntfySink, gotifySink} {
		reqs := sink.all()
		require.Len(t, reqs, 1)
		assert.Equal(t, "Reminder to Cancel: Stream <b>", reqs[0].Body["title"])
		plain(reqs[0].Body["message"].(string))
	}
	require.Len(t, pushoverSink.all(), 1)
	assert.Equal(t, "Reminder to Cancel: Stream <b>", pushoverSink.all()[0].Get("title"))
	plain(pushoverSink.all()[0].Get("message"))
	require.Len(t, telegramSink.all(), 1)
	plain(telegramSink.all()[0].Get("text"))

	hooks := webhookSink.all()
	require.Len(t, hooks, 1)
	assert.Equal(t, "to_cancel_reminder", hooks[0].Body["event"])
	assert.Equal(t, lead, hooks[0].Body["message"])
	details := hooks[0].Body["subscription"].(map[string]any)
	assert.Equal(t, true, details["to_cancel"])
	assert.Equal(t, float64(14), details["to_cancel_reminder_days"])
	assert.Equal(t, "13/11/2026", details["cancel_by_date"])
	assert.Equal(t, "20/11/2026", details["renewal_date"])
	assert.Equal(t, "https://example.com/account", details["url"])
	raw, err := json.Marshal(hooks[0].Body)
	require.NoError(t, err)
	assertNoSecrets(t, string(raw))

	subject, body, err := email.renderToCancelReminder(sub, 2)
	require.NoError(t, err)
	assert.Equal(t, "Reminder to Cancel: Stream <b>", subject)
	assert.Contains(t, body, "Stream &lt;b&gt;", "names are escaped in HTML")
	assert.NotContains(t, body, "Stream <b>")
	assert.Contains(t, body, "13 November 2026")
	assert.Contains(t, body, "20 November 2026")
	assertNoSecrets(t, body)
}

func assertNoSecrets(t *testing.T, text string) {
	t.Helper()
	for _, secret := range []string{"hunter2", "s3cret", "frag", "private-note", "private-account", "private-card", "bot-secret"} {
		assert.NotContains(t, text, secret)
	}
}

func TestToCancelReminderContent_Wording(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetDateFormat("DD/MM/YYYY"))
	sub := toCancelMessageSubscription()
	sub.Name = "S"

	assert.Contains(t, buildToCancelReminderContent(sub, 1, settings).Lead, "(1 day left)")
	assert.Contains(t, buildToCancelReminderContent(sub, 0, settings).Lead, "(last day)")

	sub.CancellationNoticeDays = 0
	content := buildToCancelReminderContent(sub, 3, settings)
	assert.Equal(t, "You planned to cancel S. Cancel before it renews on 20 November 2026 (3 days left).", content.Lead)
	assert.Empty(t, content.CancelBy)
	message := toCancelReminderMessage(sub, content, settings)
	assert.NotContains(t, message, "Cancel By:")
	assert.Contains(t, message, "Renewal Date: 20 November 2026")
}

func TestNotificationSafeURL(t *testing.T) {
	for raw, want := range map[string]string{
		"":                               "",
		"https://example.com/a":          "https://example.com/a",
		"http://u:p@example.com/a?k=v#f": "http://example.com/a",
		"example.com/account?token=1":    "https://example.com/account",
		"javascript:alert(1)":            "",
		"mailto:a@example.com":           "",
		"https://exa mple.com":           "",
	} {
		assert.Equal(t, want, notificationSafeURL(raw), raw)
	}
}

func TestWebhookFailure_DoesNotExposeTheWebhookURL(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SaveWebhookConfig(&models.WebhookConfig{URL: "http://127.0.0.1:1/hook/very-secret-path"}))
	err := NewWebhookService(settings).SendToCancelReminder(toCancelMessageSubscription(), 2)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "very-secret-path")
}

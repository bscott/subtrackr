package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestToCancelReminderDue(t *testing.T) {
	now := time.Date(2026, 11, 10, 15, 0, 0, 0, time.UTC)
	day := func(offset int) *time.Time {
		d := time.Date(2026, 11, 10+offset, 0, 0, 0, 0, time.UTC)
		return &d
	}
	tests := []struct {
		name     string
		sub      Subscription
		wantDays int
		wantDue  bool
	}{
		{"renewal is the deadline without a notice period", Subscription{Status: "Active", RenewalDate: day(3)}, 3, true},
		{"outside the lead time", Subscription{Status: "Active", RenewalDate: day(4)}, 4, false},
		{"wider lead time", Subscription{Status: "Active", RenewalDate: day(14), ToCancelReminderDays: 14}, 14, true},
		{"trial is eligible", Subscription{Status: "Trial", RenewalDate: day(1)}, 1, true},
		{"cancel-by is the deadline with a notice period", Subscription{Status: "Active", RenewalDate: day(12), CancellationNoticeDays: 10}, 2, true},
		{"notice period makes it due earlier than the renewal", Subscription{Status: "Active", RenewalDate: day(13), CancellationNoticeDays: 10}, 3, true},
		{"deadline day still counts after midnight UTC", Subscription{Status: "Active", RenewalDate: day(10), CancellationNoticeDays: 10}, 0, true},
		{"missed cancel-by does not fall back to the renewal", Subscription{Status: "Active", RenewalDate: day(2), CancellationNoticeDays: 3}, -1, false},
		{"missed trial end", Subscription{Status: "Trial", RenewalDate: day(-1)}, -1, false},
		{"paused keeps the intent but is not eligible", Subscription{Status: "Paused", RenewalDate: day(1)}, 0, false},
		{"no deadline", Subscription{Status: "Trial"}, 0, false},
		{"already sent for this deadline", Subscription{Status: "Active", RenewalDate: day(2), LastToCancelReminderAnchor: day(2)}, 2, false},
		{"sent for an earlier deadline re-arms", Subscription{Status: "Active", RenewalDate: day(2), LastToCancelReminderAnchor: day(-28)}, 2, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.sub.ToCancel = true
			tt.sub.NormalizeToCancel()
			days, due := tt.sub.ToCancelReminderDue(now)
			assert.Equal(t, tt.wantDays, days)
			assert.Equal(t, tt.wantDue, due)
		})
	}

	t.Run("not opted in", func(t *testing.T) {
		_, due := Subscription{Status: "Active", RenewalDate: day(1)}.ToCancelReminderDue(now)
		assert.False(t, due)
	})
}

func TestCalendarDaysUntil_UsesEachDatesOwnCalendarDay(t *testing.T) {
	pacific := time.FixedZone("PST", -8*60*60)
	deadline := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	// 10 Nov 20:00 PST is already 11 Nov in UTC, but the user's deadline is tomorrow.
	assert.Equal(t, 1, calendarDaysUntil(deadline, time.Date(2026, 11, 10, 20, 0, 0, 0, pacific)))
	assert.Equal(t, 0, calendarDaysUntil(deadline, time.Date(2026, 11, 11, 23, 59, 0, 0, pacific)))
	assert.Equal(t, -1, calendarDaysUntil(deadline, time.Date(2026, 11, 12, 0, 1, 0, 0, pacific)))
	// A 23-hour day across a daylight-saving change still counts as one day.
	la, err := time.LoadLocation("America/Los_Angeles")
	if err == nil {
		assert.Equal(t, 1, calendarDaysUntil(time.Date(2026, 3, 9, 0, 0, 0, 0, la), time.Date(2026, 3, 8, 0, 0, 0, 0, la)))
	}
}

func TestNormalizeToCancel(t *testing.T) {
	stamp := time.Now()
	for _, invalid := range []int{-1, 0, 4, 7, 15} {
		s := Subscription{ToCancelReminderDays: invalid}
		s.NormalizeToCancel()
		assert.Equal(t, DefaultToCancelReminderDays, s.ToCancelReminderDays)
	}
	for _, valid := range []int{3, 5, 14} {
		s := Subscription{ToCancelReminderDays: valid}
		s.NormalizeToCancel()
		assert.Equal(t, valid, s.ToCancelReminderDays)
	}

	cancelled := Subscription{Status: "Cancelled", ToCancel: true, LastToCancelReminderSent: &stamp, LastToCancelReminderAnchor: &stamp}
	cancelled.NormalizeToCancel()
	assert.False(t, cancelled.ToCancel, "Cancelled clears the intent")
	assert.Nil(t, cancelled.LastToCancelReminderSent)
	assert.Nil(t, cancelled.LastToCancelReminderAnchor)

	optedOut := Subscription{Status: "Active", LastToCancelReminderSent: &stamp, LastToCancelReminderAnchor: &stamp}
	optedOut.NormalizeToCancel()
	assert.Nil(t, optedOut.LastToCancelReminderAnchor, "opting in again fires again")

	paused := Subscription{Status: "Paused", ToCancel: true, LastToCancelReminderAnchor: &stamp}
	paused.NormalizeToCancel()
	assert.True(t, paused.ToCancel, "Paused keeps the intent")
	assert.NotNil(t, paused.LastToCancelReminderAnchor)
}

func TestToCancelDeadlineMissed(t *testing.T) {
	soon := time.Now().AddDate(0, 0, 2)
	assert.True(t, Subscription{Status: "Active", ToCancel: true, RenewalDate: &soon, CancellationNoticeDays: 5}.ToCancelDeadlineMissed())
	assert.False(t, Subscription{Status: "Active", ToCancel: true, RenewalDate: &soon}.ToCancelDeadlineMissed())
	assert.False(t, Subscription{Status: "Active", RenewalDate: &soon, CancellationNoticeDays: 5}.ToCancelDeadlineMissed(), "not opted in")
}

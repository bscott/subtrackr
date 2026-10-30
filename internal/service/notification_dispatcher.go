package service

import (
	"fmt"
	"strings"
	"subtrackr/internal/models"
)

// NotificationChannel is a destination for subscription notifications. Each
// channel keeps its own message formatting; the dispatcher decides which
// channels to try and how to judge the outcome.
//
// New notification types can be added as separate optional interfaces checked
// with a type assertion, so channels that do not support them are not forced to
// implement stubs. ToCancelReminderChannel is one.
type NotificationChannel interface {
	Name() string
	// IsConfigured reports whether the channel has the settings it needs to
	// send. Unconfigured channels are skipped and never count as delivered.
	IsConfigured() bool
	SendHighCostAlert(subscription *models.Subscription) error
	SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error
	SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error
}

// ToCancelReminderChannel is a channel that can remind the user to cancel a
// subscription. Implementations must not check the renewal reminder toggle: the
// per-subscription flag is the opt-in, and returning nil without sending would be
// counted as a delivery.
type ToCancelReminderChannel interface {
	SendToCancelReminder(subscription *models.Subscription, daysLeft int) error
}

var (
	_ ToCancelReminderChannel = (*EmailService)(nil)
	_ ToCancelReminderChannel = (*PushoverService)(nil)
	_ ToCancelReminderChannel = (*WebhookService)(nil)
	_ ToCancelReminderChannel = (*TelegramService)(nil)
	_ ToCancelReminderChannel = (*NtfyService)(nil)
	_ ToCancelReminderChannel = (*GotifyService)(nil)
)

// ChannelFailure records a channel that was attempted and failed.
type ChannelFailure struct {
	Channel string
	Err     error
}

// DeliveryReport summarizes one notification fanned out to every channel.
type DeliveryReport struct {
	// Disabled is true when the notification category is switched off, in
	// which case no channel was attempted.
	Disabled  bool
	Delivered []string
	Failed    []ChannelFailure
}

// AnyDelivered reports whether at least one configured channel accepted the
// notification. A notification that was disabled, had no configured channel,
// or failed everywhere was not delivered.
func (r DeliveryReport) AnyDelivered() bool { return len(r.Delivered) > 0 }

// Attempted is the number of configured channels that were tried.
func (r DeliveryReport) Attempted() int { return len(r.Delivered) + len(r.Failed) }

// FailureSummary formats the failed channels as "name=error, ...".
func (r DeliveryReport) FailureSummary() string {
	parts := make([]string, 0, len(r.Failed))
	for _, f := range r.Failed {
		parts = append(parts, fmt.Sprintf("%s=%v", f.Channel, f.Err))
	}
	return strings.Join(parts, ", ")
}

// NotificationDispatcher sends each notification to every configured channel.
type NotificationDispatcher struct {
	settingsService *SettingsService
	channels        []NotificationChannel
}

// NewNotificationDispatcher builds a dispatcher over the given channels in order.
func NewNotificationDispatcher(settingsService *SettingsService, channels ...NotificationChannel) *NotificationDispatcher {
	return &NotificationDispatcher{settingsService: settingsService, channels: channels}
}

// SendHighCostAlert notifies every configured channel about a high-cost subscription.
func (d *NotificationDispatcher) SendHighCostAlert(sub *models.Subscription) DeliveryReport {
	return d.dispatch("high_cost_alerts", true, func(ch NotificationChannel) error {
		return ch.SendHighCostAlert(sub)
	})
}

// SendRenewalReminder notifies every configured channel about an upcoming renewal.
func (d *NotificationDispatcher) SendRenewalReminder(sub *models.Subscription, daysUntil int) DeliveryReport {
	return d.dispatch("renewal_reminders", false, func(ch NotificationChannel) error {
		return ch.SendRenewalReminder(sub, daysUntil)
	})
}

// SendCancellationReminder notifies every configured channel about an upcoming cancellation.
func (d *NotificationDispatcher) SendCancellationReminder(sub *models.Subscription, daysUntil int) DeliveryReport {
	return d.dispatch("cancellation_reminders", false, func(ch NotificationChannel) error {
		return ch.SendCancellationReminder(sub, daysUntil)
	})
}

// SendToCancelReminder notifies every configured channel that supports it about
// a subscription the user intends to cancel. No category toggle applies.
func (d *NotificationDispatcher) SendToCancelReminder(sub *models.Subscription, daysLeft int) DeliveryReport {
	var report DeliveryReport
	for _, ch := range d.channels {
		toCancel, supported := ch.(ToCancelReminderChannel)
		if !supported || !ch.IsConfigured() {
			continue
		}
		if err := toCancel.SendToCancelReminder(sub, daysLeft); err != nil {
			report.Failed = append(report.Failed, ChannelFailure{Channel: ch.Name(), Err: err})
			continue
		}
		report.Delivered = append(report.Delivered, ch.Name())
	}
	return report
}

func (d *NotificationDispatcher) dispatch(toggleKey string, toggleDefault bool, send func(NotificationChannel) error) DeliveryReport {
	var report DeliveryReport
	// Same check every channel makes before sending: if the dispatcher treated
	// the category as enabled while channels skipped it, their nil returns
	// would be miscounted as deliveries.
	if enabled, err := d.settingsService.GetBoolSetting(toggleKey, toggleDefault); err != nil || !enabled {
		report.Disabled = true
		return report
	}
	for _, ch := range d.channels {
		if !ch.IsConfigured() {
			continue
		}
		if err := send(ch); err != nil {
			report.Failed = append(report.Failed, ChannelFailure{Channel: ch.Name(), Err: err})
			continue
		}
		report.Delivered = append(report.Delivered, ch.Name())
	}
	return report
}

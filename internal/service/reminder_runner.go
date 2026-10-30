package service

import (
	"log"
	"time"
)

// ReminderRunResult counts the outcome of one scheduled reminder pass.
type ReminderRunResult struct {
	Sent   int
	Failed int
}

// RunRenewalReminders sends due renewal reminders through every configured
// channel. A reminder is marked sent only when at least one configured channel
// delivered it, so it is retried on the next run if every channel failed or
// none is configured.
func (d *NotificationDispatcher) RunRenewalReminders(subscriptionService *SubscriptionService) ReminderRunResult {
	var result ReminderRunResult
	if !d.settingsService.GetBoolSettingWithDefault("renewal_reminders", false) {
		return result
	}

	// Resolve reminder windows: multi-value CSV takes precedence; fall back to single-int setting.
	windowsCSV := d.settingsService.GetStringSettingWithDefault("reminder_days_list", "")
	fallback := d.settingsService.GetIntSettingWithDefault("reminder_days", 7)
	windows := ParseReminderWindows(windowsCSV, fallback)
	if len(windows) == 0 {
		return result
	}

	subscriptions, err := subscriptionService.GetSubscriptionsNeedingReminders(windows)
	if err != nil {
		log.Printf("Error getting subscriptions for renewal reminders: %v", err)
		return result
	}
	if len(subscriptions) == 0 {
		log.Printf("No subscriptions need renewal reminders today")
		return result
	}

	log.Printf("Checking %d subscription(s) for renewal reminders", len(subscriptions))
	for sub, daysUntil := range subscriptions {
		report := d.SendRenewalReminder(sub, daysUntil)
		if !report.AnyDelivered() {
			logUndelivered("renewal", sub.Name, sub.ID, report)
			result.Failed++
			continue
		}

		// Remember the renewal date and the window we just fired. Renewal-date changes
		// are handled by the sameRenewal check in GetSubscriptionsNeedingReminders.
		marked, markErr := subscriptionService.MarkRenewalReminderSent(sub, daysUntil, time.Now())
		logUnrecorded("renewal", sub.Name, sub.ID, marked, markErr)

		if len(report.Failed) > 0 {
			log.Printf("Sent renewal reminder for subscription %s (renews in %d days) - some channels failed: %s", sub.Name, daysUntil, report.FailureSummary())
		} else {
			log.Printf("Sent renewal reminders for subscription %s (renews in %d days)", sub.Name, daysUntil)
		}
		result.Sent++
	}

	log.Printf("Renewal reminder check complete: %d sent, %d failed", result.Sent, result.Failed)
	return result
}

// RunCancellationReminders sends due cancellation reminders through every
// configured channel, marking each sent only when at least one channel
// delivered it.
func (d *NotificationDispatcher) RunCancellationReminders(subscriptionService *SubscriptionService) ReminderRunResult {
	var result ReminderRunResult
	if !d.settingsService.GetBoolSettingWithDefault("cancellation_reminders", false) {
		return result
	}

	// Resolve cancellation reminder windows: multi-value CSV takes precedence; fall back to single-int setting.
	windowsCSV := d.settingsService.GetStringSettingWithDefault("cancellation_reminder_days_list", "")
	fallback := d.settingsService.GetIntSettingWithDefault("cancellation_reminder_days", 7)
	windows := ParseReminderWindows(windowsCSV, fallback)
	if len(windows) == 0 {
		return result
	}

	subscriptions, err := subscriptionService.GetSubscriptionsNeedingCancellationReminders(windows)
	if err != nil {
		log.Printf("Error getting subscriptions for cancellation reminders: %v", err)
		return result
	}
	if len(subscriptions) == 0 {
		log.Printf("No subscriptions need cancellation reminders today")
		return result
	}

	log.Printf("Checking %d subscription(s) for cancellation reminders", len(subscriptions))
	for sub, daysUntil := range subscriptions {
		report := d.SendCancellationReminder(sub, daysUntil)
		if !report.AnyDelivered() {
			logUndelivered("cancellation", sub.Name, sub.ID, report)
			result.Failed++
			continue
		}

		// Remember the cancellation date and the window we just fired. Cancellation-date
		// changes are handled by the sameDate check in GetSubscriptionsNeedingCancellationReminders.
		marked, markErr := subscriptionService.MarkCancellationReminderSent(sub, daysUntil, time.Now())
		logUnrecorded("cancellation", sub.Name, sub.ID, marked, markErr)

		if len(report.Failed) > 0 {
			log.Printf("Sent cancellation reminder for subscription %s (ends in %d days) - some channels failed: %s", sub.Name, daysUntil, report.FailureSummary())
		} else {
			log.Printf("Sent cancellation reminders for subscription %s (ends in %d days)", sub.Name, daysUntil)
		}
		result.Sent++
	}

	log.Printf("Cancellation reminder check complete: %d sent, %d failed", result.Sent, result.Failed)
	return result
}

// RunToCancelReminders sends due to-cancel reminders through every configured
// channel. The per-subscription flag is the opt-in, so no global setting applies.
func (d *NotificationDispatcher) RunToCancelReminders(subscriptionService *SubscriptionService) ReminderRunResult {
	return d.runToCancelRemindersAt(subscriptionService, time.Now())
}

func (d *NotificationDispatcher) runToCancelRemindersAt(subscriptionService *SubscriptionService, now time.Time) ReminderRunResult {
	var result ReminderRunResult
	reminders, err := subscriptionService.GetSubscriptionsNeedingToCancelReminders(now)
	if err != nil {
		log.Printf("Error getting subscriptions for to-cancel reminders: %v", err)
		return result
	}
	if len(reminders) == 0 {
		log.Printf("No subscriptions need to-cancel reminders today")
		return result
	}

	log.Printf("Checking %d subscription(s) for to-cancel reminders", len(reminders))
	for _, reminder := range reminders {
		sub := reminder.Subscription
		report := d.SendToCancelReminder(sub, reminder.DaysLeft)
		if !report.AnyDelivered() {
			logUndelivered("to-cancel", sub.Name, sub.ID, report)
			result.Failed++
			continue
		}

		marked, markErr := subscriptionService.MarkToCancelReminderSent(sub, reminder.Deadline, now)
		logUnrecorded("to-cancel", sub.Name, sub.ID, marked, markErr)

		if len(report.Failed) > 0 {
			log.Printf("Sent to-cancel reminder for subscription %s (%d days left) - some channels failed: %s", sub.Name, reminder.DaysLeft, report.FailureSummary())
		} else {
			log.Printf("Sent to-cancel reminders for subscription %s (%d days left)", sub.Name, reminder.DaysLeft)
		}
		result.Sent++
	}

	log.Printf("To-cancel reminder check complete: %d sent, %d failed", result.Sent, result.Failed)
	return result
}

// logUnrecorded reports a delivered reminder whose state was not recorded.
func logUnrecorded(kind, name string, id uint, marked bool, err error) {
	if err != nil {
		log.Printf("Warning: Failed to record %s reminder for subscription %s (ID: %d): %v", kind, name, id, err)
	} else if !marked {
		log.Printf("Not recording %s reminder for subscription %s (ID: %d): it was edited while the reminder was being sent", kind, name, id)
	}
}

func logUndelivered(kind, name string, id uint, report DeliveryReport) {
	if report.Attempted() == 0 {
		log.Printf("Not sending %s reminder for subscription %s (ID: %d): no notification channel is configured", kind, name, id)
		return
	}
	log.Printf("Error sending %s reminder for subscription %s (ID: %d): %s", kind, name, id, report.FailureSummary())
}

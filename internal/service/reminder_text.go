package service

import (
	"fmt"
	"subtrackr/internal/models"
	"time"
)

// renewalReminderContent is the channel-independent content of a renewal
// reminder. While a cancellation deadline is still upcoming the wording (and
// the emoji header used by plain-text channels) switches to cancel-by
// messaging; CancelBy is non-nil exactly in that case so every channel gates
// its variants on the same condition.
type renewalReminderContent struct {
	Title    string
	Header   string
	Lead     string
	CancelBy *time.Time
}

func buildRenewalReminderContent(sub *models.Subscription, daysUntil int, settings *SettingsService) renewalReminderContent {
	daysText := "days"
	if daysUntil == 1 {
		daysText = "day"
	}
	if cancelBy := sub.UpcomingCancelByDate(); cancelBy != nil {
		return renewalReminderContent{
			Title:  fmt.Sprintf("Cancellation Deadline: %s", sub.Name),
			Header: "⏰ Cancellation Deadline",
			Lead: fmt.Sprintf("Cancel %s by %s (%d %s left) if you don't want it to renew.",
				sub.Name, cancelBy.Format(settings.GetGoDateFormatLong()), daysUntil, daysText),
			CancelBy: cancelBy,
		}
	}
	return renewalReminderContent{
		Title:  fmt.Sprintf("Renewal Reminder: %s", sub.Name),
		Header: "🔔 Renewal Reminder",
		Lead:   fmt.Sprintf("Your subscription %s will renew in %d %s.", sub.Name, daysUntil, daysText),
	}
}

// renewalReminderMessage builds the plain-text notification body shared by
// Pushover and Telegram.
func renewalReminderMessage(sub *models.Subscription, content renewalReminderContent, settings *SettingsService) string {
	currencySymbol := currencySymbolForSubscription(sub, settings)
	currencyCode := currencyCodeForSubscription(sub, settings)
	message := content.Header + "\n\n" + content.Lead + "\n\n"
	message += "Subscription Details:\n"
	message += fmt.Sprintf("Cost: %s%s %s\n", currencySymbol, FormatAmount(sub.Cost, currencyCode), sub.DisplaySchedule())
	message += fmt.Sprintf("Monthly Cost: %s%s\n", currencySymbol, FormatAmount(sub.MonthlyCost(), currencyCode))
	if sub.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", sub.Category.Name)
	}
	if sub.RenewalDate != nil {
		message += fmt.Sprintf("Renewal Date: %s\n", sub.RenewalDate.Format(settings.GetGoDateFormatLong()))
	}
	if content.CancelBy != nil {
		message += fmt.Sprintf("Cancel By: %s\n", content.CancelBy.Format(settings.GetGoDateFormatLong()))
	}
	if sub.URL != "" {
		message += fmt.Sprintf("URL: %s", sub.URL)
	}
	return message
}

package service

import (
	"fmt"
	"net/url"
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

// highCostAlertText builds the plain-text title and body of a high-cost alert
// shared by the push channels (Pushover, Telegram, ntfy and Gotify).
func highCostAlertText(sub *models.Subscription, settings *SettingsService) (title, message string) {
	currencySymbol := currencySymbolForSubscription(sub, settings)
	currencyCode := currencyCodeForSubscription(sub, settings)
	message = "⚠️ High Cost Alert\n\n"
	message += fmt.Sprintf("Subscription: %s\n", sub.Name)
	message += fmt.Sprintf("Cost: %s%s %s\n", currencySymbol, FormatAmount(sub.Cost, currencyCode), sub.DisplaySchedule())
	message += fmt.Sprintf("Monthly Cost: %s%s\n", currencySymbol, FormatAmount(sub.MonthlyCost(), currencyCode))
	if sub.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", sub.Category.Name)
	}
	if sub.RenewalDate != nil {
		message += fmt.Sprintf("Next Renewal: %s\n", sub.RenewalDate.Format(settings.GetGoDateFormatLong()))
	}
	if sub.URL != "" {
		message += fmt.Sprintf("URL: %s", sub.URL)
	}
	return fmt.Sprintf("High Cost Alert: %s", sub.Name), message
}

// cancellationReminderText builds the plain-text title and body of a
// cancellation reminder shared by the push channels.
func cancellationReminderText(sub *models.Subscription, daysUntil int, settings *SettingsService) (title, message string) {
	currencySymbol := currencySymbolForSubscription(sub, settings)
	currencyCode := currencyCodeForSubscription(sub, settings)
	daysText := "days"
	if daysUntil == 1 {
		daysText = "day"
	}
	message = "⚠️ Cancellation Reminder\n\n"
	message += fmt.Sprintf("Your subscription %s will end in %d %s.\n\n", sub.Name, daysUntil, daysText)
	message += "Subscription Details:\n"
	message += fmt.Sprintf("Cost: %s%s %s\n", currencySymbol, FormatAmount(sub.Cost, currencyCode), sub.DisplaySchedule())
	message += fmt.Sprintf("Monthly Cost: %s%s\n", currencySymbol, FormatAmount(sub.MonthlyCost(), currencyCode))
	if sub.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", sub.Category.Name)
	}
	if sub.CancellationDate != nil {
		message += fmt.Sprintf("Cancellation Date: %s\n", sub.CancellationDate.Format(settings.GetGoDateFormatLong()))
	}
	if sub.URL != "" {
		message += fmt.Sprintf("URL: %s", sub.URL)
	}
	return fmt.Sprintf("Cancellation Reminder: %s", sub.Name), message
}

// toCancelReminderContent is the channel-independent content of a to-cancel
// reminder. Dates are already formatted; CancelBy is empty when no notice period
// is set, in which case the renewal date is the deadline.
type toCancelReminderContent struct {
	Title    string
	Header   string
	Lead     string
	CancelBy string
	Renewal  string
	URL      string
}

func buildToCancelReminderContent(sub *models.Subscription, daysLeft int, settings *SettingsService) toCancelReminderContent {
	dateFormat := settings.GetGoDateFormatLong()
	content := toCancelReminderContent{
		Title:  fmt.Sprintf("Reminder to Cancel: %s", sub.Name),
		Header: "🚫 Reminder to Cancel",
		URL:    notificationSafeURL(sub.URL),
	}
	if sub.RenewalDate != nil {
		content.Renewal = sub.RenewalDate.Format(dateFormat)
	}
	remaining := fmt.Sprintf("%d days left", daysLeft)
	switch daysLeft {
	case 0:
		remaining = "last day"
	case 1:
		remaining = "1 day left"
	}
	if cancelBy := sub.CancelByDate(); cancelBy != nil {
		content.CancelBy = cancelBy.Format(dateFormat)
		content.Lead = fmt.Sprintf("You planned to cancel %s. Cancel by %s (%s) to stop it renewing on %s.", sub.Name, content.CancelBy, remaining, content.Renewal)
		return content
	}
	content.Lead = fmt.Sprintf("You planned to cancel %s. Cancel before it renews on %s (%s).", sub.Name, content.Renewal, remaining)
	return content
}

// toCancelReminderMessage builds the plain-text body shared by the push channels.
func toCancelReminderMessage(sub *models.Subscription, content toCancelReminderContent, settings *SettingsService) string {
	currencySymbol := currencySymbolForSubscription(sub, settings)
	currencyCode := currencyCodeForSubscription(sub, settings)
	message := content.Header + "\n\n" + content.Lead + "\n\n"
	message += "Subscription Details:\n"
	message += fmt.Sprintf("Cost: %s%s %s\n", currencySymbol, FormatAmount(sub.Cost, currencyCode), sub.DisplaySchedule())
	message += fmt.Sprintf("Monthly Cost: %s%s\n", currencySymbol, FormatAmount(sub.MonthlyCost(), currencyCode))
	if sub.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", sub.Category.Name)
	}
	if content.CancelBy != "" {
		message += fmt.Sprintf("Cancel By: %s\n", content.CancelBy)
	}
	message += fmt.Sprintf("Renewal Date: %s\n", content.Renewal)
	if content.URL != "" {
		message += fmt.Sprintf("URL: %s", content.URL)
	}
	return message
}

// notificationSafeURL drops the parts of a subscription URL that can carry
// credentials (user info, query string, fragment) before it leaves for a third
// party. Anything that is not a web address is left out rather than sent as typed.
func notificationSafeURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Scheme == "" {
		parsed, err = url.Parse("https://" + raw)
	}
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	parsed.User, parsed.RawQuery, parsed.Fragment, parsed.RawFragment = nil, "", "", ""
	return parsed.String()
}

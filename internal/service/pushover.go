package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"subtrackr/internal/models"
	"time"
)

// PushoverService handles sending notifications via Pushover
type PushoverService struct {
	settingsService *SettingsService
}

// NewPushoverService creates a new Pushover service
func NewPushoverService(settingsService *SettingsService) *PushoverService {
	return &PushoverService{
		settingsService: settingsService,
	}
}

// PushoverResponse represents the response from Pushover API
type PushoverResponse struct {
	Status  int      `json:"status"`
	Request string   `json:"request"`
	Errors  []string `json:"errors,omitempty"`
}

// SendNotification sends a notification via Pushover
func (p *PushoverService) SendNotification(title, message string, priority int) error {
	config, err := p.settingsService.GetPushoverConfig()
	if err != nil {
		return fmt.Errorf("failed to get Pushover config: %w", err)
	}

	if config.UserKey == "" || config.AppToken == "" {
		return fmt.Errorf("Pushover not configured: user key and app token required")
	}

	// Pushover API endpoint
	apiURL := "https://api.pushover.net/1/messages.json"

	// Prepare form data
	formData := url.Values{}
	formData.Set("token", config.AppToken)
	formData.Set("user", config.UserKey)
	formData.Set("title", title)
	formData.Set("message", message)
	formData.Set("priority", strconv.Itoa(priority))

	// Create HTTP request
	req, err := http.NewRequest("POST", apiURL, bytes.NewBufferString(formData.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Send request
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Pushover notification: %w", err)
	}
	defer resp.Body.Close()

	// Parse response
	var pushoverResp PushoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&pushoverResp); err != nil {
		return fmt.Errorf("failed to decode Pushover response: %w", err)
	}

	if pushoverResp.Status != 1 {
		errorMsg := "Pushover API error"
		if len(pushoverResp.Errors) > 0 {
			errorMsg = pushoverResp.Errors[0]
		}
		return fmt.Errorf("%s", errorMsg)
	}

	return nil
}

// SendHighCostAlert sends a Pushover alert when a high-cost subscription is created
func (p *PushoverService) SendHighCostAlert(subscription *models.Subscription) error {
	// Check if high cost alerts are enabled
	enabled, err := p.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol
	currencySymbol := p.settingsService.GetCurrencySymbol()

	// Build message
	message := "⚠️ High Cost Alert\n\n"
	message += fmt.Sprintf("Subscription: %s\n", subscription.Name)
	message += fmt.Sprintf("Cost: %s%.2f %s\n", currencySymbol, subscription.Cost, subscription.Schedule)
	message += fmt.Sprintf("Monthly Cost: %s%.2f\n", currencySymbol, subscription.MonthlyCost())
	if subscription.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", subscription.Category.Name)
	}
	if subscription.RenewalDate != nil {
		message += fmt.Sprintf("Next Renewal: %s\n", subscription.RenewalDate.Format("January 2, 2006"))
	}
	if subscription.URL != "" {
		message += fmt.Sprintf("URL: %s", subscription.URL)
	}

	title := fmt.Sprintf("High Cost Alert: %s", subscription.Name)
	// Priority 1 = high priority (with sound and vibration)
	return p.SendNotification(title, message, 1)
}

// SendRenewalReminder sends a Pushover reminder for an upcoming subscription renewal
func (p *PushoverService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	// Check if renewal reminders are enabled
	enabled, err := p.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol
	currencySymbol := p.settingsService.GetCurrencySymbol()

	// Build message
	daysText := "days"
	if daysUntilRenewal == 1 {
		daysText = "day"
	}
	message := "🔔 Renewal Reminder\n\n"
	message += fmt.Sprintf("Your subscription %s will renew in %d %s.\n\n", subscription.Name, daysUntilRenewal, daysText)
	message += "Subscription Details:\n"
	message += fmt.Sprintf("Cost: %s%.2f %s\n", currencySymbol, subscription.Cost, subscription.Schedule)
	message += fmt.Sprintf("Monthly Cost: %s%.2f\n", currencySymbol, subscription.MonthlyCost())
	if subscription.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", subscription.Category.Name)
	}
	if subscription.RenewalDate != nil {
		message += fmt.Sprintf("Renewal Date: %s\n", subscription.RenewalDate.Format("January 2, 2006"))
	}
	if subscription.URL != "" {
		message += fmt.Sprintf("URL: %s", subscription.URL)
	}

	title := fmt.Sprintf("Renewal Reminder: %s", subscription.Name)
	// Priority 0 = normal priority
	return p.SendNotification(title, message, 0)
}

// SendCancellationReminder sends a Pushover reminder for an upcoming subscription cancellation
func (p *PushoverService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	// Check if cancellation reminders are enabled
	enabled, err := p.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol
	currencySymbol := p.settingsService.GetCurrencySymbol()

	// Build message
	daysText := "days"
	if daysUntilCancellation == 1 {
		daysText = "day"
	}
	message := "⚠️ Cancellation Reminder\n\n"
	message += fmt.Sprintf("Your subscription %s will end in %d %s.\n\n", subscription.Name, daysUntilCancellation, daysText)
	message += "Subscription Details:\n"
	message += fmt.Sprintf("Cost: %s%.2f %s\n", currencySymbol, subscription.Cost, subscription.Schedule)
	message += fmt.Sprintf("Monthly Cost: %s%.2f\n", currencySymbol, subscription.MonthlyCost())
	if subscription.Category.Name != "" {
		message += fmt.Sprintf("Category: %s\n", subscription.Category.Name)
	}
	if subscription.CancellationDate != nil {
		message += fmt.Sprintf("Cancellation Date: %s\n", subscription.CancellationDate.Format("January 2, 2006"))
	}
	if subscription.URL != "" {
		message += fmt.Sprintf("URL: %s", subscription.URL)
	}

	title := fmt.Sprintf("Cancellation Reminder: %s", subscription.Name)
	// Priority 0 = normal priority
	return p.SendNotification(title, message, 0)
}


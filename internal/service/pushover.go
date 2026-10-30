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
	apiURL          string
}

// NewPushoverService creates a new Pushover service
func NewPushoverService(settingsService *SettingsService) *PushoverService {
	return &PushoverService{
		settingsService: settingsService,
		apiURL:          "https://api.pushover.net/1/messages.json",
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

	// Prepare form data
	formData := url.Values{}
	formData.Set("token", config.AppToken)
	formData.Set("user", config.UserKey)
	formData.Set("title", title)
	formData.Set("message", message)
	formData.Set("priority", strconv.Itoa(priority))

	// Create HTTP request
	req, err := http.NewRequest("POST", p.apiURL, bytes.NewBufferString(formData.Encode()))
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

	title, message := highCostAlertText(subscription, p.settingsService)
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

	content := buildRenewalReminderContent(subscription, daysUntilRenewal, p.settingsService)
	// Priority 0 = normal priority
	return p.SendNotification(content.Title, renewalReminderMessage(subscription, content, p.settingsService), 0)
}

// SendCancellationReminder sends a Pushover reminder for an upcoming subscription cancellation
func (p *PushoverService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	// Check if cancellation reminders are enabled
	enabled, err := p.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	title, message := cancellationReminderText(subscription, daysUntilCancellation, p.settingsService)
	// Priority 0 = normal priority
	return p.SendNotification(title, message, 0)
}

// SendToCancelReminder sends a Pushover reminder to cancel a subscription.
func (p *PushoverService) SendToCancelReminder(subscription *models.Subscription, daysLeft int) error {
	content := buildToCancelReminderContent(subscription, daysLeft, p.settingsService)
	return p.SendNotification(content.Title, toCancelReminderMessage(subscription, content, p.settingsService), 0)
}

// Name identifies the channel in delivery reports and logs.
func (p *PushoverService) Name() string { return "pushover" }

// IsConfigured reports whether a user key and app token have been saved.
func (p *PushoverService) IsConfigured() bool {
	config, err := p.settingsService.GetPushoverConfig()
	return err == nil && config.UserKey != "" && config.AppToken != ""
}

package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"subtrackr/internal/models"
	"time"
)

// WebhookService handles sending notifications via generic webhooks
type WebhookService struct {
	settingsService *SettingsService
}

// NewWebhookService creates a new Webhook service
func NewWebhookService(settingsService *SettingsService) *WebhookService {
	return &WebhookService{
		settingsService: settingsService,
	}
}

// WebhookPayload is the JSON body sent to webhook endpoints
type WebhookPayload struct {
	Event        string               `json:"event"`
	Title        string               `json:"title"`
	Message      string               `json:"message"`
	Subscription *WebhookSubscription `json:"subscription"`
	Timestamp    string               `json:"timestamp"`
}

// WebhookSubscription is a simplified subscription for webhook payloads
type WebhookSubscription struct {
	ID               uint    `json:"id"`
	Name             string  `json:"name"`
	Cost             float64 `json:"cost"`
	Currency         string  `json:"currency"`
	CurrencySymbol   string  `json:"currency_symbol"`
	Schedule         string  `json:"schedule"`
	MonthlyCost      float64 `json:"monthly_cost"`
	Category         string  `json:"category,omitempty"`
	URL              string  `json:"url,omitempty"`
	RenewalDate      string  `json:"renewal_date,omitempty"`
	CancellationDate string  `json:"cancellation_date,omitempty"`
}

func subscriptionToWebhook(sub *models.Subscription, settings *SettingsService) *WebhookSubscription {
	currencySymbol := currencySymbolForSubscription(sub, settings)
	ws := &WebhookSubscription{
		ID:             sub.ID,
		Name:           sub.Name,
		Cost:           sub.Cost,
		Currency:       sub.OriginalCurrency,
		CurrencySymbol: currencySymbol,
		Schedule:       sub.Schedule,
		MonthlyCost:    sub.MonthlyCost(),
	}
	if sub.Category.Name != "" {
		ws.Category = sub.Category.Name
	}
	if sub.URL != "" {
		ws.URL = sub.URL
	}
	dateFormat := settings.GetGoDateFormat()
	if sub.RenewalDate != nil {
		ws.RenewalDate = sub.RenewalDate.Format(dateFormat)
	}
	if sub.CancellationDate != nil {
		ws.CancellationDate = sub.CancellationDate.Format(dateFormat)
	}
	return ws
}

// SendWebhook sends a payload to the configured webhook endpoint
func (w *WebhookService) SendWebhook(payload *WebhookPayload) error {
	config, err := w.settingsService.GetWebhookConfig()
	if err != nil || config.URL == "" {
		return nil // Not configured, silently skip (matches email/pushover behavior)
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %w", err)
	}

	req, err := http.NewRequest("POST", config.URL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "SubTrackr-Webhook/1.0")

	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return nil
}

// SendHighCostAlert sends a webhook alert when a high-cost subscription is created
func (w *WebhookService) SendHighCostAlert(subscription *models.Subscription) error {
	enabled, err := w.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil
	}

	currencySymbol := currencySymbolForSubscription(subscription, w.settingsService)
	payload := &WebhookPayload{
		Event:        "high_cost_alert",
		Title:        fmt.Sprintf("High Cost Alert: %s", subscription.Name),
		Message:      fmt.Sprintf("A new high-cost subscription has been added: %s at %s%.2f %s", subscription.Name, currencySymbol, subscription.Cost, subscription.Schedule),
		Subscription: subscriptionToWebhook(subscription, w.settingsService),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	return w.SendWebhook(payload)
}

// SendRenewalReminder sends a webhook reminder for an upcoming subscription renewal
func (w *WebhookService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	enabled, err := w.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil
	}

	daysText := "days"
	if daysUntilRenewal == 1 {
		daysText = "day"
	}
	payload := &WebhookPayload{
		Event:        "renewal_reminder",
		Title:        fmt.Sprintf("Renewal Reminder: %s", subscription.Name),
		Message:      fmt.Sprintf("Your subscription %s will renew in %d %s", subscription.Name, daysUntilRenewal, daysText),
		Subscription: subscriptionToWebhook(subscription, w.settingsService),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	return w.SendWebhook(payload)
}

// SendCancellationReminder sends a webhook reminder for an upcoming subscription cancellation
func (w *WebhookService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	enabled, err := w.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil
	}

	daysText := "days"
	if daysUntilCancellation == 1 {
		daysText = "day"
	}
	payload := &WebhookPayload{
		Event:        "cancellation_reminder",
		Title:        fmt.Sprintf("Cancellation Reminder: %s", subscription.Name),
		Message:      fmt.Sprintf("Your subscription %s will end in %d %s", subscription.Name, daysUntilCancellation, daysText),
		Subscription: subscriptionToWebhook(subscription, w.settingsService),
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	}

	return w.SendWebhook(payload)
}

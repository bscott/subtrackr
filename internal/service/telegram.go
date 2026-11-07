package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"subtrackr/internal/models"
	"time"
)

// Telegram API and retry defaults.
const (
	telegramAPIBaseURL     = "https://api.telegram.org"
	telegramMaxRetries     = 3
	telegramInitialBackoff = 500 * time.Millisecond
)

// TelegramService handles sending notifications via the Telegram Bot API.
type TelegramService struct {
	settingsService *SettingsService

	// apiBaseURL allows overriding the Telegram API endpoint (used in tests).
	apiBaseURL string
	// maxRetries and initialBackoff control retry-with-backoff on transient failures.
	maxRetries     int
	initialBackoff time.Duration
	httpClient     *http.Client
}

// NewTelegramService creates a new Telegram service.
func NewTelegramService(settingsService *SettingsService) *TelegramService {
	return &TelegramService{
		settingsService: settingsService,
		apiBaseURL:      telegramAPIBaseURL,
		maxRetries:      telegramMaxRetries,
		initialBackoff:  telegramInitialBackoff,
		httpClient:      &http.Client{Timeout: 10 * time.Second},
	}
}

// telegramResponse represents the JSON envelope returned by the Telegram Bot API.
type telegramResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code,omitempty"`
	Description string `json:"description,omitempty"`
}

// SendNotification sends a message via Telegram, combining an optional title
// with the body. External calls are retried with exponential backoff on
// transient failures (network errors, HTTP 429, and 5xx responses).
func (t *TelegramService) SendNotification(title, message string) error {
	config, err := t.settingsService.GetTelegramConfig()
	if err != nil {
		return fmt.Errorf("failed to get Telegram config: %w", err)
	}

	if config.BotToken == "" || config.ChatID == "" {
		return fmt.Errorf("Telegram not configured: bot token and chat ID required")
	}

	text := message
	if title != "" {
		text = title + "\n\n" + message
	}

	// Telegram Bot API sendMessage endpoint.
	apiURL := fmt.Sprintf("%s/bot%s/sendMessage", t.apiBaseURL, config.BotToken)

	formData := url.Values{}
	formData.Set("chat_id", config.ChatID)
	formData.Set("text", text)

	if err := t.sendWithRetry(apiURL, formData.Encode()); err != nil {
		// The bot token is part of the request URL, and transport errors
		// (*url.Error) include the full URL in their message. Scrub the token
		// before the error reaches server logs or HTTP responses.
		return errors.New(strings.ReplaceAll(err.Error(), "/bot"+config.BotToken, "/bot[REDACTED]"))
	}
	return nil
}

// sendWithRetry attempts the request, retrying transient failures with
// exponential backoff up to maxRetries times.
func (t *TelegramService) sendWithRetry(apiURL, body string) error {
	backoff := t.initialBackoff
	if backoff <= 0 {
		backoff = telegramInitialBackoff
	}
	maxRetries := t.maxRetries
	if maxRetries < 1 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		retryable, err := t.doSend(apiURL, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == maxRetries {
			break
		}
		time.Sleep(backoff)
		backoff *= 2 // exponential backoff
	}
	return lastErr
}

// doSend performs a single request. It returns whether the failure is
// retryable along with the error (nil error means success).
func (t *TelegramService) doSend(apiURL, body string) (retryable bool, err error) {
	req, err := http.NewRequest("POST", apiURL, bytes.NewBufferString(body))
	if err != nil {
		return false, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		// Network-level errors are transient; retry.
		return true, fmt.Errorf("failed to send Telegram notification: %w", err)
	}
	defer resp.Body.Close()

	var tgResp telegramResponse
	if decodeErr := json.NewDecoder(resp.Body).Decode(&tgResp); decodeErr != nil {
		// Retry on 5xx/429 even if the body isn't valid JSON.
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		return retryable, fmt.Errorf("failed to decode Telegram response (status %d): %w", resp.StatusCode, decodeErr)
	}

	if !tgResp.OK {
		retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests
		desc := tgResp.Description
		if desc == "" {
			desc = fmt.Sprintf("Telegram API error (status %d)", resp.StatusCode)
		}
		return retryable, fmt.Errorf("%s", desc)
	}

	return false, nil
}

// SendHighCostAlert sends a Telegram alert when a high-cost subscription is created.
func (t *TelegramService) SendHighCostAlert(subscription *models.Subscription) error {
	// Check if high cost alerts are enabled
	enabled, err := t.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	title, message := highCostAlertText(subscription, t.settingsService)
	return t.SendNotification(title, message)
}

// SendRenewalReminder sends a Telegram reminder for an upcoming subscription renewal.
func (t *TelegramService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	// Check if renewal reminders are enabled
	enabled, err := t.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	content := buildRenewalReminderContent(subscription, daysUntilRenewal, t.settingsService)
	return t.SendNotification(content.Title, renewalReminderMessage(subscription, content, t.settingsService))
}

// SendCancellationReminder sends a Telegram reminder for an upcoming subscription cancellation.
func (t *TelegramService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	// Check if cancellation reminders are enabled
	enabled, err := t.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	title, message := cancellationReminderText(subscription, daysUntilCancellation, t.settingsService)
	return t.SendNotification(title, message)
}

// SendToCancelReminder sends a Telegram reminder to cancel a subscription.
func (t *TelegramService) SendToCancelReminder(subscription *models.Subscription, daysLeft int) error {
	content := buildToCancelReminderContent(subscription, daysLeft, t.settingsService)
	return t.SendNotification(content.Title, toCancelReminderMessage(subscription, content, t.settingsService))
}

// Name identifies the channel in delivery reports and logs.
func (t *TelegramService) Name() string { return "telegram" }

// IsConfigured reports whether a bot token and chat ID have been saved.
func (t *TelegramService) IsConfigured() bool {
	config, err := t.settingsService.GetTelegramConfig()
	return err == nil && config.BotToken != "" && config.ChatID != ""
}

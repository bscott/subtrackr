package service

import (
	"errors"
	"fmt"
	"subtrackr/internal/models"
)

// ntfy defaults.
const (
	NtfyDefaultServerURL = "https://ntfy.sh"
	NtfyDefaultPriority  = 3
)

// ErrNtfyNotConfigured is returned when no topic has been configured.
var ErrNtfyNotConfigured = errors.New("ntfy not configured: topic required")

// NtfyService sends notifications to an ntfy server (ntfy.sh or self-hosted).
type NtfyService struct {
	settingsService *SettingsService
	client          pushClient
}

// NewNtfyService creates a new ntfy service.
func NewNtfyService(settingsService *SettingsService) *NtfyService {
	return &NtfyService{settingsService: settingsService, client: newPushClient("ntfy")}
}

// ntfyMessage is the JSON publish body. ntfy expects it at the server root,
// not the topic URL; see https://docs.ntfy.sh/publish/#publish-as-json.
// Messages are plain text (markdown stays false), so subscription names are
// shown verbatim and need no escaping.
type ntfyMessage struct {
	Topic    string `json:"topic"`
	Title    string `json:"title,omitempty"`
	Message  string `json:"message"`
	Priority int    `json:"priority,omitempty"`
	Click    string `json:"click,omitempty"`
}

// Name identifies the channel in delivery reports and logs.
func (n *NtfyService) Name() string { return "ntfy" }

// IsConfigured reports whether a topic has been saved.
func (n *NtfyService) IsConfigured() bool {
	config, err := n.settingsService.GetNtfyConfig()
	return err == nil && config.Topic != ""
}

// SendNotification sends a message using the saved configuration.
func (n *NtfyService) SendNotification(title, message string) error {
	config, err := n.settingsService.GetNtfyConfig()
	if err != nil || config.Topic == "" {
		return ErrNtfyNotConfigured
	}
	return n.Send(config, title, message)
}

// Send publishes a message with an explicit configuration, so settings tests
// can use submitted values without saving them first.
func (n *NtfyService) Send(config *models.NtfyConfig, title, message string) error {
	if config == nil || config.Topic == "" {
		return ErrNtfyNotConfigured
	}
	serverURL := config.ServerURL
	if serverURL == "" {
		serverURL = NtfyDefaultServerURL
	}
	serverURL, err := NormalizePushServerURL(serverURL)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	if err := ValidateNtfyTopic(config.Topic); err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	if err := ValidatePushToken(config.AccessToken); err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}

	headers := map[string]string{}
	if config.AccessToken != "" {
		headers["Authorization"] = "Bearer " + config.AccessToken
	}
	return n.client.postJSON(serverURL+"/", headers, ntfyMessage{
		Topic:    config.Topic,
		Title:    title,
		Message:  message,
		Priority: NormalizeNtfyPriority(config.Priority),
		Click:    notificationClickURL(n.settingsService),
	})
}

// NormalizeNtfyPriority maps unset or out-of-range priorities to ntfy's default.
func NormalizeNtfyPriority(priority int) int {
	if priority < 1 || priority > 5 {
		return NtfyDefaultPriority
	}
	return priority
}

// SendHighCostAlert sends an ntfy alert when a high-cost subscription is created.
func (n *NtfyService) SendHighCostAlert(subscription *models.Subscription) error {
	enabled, err := n.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil
	}
	title, message := highCostAlertText(subscription, n.settingsService)
	return n.SendNotification(title, message)
}

// SendRenewalReminder sends an ntfy reminder for an upcoming renewal.
func (n *NtfyService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	enabled, err := n.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil
	}
	content := buildRenewalReminderContent(subscription, daysUntilRenewal, n.settingsService)
	return n.SendNotification(content.Title, renewalReminderMessage(subscription, content, n.settingsService))
}

// SendCancellationReminder sends an ntfy reminder for an upcoming cancellation.
func (n *NtfyService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	enabled, err := n.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil
	}
	title, message := cancellationReminderText(subscription, daysUntilCancellation, n.settingsService)
	return n.SendNotification(title, message)
}

// SendToCancelReminder sends an ntfy reminder to cancel a subscription.
func (n *NtfyService) SendToCancelReminder(subscription *models.Subscription, daysLeft int) error {
	content := buildToCancelReminderContent(subscription, daysLeft, n.settingsService)
	return n.SendNotification(content.Title, toCancelReminderMessage(subscription, content, n.settingsService))
}

// notificationClickURL returns the configured application base URL for push
// channels that support a tap/click target, or "" when unset or unusable.
func notificationClickURL(settings *SettingsService) string {
	if settings == nil {
		return ""
	}
	baseURL, err := NormalizePushServerURL(settings.GetBaseURL())
	if err != nil {
		return ""
	}
	return baseURL
}

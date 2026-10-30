package service

import (
	"errors"
	"fmt"
	"subtrackr/internal/models"
)

// Gotify defaults.
const (
	GotifyDefaultPriority = 5
	GotifyMaxPriority     = 10
)

// ErrGotifyNotConfigured is returned when the server URL or app token is missing.
var ErrGotifyNotConfigured = errors.New("Gotify not configured: server URL and app token required")

// GotifyService sends notifications to a self-hosted Gotify server.
type GotifyService struct {
	settingsService *SettingsService
	client          pushClient
}

// NewGotifyService creates a new Gotify service.
func NewGotifyService(settingsService *SettingsService) *GotifyService {
	return &GotifyService{settingsService: settingsService, client: newPushClient("Gotify")}
}

// gotifyMessage is the body of POST /message; see the CreateMessage model in
// the Gotify API spec. Extras pin plain-text rendering so names containing
// markdown characters display verbatim.
type gotifyMessage struct {
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message"`
	Priority int            `json:"priority"`
	Extras   map[string]any `json:"extras,omitempty"`
}

// Name identifies the channel in delivery reports and logs.
func (g *GotifyService) Name() string { return "gotify" }

// IsConfigured reports whether a server URL and app token have been saved.
func (g *GotifyService) IsConfigured() bool {
	config, err := g.settingsService.GetGotifyConfig()
	return err == nil && config.ServerURL != "" && config.AppToken != ""
}

// SendNotification sends a message using the saved configuration.
func (g *GotifyService) SendNotification(title, message string) error {
	config, err := g.settingsService.GetGotifyConfig()
	if err != nil {
		return ErrGotifyNotConfigured
	}
	return g.Send(config, title, message)
}

// Send posts a message with an explicit configuration, so settings tests can
// use submitted values without saving them first. The app token travels in
// the X-Gotify-Key header, never in the query string.
func (g *GotifyService) Send(config *models.GotifyConfig, title, message string) error {
	if config == nil || config.ServerURL == "" || config.AppToken == "" {
		return ErrGotifyNotConfigured
	}
	serverURL, err := NormalizePushServerURL(config.ServerURL)
	if err != nil {
		return fmt.Errorf("Gotify: %w", err)
	}
	if err := ValidatePushToken(config.AppToken); err != nil {
		return fmt.Errorf("Gotify: %w", err)
	}

	extras := map[string]any{
		"client::display": map[string]any{"contentType": "text/plain"},
	}
	if click := notificationClickURL(g.settingsService); click != "" {
		extras["client::notification"] = map[string]any{"click": map[string]any{"url": click}}
	}
	return g.client.postJSON(serverURL+"/message", map[string]string{"X-Gotify-Key": config.AppToken}, gotifyMessage{
		Title:    title,
		Message:  message,
		Priority: NormalizeGotifyPriority(config.Priority),
		Extras:   extras,
	})
}

// NormalizeGotifyPriority clamps a priority into Gotify's 0-10 range.
func NormalizeGotifyPriority(priority int) int {
	if priority < 0 || priority > GotifyMaxPriority {
		return GotifyDefaultPriority
	}
	return priority
}

// SendHighCostAlert sends a Gotify alert when a high-cost subscription is created.
func (g *GotifyService) SendHighCostAlert(subscription *models.Subscription) error {
	enabled, err := g.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil
	}
	title, message := highCostAlertText(subscription, g.settingsService)
	return g.SendNotification(title, message)
}

// SendRenewalReminder sends a Gotify reminder for an upcoming renewal.
func (g *GotifyService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	enabled, err := g.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil
	}
	content := buildRenewalReminderContent(subscription, daysUntilRenewal, g.settingsService)
	return g.SendNotification(content.Title, renewalReminderMessage(subscription, content, g.settingsService))
}

// SendCancellationReminder sends a Gotify reminder for an upcoming cancellation.
func (g *GotifyService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	enabled, err := g.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil
	}
	title, message := cancellationReminderText(subscription, daysUntilCancellation, g.settingsService)
	return g.SendNotification(title, message)
}

// SendToCancelReminder sends a Gotify reminder to cancel a subscription.
func (g *GotifyService) SendToCancelReminder(subscription *models.Subscription, daysLeft int) error {
	content := buildToCancelReminderContent(subscription, daysLeft, g.settingsService)
	return g.SendNotification(content.Title, toCancelReminderMessage(subscription, content, g.settingsService))
}

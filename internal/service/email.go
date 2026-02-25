package service

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/smtp"
	"subtrackr/internal/models"
)

// currencySymbolForSubscription returns the appropriate currency symbol for a subscription.
// If the subscription has an original currency that differs from the preferred currency,
// use the subscription's own currency symbol to avoid misleading display.
func currencySymbolForSubscription(subscription *models.Subscription, settings *SettingsService) string {
	preferred := settings.GetCurrency()
	if subscription.OriginalCurrency != "" && subscription.OriginalCurrency != preferred {
		return CurrencySymbolForCode(subscription.OriginalCurrency)
	}
	return settings.GetCurrencySymbol()
}

// EmailService handles sending emails via SMTP
type EmailService struct {
	settingsService *SettingsService
}

// NewEmailService creates a new email service
func NewEmailService(settingsService *SettingsService) *EmailService {
	return &EmailService{
		settingsService: settingsService,
	}
}

// SendEmail sends an email using the configured SMTP settings
func (e *EmailService) SendEmail(subject, body string) error {
	config, err := e.settingsService.GetSMTPConfig()
	if err != nil {
		return fmt.Errorf("failed to get SMTP config: %w", err)
	}

	if config.To == "" {
		return fmt.Errorf("no recipient email configured")
	}

	// Determine if this is an implicit TLS port (SMTPS)
	isSSLPort := config.Port == 465 || config.Port == 8465 || config.Port == 443

	var auth smtp.Auth
	var addr string

	auth = smtp.PlainAuth("", config.Username, config.Password, config.Host)
	addr = fmt.Sprintf("%s:%d", config.Host, config.Port)

	if isSSLPort {
		// Use implicit TLS (direct SSL connection)
		tlsConfig := &tls.Config{
			ServerName: config.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to connect via SSL: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, config.Host)
		if err != nil {
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		defer client.Close()

		// Authenticate
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Set sender and recipient
		if err = client.Mail(config.From); err != nil {
			return fmt.Errorf("failed to set sender: %w", err)
		}
		if err = client.Rcpt(config.To); err != nil {
			return fmt.Errorf("failed to set recipient: %w", err)
		}

		// Send email body
		writer, err := client.Data()
		if err != nil {
			return fmt.Errorf("failed to get data writer: %w", err)
		}

		fromName := config.FromName
		if fromName == "" {
			fromName = "SubTrackr"
		}

		message := fmt.Sprintf("From: %s <%s>\r\n", fromName, config.From)
		message += fmt.Sprintf("To: %s\r\n", config.To)
		message += fmt.Sprintf("Subject: %s\r\n", subject)
		message += "MIME-Version: 1.0\r\n"
		message += "Content-Type: text/html; charset=UTF-8\r\n"
		message += "\r\n"
		message += body

		_, err = writer.Write([]byte(message))
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}
		err = writer.Close()
		if err != nil {
			return fmt.Errorf("failed to close writer: %w", err)
		}
	} else {
		// Use STARTTLS (opportunistic TLS)
		client, err := smtp.Dial(addr)
		if err != nil {
			return fmt.Errorf("failed to connect: %w", err)
		}
		defer client.Close()

		// Upgrade to TLS
		tlsConfig := &tls.Config{
			ServerName: config.Host,
		}

		if err = client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("failed to start TLS: %w", err)
		}

		// Authenticate
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		// Set sender and recipient
		if err = client.Mail(config.From); err != nil {
			return fmt.Errorf("failed to set sender: %w", err)
		}
		if err = client.Rcpt(config.To); err != nil {
			return fmt.Errorf("failed to set recipient: %w", err)
		}

		// Send email body
		writer, err := client.Data()
		if err != nil {
			return fmt.Errorf("failed to get data writer: %w", err)
		}

		fromName := config.FromName
		if fromName == "" {
			fromName = "SubTrackr"
		}

		message := fmt.Sprintf("From: %s <%s>\r\n", fromName, config.From)
		message += fmt.Sprintf("To: %s\r\n", config.To)
		message += fmt.Sprintf("Subject: %s\r\n", subject)
		message += "MIME-Version: 1.0\r\n"
		message += "Content-Type: text/html; charset=UTF-8\r\n"
		message += "\r\n"
		message += body

		_, err = writer.Write([]byte(message))
		if err != nil {
			return fmt.Errorf("failed to write message: %w", err)
		}
		err = writer.Close()
		if err != nil {
			return fmt.Errorf("failed to close writer: %w", err)
		}
	}

	return nil
}

// SendHighCostAlert sends an email alert when a high-cost subscription is created
func (e *EmailService) SendHighCostAlert(subscription *models.Subscription) error {
	// Check if high cost alerts are enabled
	enabled, err := e.settingsService.GetBoolSetting("high_cost_alerts", true)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol - use subscription's own currency if it differs from preferred
	currencySymbol := currencySymbolForSubscription(subscription, e.settingsService)

	// Build email body
	tmpl := `
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.alert { background-color: #fff3cd; border: 1px solid #ffc107; border-radius: 5px; padding: 15px; margin: 20px 0; }
		.subscription-details { background-color: #f8f9fa; padding: 15px; border-radius: 5px; margin: 20px 0; }
		.detail-row { margin: 10px 0; }
		.label { font-weight: bold; }
		.footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; font-size: 12px; color: #666; }
	</style>
</head>
<body>
	<div class="container">
		<h2>High Cost Subscription Alert</h2>
		<div class="alert">
			<strong>⚠️ Alert:</strong> A new high-cost subscription has been added to your SubTrackr account.
		</div>
		<div class="subscription-details">
			<h3>Subscription Details</h3>
			<div class="detail-row"><span class="label">Name:</span> {{.Subscription.Name}}</div>
			<div class="detail-row"><span class="label">Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" .Subscription.Cost}} {{.Subscription.Schedule}}</div>
			<div class="detail-row"><span class="label">Monthly Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" (.Subscription.MonthlyCost)}}</div>
			{{if and .Subscription.Category .Subscription.Category.Name}}<div class="detail-row"><span class="label">Category:</span> {{.Subscription.Category.Name}}</div>{{end}}
			{{if .FormattedRenewalDate}}<div class="detail-row"><span class="label">Next Renewal:</span> {{.FormattedRenewalDate}}</div>{{end}}
			{{if .Subscription.URL}}<div class="detail-row"><span class="label">URL:</span> <a href="{{.Subscription.URL}}">{{.Subscription.URL}}</a></div>{{end}}
		</div>
		<div class="footer">
			<p>This is an automated notification from SubTrackr.</p>
			<p>You can manage your notification preferences in the Settings page.</p>
		</div>
	</div>
</body>
</html>
`

	type AlertData struct {
		Subscription        *models.Subscription
		CurrencySymbol      string
		FormattedRenewalDate string
	}

	var formattedRenewal string
	if subscription.RenewalDate != nil {
		formattedRenewal = subscription.RenewalDate.Format(e.settingsService.GetGoDateFormatLong())
	}

	data := AlertData{
		Subscription:        subscription,
		CurrencySymbol:      currencySymbol,
		FormattedRenewalDate: formattedRenewal,
	}

	t, err := template.New("highCostAlert").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	subject := fmt.Sprintf("High Cost Alert: %s - %s%.2f/month", subscription.Name, currencySymbol, subscription.MonthlyCost())
	return e.SendEmail(subject, buf.String())
}

// SendRenewalReminder sends an email reminder for an upcoming subscription renewal
func (e *EmailService) SendRenewalReminder(subscription *models.Subscription, daysUntilRenewal int) error {
	// Check if renewal reminders are enabled
	enabled, err := e.settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol - use subscription's own currency if it differs from preferred
	currencySymbol := currencySymbolForSubscription(subscription, e.settingsService)

	// Build email body
	tmpl := `
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.reminder { background-color: #d1ecf1; border: 1px solid #0c5460; border-radius: 5px; padding: 15px; margin: 20px 0; }
		.subscription-details { background-color: #f8f9fa; padding: 15px; border-radius: 5px; margin: 20px 0; }
		.detail-row { margin: 10px 0; }
		.label { font-weight: bold; }
		.footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; font-size: 12px; color: #666; }
	</style>
</head>
<body>
	<div class="container">
		<h2>Subscription Renewal Reminder</h2>
		<div class="reminder">
			<strong>🔔 Reminder:</strong> Your subscription <strong>{{.Subscription.Name}}</strong> will renew in {{.DaysUntilRenewal}} {{if eq .DaysUntilRenewal 1}}day{{else}}days{{end}}.
		</div>
		<div class="subscription-details">
			<h3>Subscription Details</h3>
			<div class="detail-row"><span class="label">Name:</span> {{.Subscription.Name}}</div>
			<div class="detail-row"><span class="label">Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" .Subscription.Cost}} {{.Subscription.Schedule}}</div>
			<div class="detail-row"><span class="label">Monthly Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" (.Subscription.MonthlyCost)}}</div>
			{{if and .Subscription.Category .Subscription.Category.Name}}<div class="detail-row"><span class="label">Category:</span> {{.Subscription.Category.Name}}</div>{{end}}
			{{if .FormattedRenewalDate}}<div class="detail-row"><span class="label">Renewal Date:</span> {{.FormattedRenewalDate}}</div>{{end}}
			{{if .Subscription.URL}}<div class="detail-row"><span class="label">URL:</span> <a href="{{.Subscription.URL}}">{{.Subscription.URL}}</a></div>{{end}}
		</div>
		<div class="footer">
			<p>This is an automated reminder from SubTrackr.</p>
			<p>You can manage your notification preferences in the Settings page.</p>
		</div>
	</div>
</body>
</html>
`

	type ReminderData struct {
		Subscription         *models.Subscription
		DaysUntilRenewal     int
		CurrencySymbol       string
		FormattedRenewalDate string
	}

	var formattedRenewal string
	if subscription.RenewalDate != nil {
		formattedRenewal = subscription.RenewalDate.Format(e.settingsService.GetGoDateFormatLong())
	}

	data := ReminderData{
		Subscription:         subscription,
		DaysUntilRenewal:     daysUntilRenewal,
		CurrencySymbol:       currencySymbol,
		FormattedRenewalDate: formattedRenewal,
	}

	t, err := template.New("renewalReminder").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	daysText := "days"
	if daysUntilRenewal == 1 {
		daysText = "day"
	}
	subject := fmt.Sprintf("Renewal Reminder: %s renews in %d %s", subscription.Name, daysUntilRenewal, daysText)
	return e.SendEmail(subject, buf.String())
}

// SendCancellationReminder sends an email reminder for an upcoming subscription cancellation
func (e *EmailService) SendCancellationReminder(subscription *models.Subscription, daysUntilCancellation int) error {
	// Check if cancellation reminders are enabled
	enabled, err := e.settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return nil // Silently skip if disabled
	}

	// Get currency symbol - use subscription's own currency if it differs from preferred
	currencySymbol := currencySymbolForSubscription(subscription, e.settingsService)

	// Build email body
	tmpl := `
<!DOCTYPE html>
<html>
<head>
	<meta charset="UTF-8">
	<style>
		body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.reminder { background-color: #fff3cd; border: 1px solid #856404; border-radius: 5px; padding: 15px; margin: 20px 0; }
		.subscription-details { background-color: #f8f9fa; padding: 15px; border-radius: 5px; margin: 20px 0; }
		.detail-row { margin: 10px 0; }
		.label { font-weight: bold; }
		.footer { margin-top: 30px; padding-top: 20px; border-top: 1px solid #ddd; font-size: 12px; color: #666; }
	</style>
</head>
<body>
	<div class="container">
		<h2>Subscription Cancellation Reminder</h2>
		<div class="reminder">
			<strong>⚠️ Reminder:</strong> Your subscription <strong>{{.Subscription.Name}}</strong> will end in {{.DaysUntilCancellation}} {{if eq .DaysUntilCancellation 1}}day{{else}}days{{end}}.
		</div>
		<div class="subscription-details">
			<h3>Subscription Details</h3>
			<div class="detail-row"><span class="label">Name:</span> {{.Subscription.Name}}</div>
			<div class="detail-row"><span class="label">Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" .Subscription.Cost}} {{.Subscription.Schedule}}</div>
			<div class="detail-row"><span class="label">Monthly Cost:</span> {{.CurrencySymbol}}{{printf "%.2f" (.Subscription.MonthlyCost)}}</div>
			{{if and .Subscription.Category .Subscription.Category.Name}}<div class="detail-row"><span class="label">Category:</span> {{.Subscription.Category.Name}}</div>{{end}}
			{{if .FormattedCancellationDate}}<div class="detail-row"><span class="label">Cancellation Date:</span> {{.FormattedCancellationDate}}</div>{{end}}
			{{if .Subscription.URL}}<div class="detail-row"><span class="label">URL:</span> <a href="{{.Subscription.URL}}">{{.Subscription.URL}}</a></div>{{end}}
		</div>
		<div class="footer">
			<p>This is an automated reminder from SubTrackr.</p>
			<p>You can manage your notification preferences in the Settings page.</p>
		</div>
	</div>
</body>
</html>
`

	type CancellationReminderData struct {
		Subscription               *models.Subscription
		DaysUntilCancellation      int
		CurrencySymbol             string
		FormattedCancellationDate  string
	}

	var formattedCancellation string
	if subscription.CancellationDate != nil {
		formattedCancellation = subscription.CancellationDate.Format(e.settingsService.GetGoDateFormatLong())
	}

	data := CancellationReminderData{
		Subscription:              subscription,
		DaysUntilCancellation:     daysUntilCancellation,
		CurrencySymbol:            currencySymbol,
		FormattedCancellationDate: formattedCancellation,
	}

	t, err := template.New("cancellationReminder").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	daysText := "days"
	if daysUntilCancellation == 1 {
		daysText = "day"
	}
	subject := fmt.Sprintf("Cancellation Reminder: %s ends in %d %s", subscription.Name, daysUntilCancellation, daysText)
	return e.SendEmail(subject, buf.String())
}

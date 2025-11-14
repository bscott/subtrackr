package service

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/smtp"
	"subtrackr/internal/models"
)

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
			<div class="detail-row"><span class="label">Name:</span> {{.Name}}</div>
			<div class="detail-row"><span class="label">Cost:</span> ${{printf "%.2f" .Cost}} {{.Schedule}}</div>
			<div class="detail-row"><span class="label">Monthly Cost:</span> ${{printf "%.2f" .MonthlyCost}}</div>
			{{if and .Category .Category.Name}}<div class="detail-row"><span class="label">Category:</span> {{.Category.Name}}</div>{{end}}
			{{if .RenewalDate}}<div class="detail-row"><span class="label">Next Renewal:</span> {{.RenewalDate.Format "January 2, 2006"}}</div>{{end}}
			{{if .URL}}<div class="detail-row"><span class="label">URL:</span> <a href="{{.URL}}">{{.URL}}</a></div>{{end}}
		</div>
		<div class="footer">
			<p>This is an automated notification from SubTrackr.</p>
			<p>You can manage your notification preferences in the Settings page.</p>
		</div>
	</div>
</body>
</html>
`

	t, err := template.New("highCostAlert").Parse(tmpl)
	if err != nil {
		return fmt.Errorf("failed to parse email template: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, subscription); err != nil {
		return fmt.Errorf("failed to execute email template: %w", err)
	}

	subject := fmt.Sprintf("High Cost Alert: %s - $%.2f/month", subscription.Name, subscription.MonthlyCost())
	return e.SendEmail(subject, buf.String())
}


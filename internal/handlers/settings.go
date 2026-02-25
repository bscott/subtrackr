package handlers

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/service"
	"time"

	"github.com/gin-gonic/gin"
)

func splitLines(s string) []string { return strings.Split(s, "\n") }
func trimSpace(s string) string    { return strings.TrimSpace(s) }
func splitN(s, sep string, n int) []string { return strings.SplitN(s, sep, n) }

type SettingsHandler struct {
	service *service.SettingsService
}

func NewSettingsHandler(service *service.SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
}

// SaveSMTPSettings saves SMTP configuration
func (h *SettingsHandler) SaveSMTPSettings(c *gin.Context) {
	var config models.SMTPConfig

	// Parse form data
	config.Host = c.PostForm("smtp_host")
	config.Username = c.PostForm("smtp_username")
	config.Password = c.PostForm("smtp_password")
	config.From = c.PostForm("smtp_from")
	config.FromName = c.PostForm("smtp_from_name")
	config.To = c.PostForm("smtp_to")

	// Parse port
	if portStr := c.PostForm("smtp_port"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Validate required fields
	if config.Host == "" || config.Port == 0 || config.Username == "" || config.Password == "" || config.From == "" || config.To == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "Required SMTP fields: Host, Port, Username, Password, From email, To email",
			"Type":  "error",
		})
		return
	}

	// Save configuration
	err := h.service.SaveSMTPConfig(&config)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "smtp-message.html", gin.H{
			"Error": err.Error(),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "SMTP settings saved successfully",
		"Type":    "success",
	})
}

// TestSMTPConnection tests SMTP configuration with TLS/SSL support
func (h *SettingsHandler) TestSMTPConnection(c *gin.Context) {
	var config models.SMTPConfig

	// Parse form data
	config.Host = c.PostForm("smtp_host")
	config.Username = c.PostForm("smtp_username")
	config.Password = c.PostForm("smtp_password")
	config.From = c.PostForm("smtp_from")
	config.FromName = c.PostForm("smtp_from_name")
	config.To = c.PostForm("smtp_to")

	// Parse port
	if portStr := c.PostForm("smtp_port"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Validate required fields for testing (connection test doesn't need From/To, but we validate for consistency)
	if config.Host == "" || config.Port == 0 || config.Username == "" || config.Password == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "Host, Port, Username, and Password are required for testing",
			"Type":  "error",
		})
		return
	}

	// Test connection with TLS/SSL support
	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	auth := smtp.PlainAuth("", config.Username, config.Password, config.Host)

	// Determine if this is an implicit TLS port (SMTPS)
	isSSLPort := config.Port == 465 || config.Port == 8465 || config.Port == 443

	var client *smtp.Client
	var err error

	if isSSLPort {
		// Use implicit TLS (direct SSL connection)
		tlsConfig := &tls.Config{
			ServerName: config.Host,
		}

		conn, err := tls.Dial("tcp", addr, tlsConfig)
		if err != nil {
			c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
				"Error": fmt.Sprintf("Failed to connect via SSL: %v", err),
				"Type":  "error",
			})
			return
		}

		client, err = smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
				"Error": fmt.Sprintf("Failed to create SMTP client: %v", err),
				"Type":  "error",
			})
			return
		}
	} else {
		// Use STARTTLS (opportunistic TLS)
		client, err = smtp.Dial(addr)
		if err != nil {
			c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
				"Error": fmt.Sprintf("Failed to connect: %v", err),
				"Type":  "error",
			})
			return
		}

		// Upgrade to TLS
		tlsConfig := &tls.Config{
			ServerName: config.Host,
		}

		if err = client.StartTLS(tlsConfig); err != nil {
			client.Close()
			c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
				"Error": fmt.Sprintf("Failed to start TLS: %v", err),
				"Type":  "error",
			})
			return
		}
	}

	defer client.Close()

	// Try to authenticate
	if err = client.Auth(auth); err != nil {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": fmt.Sprintf("Authentication failed: %v", err),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "SMTP connection test successful!",
		"Type":    "success",
	})
}

// UpdateNotificationSetting updates a notification preference
func (h *SettingsHandler) UpdateNotificationSetting(c *gin.Context) {
	setting := c.Param("setting")

	switch setting {
	case "renewal":
		current, _ := h.service.GetBoolSetting("renewal_reminders", false)
		err := h.service.SetBoolSetting("renewal_reminders", !current)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"enabled": !current})

	case "highcost":
		current, _ := h.service.GetBoolSetting("high_cost_alerts", true)
		err := h.service.SetBoolSetting("high_cost_alerts", !current)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"enabled": !current})

	case "days":
		daysStr := c.PostForm("reminder_days")
		if days, err := strconv.Atoi(daysStr); err == nil && days > 0 && days <= 30 {
			err := h.service.SetIntSetting("reminder_days", days)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"days": days})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid days value"})
		}

	case "threshold":
		thresholdStr := c.PostForm("high_cost_threshold")
		if threshold, err := strconv.ParseFloat(thresholdStr, 64); err == nil && threshold >= 0 && threshold <= 10000 {
			err := h.service.SetFloatSetting("high_cost_threshold", threshold)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"threshold": threshold})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid threshold value (must be between 0 and 10000)"})
		}

	case "cancellation":
		current, _ := h.service.GetBoolSetting("cancellation_reminders", false)
		err := h.service.SetBoolSetting("cancellation_reminders", !current)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"enabled": !current})

	case "cancellation_days":
		daysStr := c.PostForm("cancellation_reminder_days")
		if days, err := strconv.Atoi(daysStr); err == nil && days > 0 && days <= 30 {
			err := h.service.SetIntSetting("cancellation_reminder_days", days)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"days": days})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid days value"})
		}

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown setting"})
	}
}

// GetNotificationSettings returns current notification settings
func (h *SettingsHandler) GetNotificationSettings(c *gin.Context) {
	settings := models.NotificationSettings{
		RenewalReminders:         h.service.GetBoolSettingWithDefault("renewal_reminders", false),
		HighCostAlerts:           h.service.GetBoolSettingWithDefault("high_cost_alerts", true),
		HighCostThreshold:        h.service.GetFloatSettingWithDefault("high_cost_threshold", 50.0),
		ReminderDays:             h.service.GetIntSettingWithDefault("reminder_days", 7),
		CancellationReminders:    h.service.GetBoolSettingWithDefault("cancellation_reminders", false),
		CancellationReminderDays: h.service.GetIntSettingWithDefault("cancellation_reminder_days", 7),
	}

	c.JSON(http.StatusOK, settings)
}

// GetSMTPConfig returns current SMTP configuration (without password)
func (h *SettingsHandler) GetSMTPConfig(c *gin.Context) {
	config, err := h.service.GetSMTPConfig()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"configured": false})
		return
	}

	// Don't send the password
	config.Password = ""
	c.JSON(http.StatusOK, gin.H{
		"configured": true,
		"config":     config,
	})
}

// ListAPIKeys returns all API keys
func (h *SettingsHandler) ListAPIKeys(c *gin.Context) {
	keys, err := h.service.GetAllAPIKeys()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "api-keys-list.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Don't send the actual key values for existing keys
	for i := range keys {
		if !keys[i].IsNew {
			keys[i].Key = ""
		}
	}

	c.HTML(http.StatusOK, "api-keys-list.html", gin.H{
		"Keys":         keys,
		"GoDateFormat": h.service.GetGoDateFormat(),
	})
}

// CreateAPIKey generates a new API key
func (h *SettingsHandler) CreateAPIKey(c *gin.Context) {
	name := c.PostForm("name")
	if name == "" {
		c.HTML(http.StatusBadRequest, "api-keys-list.html", gin.H{
			"Error": "API key name is required",
		})
		return
	}

	// Generate a secure random API key
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		c.HTML(http.StatusInternalServerError, "api-keys-list.html", gin.H{
			"Error": "Failed to generate API key",
		})
		return
	}

	apiKey := "sk_" + hex.EncodeToString(keyBytes)

	// Save the API key
	newKey, err := h.service.CreateAPIKey(name, apiKey)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "api-keys-list.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Get all keys including the new one
	keys, err := h.service.GetAllAPIKeys()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "api-keys-list.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Mark the new key and include its value
	for i := range keys {
		if keys[i].ID == newKey.ID {
			keys[i].IsNew = true
			keys[i].Key = apiKey
		} else {
			keys[i].Key = ""
		}
	}

	c.HTML(http.StatusOK, "api-keys-list.html", gin.H{
		"Keys":         keys,
		"GoDateFormat": h.service.GetGoDateFormat(),
	})
}

// DeleteAPIKey removes an API key
func (h *SettingsHandler) DeleteAPIKey(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.HTML(http.StatusBadRequest, "api-keys-list.html", gin.H{
			"Error": "Invalid API key ID",
		})
		return
	}

	err = h.service.DeleteAPIKey(uint(id))
	if err != nil {
		c.HTML(http.StatusInternalServerError, "api-keys-list.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Return updated list
	h.ListAPIKeys(c)
}

// UpdateCurrency updates the currency preference
func (h *SettingsHandler) UpdateCurrency(c *gin.Context) {
	currency := c.PostForm("currency")

	err := h.service.SetCurrency(currency)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"currency": currency,
		"symbol":   h.service.GetCurrencySymbol(),
	})
}

// UpdateDateFormat updates the date format preference
func (h *SettingsHandler) UpdateDateFormat(c *gin.Context) {
	format := c.PostForm("date_format")

	err := h.service.SetDateFormat(format)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"date_format": format})
}

// ToggleDarkMode toggles dark mode preference
func (h *SettingsHandler) ToggleDarkMode(c *gin.Context) {
	enabled := c.PostForm("enabled") == "true"

	err := h.service.SetDarkMode(enabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"dark_mode": enabled,
	})
}

// SetupAuth enables authentication with username and password
func (h *SettingsHandler) SetupAuth(c *gin.Context) {
	username := c.PostForm("username")
	password := c.PostForm("password")
	confirmPassword := c.PostForm("confirm_password")

	// Validate inputs
	if username == "" || password == "" {
		c.HTML(http.StatusBadRequest, "auth-message.html", gin.H{
			"Error": "Username and password are required",
			"Type":  "error",
		})
		return
	}

	if password != confirmPassword {
		c.HTML(http.StatusBadRequest, "auth-message.html", gin.H{
			"Error": "Passwords do not match",
			"Type":  "error",
		})
		return
	}

	if len(password) < 8 {
		c.HTML(http.StatusBadRequest, "auth-message.html", gin.H{
			"Error": "Password must be at least 8 characters long",
			"Type":  "error",
		})
		return
	}

	// Check if SMTP is configured (required for password reset)
	_, err := h.service.GetSMTPConfig()
	if err != nil {
		c.HTML(http.StatusBadRequest, "auth-message.html", gin.H{
			"Error": "Please configure email settings first (required for password recovery)",
			"Type":  "error",
		})
		return
	}

	// Setup authentication
	err = h.service.SetupAuth(username, password)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "auth-message.html", gin.H{
			"Error": err.Error(),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "auth-message.html", gin.H{
		"Message": "Authentication enabled successfully. You will need to login on next page load.",
		"Type":    "success",
	})
}

// DisableAuth disables authentication
func (h *SettingsHandler) DisableAuth(c *gin.Context) {
	err := h.service.DisableAuth()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "auth-message.html", gin.H{
			"Error": err.Error(),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "auth-message.html", gin.H{
		"Message": "Authentication disabled successfully",
		"Type":    "success",
	})
}

// GetAuthStatus returns the current authentication status
func (h *SettingsHandler) GetAuthStatus(c *gin.Context) {
	isEnabled := h.service.IsAuthEnabled()
	username, _ := h.service.GetAuthUsername()

	c.JSON(http.StatusOK, gin.H{
		"enabled":  isEnabled,
		"username": username,
	})
}

// GetTheme returns the current theme setting
func (h *SettingsHandler) GetTheme(c *gin.Context) {
	theme, err := h.service.GetTheme()
	if err != nil {
		// Default to 'default' theme if not set
		theme = "default"
	}

	c.JSON(http.StatusOK, gin.H{
		"theme": theme,
	})
}

// SavePushoverSettings saves Pushover configuration
func (h *SettingsHandler) SavePushoverSettings(c *gin.Context) {
	var config models.PushoverConfig

	// Parse form data
	config.UserKey = c.PostForm("pushover_user_key")
	config.AppToken = c.PostForm("pushover_app_token")

	// Validate required fields
	if config.UserKey == "" || config.AppToken == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "User Key and App Token are required",
			"Type":  "error",
		})
		return
	}

	// Save configuration
	err := h.service.SavePushoverConfig(&config)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "smtp-message.html", gin.H{
			"Error": err.Error(),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "Pushover settings saved successfully",
		"Type":    "success",
	})
}

// TestPushoverConnection tests Pushover configuration
func (h *SettingsHandler) TestPushoverConnection(c *gin.Context) {
	var config models.PushoverConfig

	// Parse form data
	config.UserKey = c.PostForm("pushover_user_key")
	config.AppToken = c.PostForm("pushover_app_token")

	// Validate required fields
	if config.UserKey == "" || config.AppToken == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "User Key and App Token are required for testing",
			"Type":  "error",
		})
		return
	}

	// Create a temporary PushoverService to test
	pushoverService := service.NewPushoverService(h.service)

	// Temporarily save config for testing
	originalConfig, _ := h.service.GetPushoverConfig()
	defer func() {
		if originalConfig != nil {
			h.service.SavePushoverConfig(originalConfig)
		} else {
			// No original config existed, so delete the test config by saving empty values
			h.service.SavePushoverConfig(&models.PushoverConfig{
				UserKey:  "",
				AppToken: "",
			})
		}
	}()

	// Save test config
	if err := h.service.SavePushoverConfig(&config); err != nil {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": fmt.Sprintf("Failed to save test config: %v", err),
			"Type":  "error",
		})
		return
	}

	// Send test notification
	err := pushoverService.SendNotification("SubTrackr Test", "This is a test notification from SubTrackr. If you received this, your Pushover configuration is working correctly!", 0)
	if err != nil {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": fmt.Sprintf("Failed to send test notification: %v", err),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "Pushover connection test successful! Check your device for the test notification.",
		"Type":    "success",
	})
}

// SaveWebhookSettings saves Webhook configuration
func (h *SettingsHandler) SaveWebhookSettings(c *gin.Context) {
	var config models.WebhookConfig
	config.URL = c.PostForm("webhook_url")

	if config.URL == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "Webhook URL is required",
			"Type":  "error",
		})
		return
	}

	// Parse headers from textarea (Key: Value format, one per line)
	headersRaw := c.PostForm("webhook_headers")
	headers := make(map[string]string)
	for _, line := range splitLines(headersRaw) {
		line = trimSpace(line)
		if line == "" {
			continue
		}
		parts := splitN(line, ":", 2)
		if len(parts) == 2 {
			headers[trimSpace(parts[0])] = trimSpace(parts[1])
		}
	}
	config.Headers = headers

	err := h.service.SaveWebhookConfig(&config)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "smtp-message.html", gin.H{
			"Error": err.Error(),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "Webhook settings saved successfully",
		"Type":    "success",
	})
}

// TestWebhookConnection tests Webhook configuration
func (h *SettingsHandler) TestWebhookConnection(c *gin.Context) {
	webhookURL := c.PostForm("webhook_url")
	if webhookURL == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "Webhook URL is required for testing",
			"Type":  "error",
		})
		return
	}

	// Parse headers
	headersRaw := c.PostForm("webhook_headers")
	headers := make(map[string]string)
	for _, line := range splitLines(headersRaw) {
		line = trimSpace(line)
		if line == "" {
			continue
		}
		parts := splitN(line, ":", 2)
		if len(parts) == 2 {
			headers[trimSpace(parts[0])] = trimSpace(parts[1])
		}
	}

	testConfig := &models.WebhookConfig{URL: webhookURL, Headers: headers}

	// Temporarily save config for testing
	originalConfig, _ := h.service.GetWebhookConfig()
	defer func() {
		if originalConfig != nil {
			h.service.SaveWebhookConfig(originalConfig)
		} else {
			h.service.SaveWebhookConfig(&models.WebhookConfig{})
		}
	}()

	if err := h.service.SaveWebhookConfig(testConfig); err != nil {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": fmt.Sprintf("Failed to save test config: %v", err),
			"Type":  "error",
		})
		return
	}

	webhookService := service.NewWebhookService(h.service)
	payload := &service.WebhookPayload{
		Event:     "test",
		Title:     "SubTrackr Test",
		Message:   "This is a test notification from SubTrackr. If you received this, your webhook configuration is working correctly!",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	err := webhookService.SendWebhook(payload)
	if err != nil {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": fmt.Sprintf("Webhook test failed: %v", err),
			"Type":  "error",
		})
		return
	}

	c.HTML(http.StatusOK, "smtp-message.html", gin.H{
		"Message": "Webhook test successful! Check your endpoint for the test payload.",
		"Type":    "success",
	})
}

// GetPushoverConfig returns current Pushover configuration (without sensitive data)
func (h *SettingsHandler) GetPushoverConfig(c *gin.Context) {
	config, err := h.service.GetPushoverConfig()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"configured": false})
		return
	}

	// Don't send the full token, just indicate if configured
	c.JSON(http.StatusOK, gin.H{
		"configured":    true,
		"has_user_key":  config.UserKey != "",
		"has_app_token": config.AppToken != "",
	})
}

// ToggleICalSubscription toggles iCal subscription on/off
func (h *SettingsHandler) ToggleICalSubscription(c *gin.Context) {
	current := h.service.IsICalSubscriptionEnabled()
	newState := !current

	if err := h.service.SetICalSubscriptionEnabled(newState); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var url string
	if newState {
		token, err := h.service.GetOrGenerateICalToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		url = buildBaseURL(c, h.service.GetBaseURL()) + "/ical/" + token
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled": newState,
		"url":     url,
	})
}

// GetICalSubscriptionURL returns the current iCal subscription status and URL
func (h *SettingsHandler) GetICalSubscriptionURL(c *gin.Context) {
	enabled := h.service.IsICalSubscriptionEnabled()
	var url string
	if enabled {
		token, err := h.service.GetOrGenerateICalToken()
		if err == nil {
			url = buildBaseURL(c, h.service.GetBaseURL()) + "/ical/" + token
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled,
		"url":     url,
	})
}

// RegenerateICalToken generates a new iCal subscription token
func (h *SettingsHandler) RegenerateICalToken(c *gin.Context) {
	token, err := h.service.RegenerateICalToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	url := buildBaseURL(c, h.service.GetBaseURL()) + "/ical/" + token

	c.JSON(http.StatusOK, gin.H{
		"url": url,
	})
}

// UpdateBaseURL saves the base URL setting
func (h *SettingsHandler) UpdateBaseURL(c *gin.Context) {
	baseURL := c.PostForm("base_url")

	if err := h.service.SetBaseURL(baseURL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"base_url": baseURL,
	})
}

// SetTheme saves the theme preference
func (h *SettingsHandler) SetTheme(c *gin.Context) {
	var req struct {
		Theme string `json:"theme" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid request",
		})
		return
	}

	// Validate theme name
	validThemes := map[string]bool{
		"default":   true,
		"dark":      true,
		"christmas": true,
		"midnight":  true,
		"ocean":     true,
	}

	if !validThemes[req.Theme] {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid theme name",
		})
		return
	}

	if err := h.service.SetTheme(req.Theme); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to save theme",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"theme":   req.Theme,
	})
}

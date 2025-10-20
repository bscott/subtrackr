package handlers

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/smtp"
	"strconv"
	"subtrackr/internal/models"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

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

	// Parse port
	if portStr := c.PostForm("smtp_port"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Validate required fields
	if config.Host == "" || config.Port == 0 || config.Username == "" || config.Password == "" || config.From == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "All SMTP fields are required",
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

	// Parse port
	if portStr := c.PostForm("smtp_port"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			config.Port = port
		}
	}

	// Validate
	if config.Host == "" || config.Port == 0 || config.Username == "" || config.Password == "" {
		c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
			"Error": "All SMTP fields are required for testing",
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

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown setting"})
	}
}

// GetNotificationSettings returns current notification settings
func (h *SettingsHandler) GetNotificationSettings(c *gin.Context) {
	settings := models.NotificationSettings{
		RenewalReminders: h.service.GetBoolSettingWithDefault("renewal_reminders", false),
		HighCostAlerts:   h.service.GetBoolSettingWithDefault("high_cost_alerts", true),
		ReminderDays:     h.service.GetIntSettingWithDefault("reminder_days", 7),
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
		"Keys": keys,
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
		"Keys": keys,
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

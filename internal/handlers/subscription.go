package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"subtrackr/internal/models"
	"subtrackr/internal/service"
	"time"

	"github.com/gin-gonic/gin"
)

type SubscriptionHandler struct {
	service         *service.SubscriptionService
	settingsService *service.SettingsService
}

func NewSubscriptionHandler(service *service.SubscriptionService, settingsService *service.SettingsService) *SubscriptionHandler {
	return &SubscriptionHandler{
		service:         service,
		settingsService: settingsService,
	}
}

// Dashboard renders the main dashboard page
func (h *SubscriptionHandler) Dashboard(c *gin.Context) {
	stats, err := h.service.GetStats()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	// Get recent subscriptions for the list
	recentSubs := subscriptions
	if len(subscriptions) > 5 {
		recentSubs = subscriptions[:5]
	}

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"Title":          "Dashboard",
		"CurrentPage":    "dashboard",
		"Stats":          stats,
		"Subscriptions":  recentSubs,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
	})
}

// SubscriptionsList renders the subscriptions list page
func (h *SubscriptionHandler) SubscriptionsList(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.HTML(http.StatusOK, "subscriptions.html", gin.H{
		"Title":          "Subscriptions",
		"CurrentPage":    "subscriptions",
		"Subscriptions":  subscriptions,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
	})
}

// Analytics renders the analytics page
func (h *SubscriptionHandler) Analytics(c *gin.Context) {
	stats, err := h.service.GetStats()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	c.HTML(http.StatusOK, "analytics.html", gin.H{
		"Title":          "Analytics",
		"CurrentPage":    "analytics",
		"Stats":          stats,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
	})
}

// Settings renders the settings page
func (h *SubscriptionHandler) Settings(c *gin.Context) {
	c.HTML(http.StatusOK, "settings.html", gin.H{
		"Title":            "Settings",
		"CurrentPage":      "settings",
		"Currency":         h.settingsService.GetCurrency(),
		"CurrencySymbol":   h.settingsService.GetCurrencySymbol(),
		"RenewalReminders": h.settingsService.GetBoolSettingWithDefault("renewal_reminders", false),
		"HighCostAlerts":   h.settingsService.GetBoolSettingWithDefault("high_cost_alerts", true),
		"ReminderDays":     h.settingsService.GetIntSettingWithDefault("reminder_days", 7),
	})
}

// API endpoints for HTMX

// GetSubscriptions returns subscriptions as HTML fragments
func (h *SubscriptionHandler) GetSubscriptions(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.HTML(http.StatusOK, "subscription-list.html", gin.H{
		"Subscriptions":  subscriptions,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
	})
}

// CreateSubscription handles creating a new subscription
func (h *SubscriptionHandler) CreateSubscription(c *gin.Context) {
	var subscription models.Subscription

	// Parse form data
	subscription.Name = c.PostForm("name")
	// Parse category_id as uint
	if categoryIDStr := c.PostForm("category_id"); categoryIDStr != "" {
		if categoryID, err := strconv.ParseUint(categoryIDStr, 10, 32); err == nil {
			subscription.CategoryID = uint(categoryID)
		}
	}
	subscription.Schedule = c.PostForm("schedule")
	subscription.Status = c.PostForm("status")
	subscription.PaymentMethod = c.PostForm("payment_method")
	subscription.Account = c.PostForm("account")
	subscription.URL = c.PostForm("url")
	subscription.Notes = c.PostForm("notes")
	subscription.Usage = c.PostForm("usage")

	// Parse cost
	if costStr := c.PostForm("cost"); costStr != "" {
		if cost, err := strconv.ParseFloat(costStr, 64); err == nil {
			subscription.Cost = cost
		}
	}

	// Parse dates
	if startDateStr := c.PostForm("start_date"); startDateStr != "" {
		if startDate, err := time.Parse("2006-01-02", startDateStr); err == nil {
			subscription.StartDate = &startDate
		}
	}

	if renewalDateStr := c.PostForm("renewal_date"); renewalDateStr != "" {
		if renewalDate, err := time.Parse("2006-01-02", renewalDateStr); err == nil {
			subscription.RenewalDate = &renewalDate
		}
	}

	if cancellationDateStr := c.PostForm("cancellation_date"); cancellationDateStr != "" {
		if cancellationDate, err := time.Parse("2006-01-02", cancellationDateStr); err == nil {
			subscription.CancellationDate = &cancellationDate
		}
	}

	// Create subscription
	created, err := h.service.Create(&subscription)
	if err != nil {
		if c.GetHeader("HX-Request") != "" {
			c.Header("HX-Retarget", "#form-errors")
			c.HTML(http.StatusBadRequest, "form-errors.html", gin.H{
				"Error": err.Error(),
			})
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	if c.GetHeader("HX-Request") != "" {
		c.Header("HX-Refresh", "true")
		c.Status(http.StatusCreated)
	} else {
		c.JSON(http.StatusCreated, created)
	}
}

// GetSubscription returns a single subscription
func (h *SubscriptionHandler) GetSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	subscription, err := h.service.GetByID(uint(id))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription not found"})
		return
	}

	c.JSON(http.StatusOK, subscription)
}

// UpdateSubscription handles updating an existing subscription
func (h *SubscriptionHandler) UpdateSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	var subscription models.Subscription

	// Parse form data (similar to CreateSubscription)
	subscription.Name = c.PostForm("name")
	// Parse category_id as uint
	if categoryIDStr := c.PostForm("category_id"); categoryIDStr != "" {
		if categoryID, err := strconv.ParseUint(categoryIDStr, 10, 32); err == nil {
			subscription.CategoryID = uint(categoryID)
		}
	}
	subscription.Schedule = c.PostForm("schedule")
	subscription.Status = c.PostForm("status")
	subscription.PaymentMethod = c.PostForm("payment_method")
	subscription.Account = c.PostForm("account")
	subscription.URL = c.PostForm("url")
	subscription.Notes = c.PostForm("notes")
	subscription.Usage = c.PostForm("usage")

	// Parse cost
	if costStr := c.PostForm("cost"); costStr != "" {
		if cost, err := strconv.ParseFloat(costStr, 64); err == nil {
			subscription.Cost = cost
		}
	}

	// Update subscription
	_, err = h.service.Update(uint(id), &subscription)
	if err != nil {
		c.Header("HX-Retarget", "#form-errors")
		c.HTML(http.StatusBadRequest, "form-errors.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Return success response that triggers a page refresh
	c.Header("HX-Refresh", "true")
	c.Status(http.StatusOK)
}

// DeleteSubscription handles deleting a subscription
func (h *SubscriptionHandler) DeleteSubscription(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid ID"})
		return
	}

	err = h.service.Delete(uint(id))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Return success response that triggers a page refresh
	c.Header("HX-Refresh", "true")
	c.Status(http.StatusOK)
}

// GetStats returns current statistics
func (h *SubscriptionHandler) GetStats(c *gin.Context) {
	stats, err := h.service.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// GetSubscriptionForm returns the subscription form (for add/edit)
func (h *SubscriptionHandler) GetSubscriptionForm(c *gin.Context) {
	var subscription *models.Subscription
	isEdit := false

	// Check if this is an edit form
	if idStr := c.Param("id"); idStr != "" {
		id, err := strconv.ParseUint(idStr, 10, 32)
		if err == nil {
			sub, err := h.service.GetByID(uint(id))
			if err == nil {
				subscription = sub
				isEdit = true
			}
		}
	}

	categories, err := h.service.GetAllCategories()
	if err != nil {
		categories = []models.Category{}
	}

	c.HTML(http.StatusOK, "subscription-form.html", gin.H{
		"Subscription":   subscription,
		"IsEdit":         isEdit,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
		"Categories":     categories,
	})
}

// ExportCSV exports all subscriptions as CSV
func (h *SubscriptionHandler) ExportCSV(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", "attachment; filename=subscriptions.csv")

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// Write CSV header
	header := []string{"ID", "Name", "Category", "Cost", "Schedule", "Status", "Payment Method", "Account", "Start Date", "Renewal Date", "Cancellation Date", "URL", "Notes", "Usage", "Created At"}
	writer.Write(header)

	// Write subscription data
	for _, sub := range subscriptions {
		categoryName := ""
		if sub.Category.Name != "" {
			categoryName = sub.Category.Name
		}
		record := []string{
			fmt.Sprintf("%d", sub.ID),
			sub.Name,
			categoryName,
			fmt.Sprintf("%.2f", sub.Cost),
			sub.Schedule,
			sub.Status,
			sub.PaymentMethod,
			sub.Account,
			formatDate(sub.StartDate),
			formatDate(sub.RenewalDate),
			formatDate(sub.CancellationDate),
			sub.URL,
			sub.Notes,
			sub.Usage,
			sub.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		writer.Write(record)
	}
}

// ExportJSON exports all subscriptions as JSON
func (h *SubscriptionHandler) ExportJSON(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=subscriptions.json")

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": subscriptions,
		"exported_at":   time.Now(),
		"total_count":   len(subscriptions),
	})
}

// BackupData creates a complete backup of all data
func (h *SubscriptionHandler) BackupData(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	stats, err := h.service.GetStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	backup := gin.H{
		"version":       "1.0",
		"backup_date":   time.Now(),
		"subscriptions": subscriptions,
		"stats":         stats,
		"total_count":   len(subscriptions),
	}

	c.Header("Content-Type", "application/json")
	c.Header("Content-Disposition", "attachment; filename=subtrackr-backup.json")
	c.JSON(http.StatusOK, backup)
}

// ClearAllData removes all subscription data
func (h *SubscriptionHandler) ClearAllData(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Delete all subscriptions
	for _, sub := range subscriptions {
		err := h.service.Delete(sub.ID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to delete subscription %d: %v", sub.ID, err)})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "All subscription data has been cleared",
		"deleted_count": len(subscriptions),
	})
}

// Helper function to format currency
func formatCurrency(amount float64) string {
	return fmt.Sprintf("$%.2f", amount)
}

// Helper function to format date pointers
func formatDate(date *time.Time) string {
	if date == nil {
		return ""
	}
	return date.Format("2006-01-02")
}

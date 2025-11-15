package handlers

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"subtrackr/internal/models"
	"subtrackr/internal/service"
	"subtrackr/internal/version"
	"time"

	"github.com/gin-gonic/gin"
)

// SubscriptionWithConversion represents a subscription with currency conversion info
type SubscriptionWithConversion struct {
	*models.Subscription
	ConvertedCost         float64 `json:"converted_cost"`
	ConvertedAnnualCost   float64 `json:"converted_annual_cost"`
	ConvertedMonthlyCost  float64 `json:"converted_monthly_cost"`
	DisplayCurrency       string  `json:"display_currency"`
	DisplayCurrencySymbol string  `json:"display_currency_symbol"`
	ShowConversion        bool    `json:"show_conversion"`
}

type SubscriptionHandler struct {
	service         *service.SubscriptionService
	settingsService *service.SettingsService
	currencyService *service.CurrencyService
	emailService    *service.EmailService
	logoService     *service.LogoService
}

func NewSubscriptionHandler(service *service.SubscriptionService, settingsService *service.SettingsService, currencyService *service.CurrencyService, emailService *service.EmailService, logoService *service.LogoService) *SubscriptionHandler {
	return &SubscriptionHandler{
		service:         service,
		settingsService: settingsService,
		currencyService: currencyService,
		emailService:    emailService,
		logoService:     logoService,
	}
}

// enrichWithCurrencyConversion adds currency conversion info to subscriptions
func (h *SubscriptionHandler) enrichWithCurrencyConversion(subscriptions []models.Subscription) []SubscriptionWithConversion {
	displayCurrency := h.settingsService.GetCurrency()
	displaySymbol := h.settingsService.GetCurrencySymbol()

	result := make([]SubscriptionWithConversion, len(subscriptions))

	for i := range subscriptions {
		// Create a copy of the subscription for modification; this pattern is correct for Go 1.22+
		sub := subscriptions[i]
		enriched := SubscriptionWithConversion{
			Subscription:          &sub,
			DisplayCurrency:       displayCurrency,
			DisplayCurrencySymbol: displaySymbol,
			ShowConversion:        false,
		}

		// Only show conversion if currency service is enabled and currencies differ
		if h.currencyService.IsEnabled() && sub.OriginalCurrency != "" && sub.OriginalCurrency != displayCurrency {
			if convertedCost, err := h.currencyService.ConvertAmount(sub.Cost, sub.OriginalCurrency, displayCurrency); err == nil {
				enriched.ConvertedCost = convertedCost
				enriched.ConvertedAnnualCost = convertedCost * h.getScheduleMultiplier(sub.Schedule)
				enriched.ConvertedMonthlyCost = enriched.ConvertedAnnualCost / 12
				enriched.ShowConversion = true
			}
		} else {
			// Same currency or no conversion needed
			enriched.ConvertedCost = sub.Cost
			enriched.ConvertedAnnualCost = sub.AnnualCost()
			enriched.ConvertedMonthlyCost = sub.MonthlyCost()
		}

		result[i] = enriched
	}

	return result
}

// isHighCostWithCurrency checks if a subscription is high-cost, respecting currency conversion
// The threshold is in the user's display currency, so we convert the subscription's monthly cost
// to the display currency before comparing
func (h *SubscriptionHandler) isHighCostWithCurrency(subscription *models.Subscription) bool {
	threshold := h.settingsService.GetFloatSettingWithDefault("high_cost_threshold", 50.0)
	displayCurrency := h.settingsService.GetCurrency()
	
	// Get monthly cost in subscription's original currency
	monthlyCost := subscription.MonthlyCost()
	
	// If currencies match or conversion is disabled, compare directly
	if subscription.OriginalCurrency == displayCurrency || !h.currencyService.IsEnabled() {
		return monthlyCost > threshold
	}
	
	// Convert monthly cost to display currency
	convertedMonthlyCost, err := h.currencyService.ConvertAmount(monthlyCost, subscription.OriginalCurrency, displayCurrency)
	if err != nil {
		// If conversion fails, fall back to direct comparison (better than failing silently)
		log.Printf("Warning: Failed to convert currency for high-cost check: %v", err)
		return monthlyCost > threshold
	}
	
	// Compare converted monthly cost against threshold
	return convertedMonthlyCost > threshold
}

// fetchAndSetLogo fetches a logo for a subscription if URL is provided and icon_url is empty
// This is a helper method to avoid code duplication between create and update handlers
func (h *SubscriptionHandler) fetchAndSetLogo(subscription *models.Subscription) {
	if subscription.URL == "" || subscription.IconURL != "" {
		return
	}

	iconURL, err := h.logoService.FetchLogoFromURL(subscription.URL)
	if err == nil && iconURL != "" {
		subscription.IconURL = iconURL
		log.Printf("Fetched logo: %s -> %s", subscription.URL, iconURL)
	} else if err != nil {
		log.Printf("Failed to fetch logo for URL %s: %v", subscription.URL, err)
	}
}

// getScheduleMultiplier returns the annual multiplier for a schedule
func (h *SubscriptionHandler) getScheduleMultiplier(schedule string) float64 {
	switch schedule {
	case "Annual":
		return 1
	case "Quarterly":
		return 4
	case "Monthly":
		return 12
	case "Weekly":
		return 52
	case "Daily":
		return 365
	default:
		return 12
	}
}

// parseDatePtr parses a date string in "2006-01-02" format and returns a pointer to time.Time.
// Returns nil if the string is empty or if parsing fails.
// Logs parsing errors for debugging purposes.
func parseDatePtr(dateStr string) *time.Time {
	if dateStr == "" {
		return nil
	}
	if date, err := time.Parse("2006-01-02", dateStr); err == nil {
		return &date
	}
	// Log parsing errors for debugging (invalid date format from form)
	log.Printf("Failed to parse date string '%s': expected format YYYY-MM-DD", dateStr)
	return nil
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

	// Enrich with currency conversion
	enrichedSubs := h.enrichWithCurrencyConversion(subscriptions)

	c.HTML(http.StatusOK, "dashboard.html", gin.H{
		"Title":          "Dashboard",
		"CurrentPage":    "dashboard",
		"Stats":          stats,
		"Subscriptions":  enrichedSubs,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
		"DarkMode":       h.settingsService.IsDarkModeEnabled(),
	})
}

// SubscriptionsList renders the subscriptions list page
func (h *SubscriptionHandler) SubscriptionsList(c *gin.Context) {
	// Get sort parameters from query string
	sortBy := c.DefaultQuery("sort", "created_at")
	order := c.DefaultQuery("order", "desc")

	// Get sorted subscriptions
	subscriptions, err := h.service.GetAllSorted(sortBy, order)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	// Enrich with currency conversion
	enrichedSubs := h.enrichWithCurrencyConversion(subscriptions)

	c.HTML(http.StatusOK, "subscriptions.html", gin.H{
		"Title":          "Subscriptions",
		"CurrentPage":    "subscriptions",
		"Subscriptions":  enrichedSubs,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
		"DarkMode":       h.settingsService.IsDarkModeEnabled(),
		"SortBy":         sortBy,
		"Order":          order,
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
		"DarkMode":       h.settingsService.IsDarkModeEnabled(),
	})
}

// Calendar renders the calendar page with subscription renewal dates
func (h *SubscriptionHandler) Calendar(c *gin.Context) {
	// Get all subscriptions with renewal dates
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "error.html", gin.H{"error": err.Error()})
		return
	}

	// Filter subscriptions with renewal dates and group by date
	// Create a simplified structure for JavaScript
	type Event struct {
		Name    string  `json:"name"`
		Cost    float64 `json:"cost"`
		ID      uint    `json:"id"`
		IconURL string  `json:"icon_url"`
	}
	eventsByDate := make(map[string][]Event)
	for _, sub := range subscriptions {
		if sub.RenewalDate != nil && sub.Status == "Active" {
			dateKey := sub.RenewalDate.Format("2006-01-02")
			eventsByDate[dateKey] = append(eventsByDate[dateKey], Event{
				Name:    sub.Name,
				Cost:    sub.Cost,
				ID:      sub.ID,
				IconURL: sub.IconURL,
			})
		}
	}

	// Get current month/year or from query params
	now := time.Now()
	year := now.Year()
	month := int(now.Month())

	if y := c.Query("year"); y != "" {
		if yInt, err := strconv.Atoi(y); err == nil {
			year = yInt
		}
	}
	if m := c.Query("month"); m != "" {
		if mInt, err := strconv.Atoi(m); err == nil {
			month = mInt
		}
	}

	// Validate month range
	if month < 1 {
		month = 1
	}
	if month > 12 {
		month = 12
	}

	// Calculate previous and next month
	firstOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	prevMonth := firstOfMonth.AddDate(0, -1, 0)
	nextMonth := firstOfMonth.AddDate(0, 1, 0)

	// Serialize events to JSON for JavaScript
	eventsJSON, _ := json.Marshal(eventsByDate)

	// Prevent caching to ensure calendar updates when navigating months
	c.Header("Cache-Control", "no-cache, no-store, must-revalidate")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")

	c.HTML(http.StatusOK, "calendar.html", gin.H{
		"Title":          "Calendar",
		"CurrentPage":    "calendar",
		"Year":           year,
		"Month":          month,
		"MonthName":      firstOfMonth.Format("January 2006"),
		"EventsByDate":   template.JS(string(eventsJSON)),
		"FirstOfMonth":   firstOfMonth,
		"PrevMonth":      prevMonth,
		"NextMonth":      nextMonth,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
		"DarkMode":       h.settingsService.IsDarkModeEnabled(),
	})
}

// ExportICal generates and downloads an iCal file with all subscription renewal dates
func (h *SubscriptionHandler) ExportICal(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Generate iCal content
	icalContent := "BEGIN:VCALENDAR\r\n"
	icalContent += "VERSION:2.0\r\n"
	icalContent += "PRODID:-//SubTrackr//Subscription Renewals//EN\r\n"
	icalContent += "CALSCALE:GREGORIAN\r\n"
	icalContent += "METHOD:PUBLISH\r\n"

	now := time.Now()
	for _, sub := range subscriptions {
		if sub.RenewalDate != nil && sub.Status == "Active" {
			// Format dates in iCal format (YYYYMMDDTHHMMSSZ)
			dtStart := sub.RenewalDate.Format("20060102T150000Z")
			dtEnd := sub.RenewalDate.Add(1 * time.Hour).Format("20060102T150000Z")
			dtStamp := now.Format("20060102T150000Z")
			uid := fmt.Sprintf("subtrackr-%d-%d@subtrackr", sub.ID, sub.RenewalDate.Unix())

			// Escape text for iCal (simplified - should escape commas, semicolons, etc.)
			summary := fmt.Sprintf("%s Renewal", sub.Name)
			description := fmt.Sprintf("Subscription: %s\\nCost: %s%.2f\\nSchedule: %s", sub.Name, h.settingsService.GetCurrencySymbol(), sub.Cost, sub.Schedule)
			if sub.URL != "" {
				description += fmt.Sprintf("\\nURL: %s", sub.URL)
			}

			icalContent += "BEGIN:VEVENT\r\n"
			icalContent += fmt.Sprintf("UID:%s\r\n", uid)
			icalContent += fmt.Sprintf("DTSTAMP:%s\r\n", dtStamp)
			icalContent += fmt.Sprintf("DTSTART:%s\r\n", dtStart)
			icalContent += fmt.Sprintf("DTEND:%s\r\n", dtEnd)
			icalContent += fmt.Sprintf("SUMMARY:%s\r\n", summary)
			icalContent += fmt.Sprintf("DESCRIPTION:%s\r\n", description)
			icalContent += "STATUS:CONFIRMED\r\n"
			icalContent += "SEQUENCE:0\r\n"

			// Add recurrence rule based on schedule
			switch sub.Schedule {
			case "Daily":
				icalContent += "RRULE:FREQ=DAILY;INTERVAL=1\r\n"
			case "Weekly":
				icalContent += "RRULE:FREQ=WEEKLY;INTERVAL=1\r\n"
			case "Monthly":
				icalContent += "RRULE:FREQ=MONTHLY;INTERVAL=1\r\n"
			case "Quarterly":
				icalContent += "RRULE:FREQ=MONTHLY;INTERVAL=3\r\n"
			case "Annual":
				icalContent += "RRULE:FREQ=YEARLY;INTERVAL=1\r\n"
			}

			icalContent += "END:VEVENT\r\n"
		}
	}

	icalContent += "END:VCALENDAR\r\n"

	// Set headers for file download
	c.Header("Content-Type", "text/calendar; charset=utf-8")
	c.Header("Content-Disposition", `attachment; filename="subtrackr-renewals.ics"`)
	c.Data(http.StatusOK, "text/calendar; charset=utf-8", []byte(icalContent))
}

// Settings renders the settings page
func (h *SubscriptionHandler) Settings(c *gin.Context) {
	// Load SMTP config if available (without password)
	var smtpConfig *models.SMTPConfig
	config, err := h.settingsService.GetSMTPConfig()
	if err == nil && config != nil {
		// Don't include password in template
		config.Password = ""
		smtpConfig = config
	}

	c.HTML(http.StatusOK, "settings.html", gin.H{
		"Title":              "Settings",
		"CurrentPage":        "settings",
		"Currency":           h.settingsService.GetCurrency(),
		"CurrencySymbol":     h.settingsService.GetCurrencySymbol(),
		"RenewalReminders":   h.settingsService.GetBoolSettingWithDefault("renewal_reminders", false),
		"HighCostAlerts":     h.settingsService.GetBoolSettingWithDefault("high_cost_alerts", true),
		"HighCostThreshold":  h.settingsService.GetFloatSettingWithDefault("high_cost_threshold", 50.0),
		"ReminderDays":       h.settingsService.GetIntSettingWithDefault("reminder_days", 7),
		"DarkMode":           h.settingsService.IsDarkModeEnabled(),
		"Version":            version.GetVersion(),
		"SMTPConfig":         smtpConfig,
	})
}

// API endpoints for HTMX

// GetSubscriptions returns subscriptions as HTML fragments
func (h *SubscriptionHandler) GetSubscriptions(c *gin.Context) {
	// Get sort parameters from query string
	sortBy := c.DefaultQuery("sort", "created_at")
	order := c.DefaultQuery("order", "desc")

	// Get sorted subscriptions
	subscriptions, err := h.service.GetAllSorted(sortBy, order)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Enrich with currency conversion
	enrichedSubs := h.enrichWithCurrencyConversion(subscriptions)

	c.HTML(http.StatusOK, "subscription-list.html", gin.H{
		"Subscriptions":  enrichedSubs,
		"CurrencySymbol": h.settingsService.GetCurrencySymbol(),
		"SortBy":         sortBy,
		"Order":          order,
	})
}

// GetSubscriptionsAPI returns subscriptions as JSON for API calls
func (h *SubscriptionHandler) GetSubscriptionsAPI(c *gin.Context) {
	subscriptions, err := h.service.GetAll()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, subscriptions)
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
	subscription.OriginalCurrency = c.PostForm("original_currency")
	if subscription.OriginalCurrency == "" {
		subscription.OriginalCurrency = "USD" // Default to USD
	}
	subscription.PaymentMethod = c.PostForm("payment_method")
	subscription.Account = c.PostForm("account")
	subscription.URL = c.PostForm("url")
	subscription.IconURL = c.PostForm("icon_url") // Allow manual icon URL override
	subscription.Notes = c.PostForm("notes")
	subscription.Usage = c.PostForm("usage")

	// Parse cost
	if costStr := c.PostForm("cost"); costStr != "" {
		if cost, err := strconv.ParseFloat(costStr, 64); err == nil {
			subscription.Cost = cost
		}
	}

	// Parse dates using helper function
	subscription.StartDate = parseDatePtr(c.PostForm("start_date"))
	subscription.RenewalDate = parseDatePtr(c.PostForm("renewal_date"))
	subscription.CancellationDate = parseDatePtr(c.PostForm("cancellation_date"))

	// Fetch logo synchronously before creation if URL is provided and icon_url is empty
	h.fetchAndSetLogo(&subscription)

	// Create subscription
	created, err := h.service.Create(&subscription)
	if err != nil {
		// Log the error for debugging
		log.Printf("Failed to create subscription: %v", err)
		log.Printf("Subscription data: Name=%s, CategoryID=%d, Status=%s, Schedule=%s", 
			subscription.Name, subscription.CategoryID, subscription.Status, subscription.Schedule)
		
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

	// Send high-cost alert email if applicable
	if h.isHighCostWithCurrency(created) {
		// Reload subscription with category for email template
		subscriptionWithCategory, err := h.service.GetByID(created.ID)
		if err == nil && subscriptionWithCategory != nil {
			if err := h.emailService.SendHighCostAlert(subscriptionWithCategory); err != nil {
				// Log error but don't fail the request
				log.Printf("Failed to send high-cost alert email: %v", err)
			}
		}
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
	subscription.OriginalCurrency = c.PostForm("original_currency")
	if subscription.OriginalCurrency == "" {
		subscription.OriginalCurrency = "USD" // Default to USD
	}
	subscription.PaymentMethod = c.PostForm("payment_method")
	subscription.Account = c.PostForm("account")
	subscription.URL = c.PostForm("url")
	subscription.IconURL = c.PostForm("icon_url") // Allow manual icon URL override
	subscription.Notes = c.PostForm("notes")
	subscription.Usage = c.PostForm("usage")

	// Parse cost
	if costStr := c.PostForm("cost"); costStr != "" {
		if cost, err := strconv.ParseFloat(costStr, 64); err == nil {
			subscription.Cost = cost
		}
	}

	// Parse dates using helper function
	// Always parse renewal date if provided; let service/model layer handle schedule change logic
	subscription.StartDate = parseDatePtr(c.PostForm("start_date"))
	subscription.RenewalDate = parseDatePtr(c.PostForm("renewal_date"))
	subscription.CancellationDate = parseDatePtr(c.PostForm("cancellation_date"))

	// Get the original subscription to check if it was high-cost before update
	original, _ := h.service.GetByID(uint(id))
	wasHighCost := original != nil && h.isHighCostWithCurrency(original)

	// Preserve existing IconURL if not explicitly set in form
	if subscription.IconURL == "" && original != nil {
		subscription.IconURL = original.IconURL
	}

	// Check if URL changed - if so, we should fetch a new logo
	urlChanged := original != nil && original.URL != subscription.URL
	if urlChanged || (subscription.URL != "" && subscription.IconURL == "") {
		h.fetchAndSetLogo(&subscription)
	}

	// Update subscription
	updated, err := h.service.Update(uint(id), &subscription)
	if err != nil {
		c.Header("HX-Retarget", "#form-errors")
		c.HTML(http.StatusBadRequest, "form-errors.html", gin.H{
			"Error": err.Error(),
		})
		return
	}

	// Send high-cost alert email if subscription became high-cost (wasn't before, but is now)
	if updated != nil && !wasHighCost && h.isHighCostWithCurrency(updated) {
		// Reload subscription with category for email template
		subscriptionWithCategory, err := h.service.GetByID(updated.ID)
		if err == nil && subscriptionWithCategory != nil {
			if err := h.emailService.SendHighCostAlert(subscriptionWithCategory); err != nil {
				// Log error but don't fail the request
				log.Printf("Failed to send high-cost alert email: %v", err)
			}
		}
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

package main

import (
	"crypto/subtle"
	"flag"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"strings"
	"subtrackr/internal/config"
	"subtrackr/internal/database"
	"subtrackr/internal/handlers"
	"subtrackr/internal/i18n"
	"subtrackr/internal/middleware"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/term"
)

func main() {
	// CLI flags
	resetPassword := flag.Bool("reset-password", false, "Reset admin password (interactive or with --new-password)")
	newPassword := flag.String("new-password", "", "New password for admin (non-interactive, use with --reset-password)")
	disableAuth := flag.Bool("disable-auth", false, "Disable authentication and remove credentials")
	flag.Parse()

	// Load configuration
	cfg := config.Load()

	// Initialize database
	db, err := database.Initialize(cfg.DatabasePath)
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}

	// Run database migrations
	err = database.RunMigrations(db)
	if err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// Initialize repositories
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	exchangeRateRepo := repository.NewExchangeRateRepository(db)
	tagRepo := repository.NewTagRepository(db)

	// Initialize services
	categoryService := service.NewCategoryService(categoryRepo)
	currencyService := service.NewCurrencyService(exchangeRateRepo, settingsRepo)
	subscriptionService := service.NewSubscriptionService(subscriptionRepo, categoryService)
	tagService := service.NewTagService(tagRepo)
	settingsService := service.NewSettingsService(settingsRepo)
	emailService := service.NewEmailService(settingsService)
	pushoverService := service.NewPushoverService(settingsService)
	webhookService := service.NewWebhookService(settingsService)
	telegramService := service.NewTelegramService(settingsService)
	logoService := service.NewLogoService()

	// Handle CLI commands (run before starting HTTP server)
	if *disableAuth {
		handleDisableAuth(settingsService)
		return
	}

	if *resetPassword {
		handleResetPassword(settingsService, *newPassword)
		return
	}

	// Initialize session service (get or generate session secret)
	sessionSecret, err := settingsService.GetOrGenerateSessionSecret()
	if err != nil {
		log.Fatal("Failed to initialize session secret:", err)
	}
	sessionService := service.NewSessionService(sessionSecret)

	// Load translation catalog (UI strings). Failure is non-fatal: keys fall back to themselves.
	i18nCatalog := i18n.NewCatalog()
	if err := i18nCatalog.LoadDir("web/locales"); err != nil {
		log.Printf("Warning: i18n catalog could not be loaded (%v) - UI will render English fallbacks", err)
	} else {
		log.Printf("Loaded UI translations for %d language(s)", len(i18nCatalog.AvailableLanguages()))
	}

	// Initialize handlers
	subscriptionHandler := handlers.NewSubscriptionHandler(subscriptionService, settingsService, currencyService, emailService, pushoverService, webhookService, telegramService, logoService, categoryService, tagService, i18nCatalog)
	settingsHandler := handlers.NewSettingsHandler(settingsService, i18nCatalog)
	categoryHandler := handlers.NewCategoryHandler(categoryService)
	authHandler := handlers.NewAuthHandler(settingsService, sessionService, emailService, i18nCatalog)

	// Setup Gin router
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.Default()

	// Create template functions
	router.SetFuncMap(template.FuncMap{
		"add": func(a, b float64) float64 { return a + b },
		"sub": func(a, b float64) float64 { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
		"div": func(a, b float64) float64 {
			if b == 0 {
				return 0
			}
			return a / b
		},
		"int": func(v interface{}) int {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			case time.Month:
				return int(val)
			default:
				return 0
			}
		},
		"fmtDate": func(t *time.Time, format string) string {
			if t == nil {
				return ""
			}
			return t.Format(format)
		},
		"fmtTime": func(t time.Time, format string) string {
			return t.Format(format)
		},
		"t": func(lang interface{}, key string) string {
			langStr, _ := lang.(string)
			return i18nCatalog.T(langStr, key)
		},
		"statusLabel":   statusLabelFunc(i18nCatalog),
		"scheduleLabel": scheduleLabelFunc(i18nCatalog),
	})

	// Load HTML templates with error handling
	tmpl := loadTemplates(i18nCatalog)
	if tmpl != nil && len(tmpl.Templates()) > 0 {
		router.SetHTMLTemplate(tmpl)
	} else {
		log.Printf("Warning: Template loading failed, using fallback")
		// Fallback to LoadHTMLGlob for compatibility
		router.LoadHTMLGlob("templates/*")
	}

	// Serve static files
	router.Static("/static", "./web/static")
	router.StaticFile("/favicon.ico", "./web/static/favicon.ico")
	router.StaticFile("/manifest.json", "./web/static/manifest.json")

	// Health check endpoint with database connectivity check
	router.GET("/healthz", func(c *gin.Context) {
		// Check database connectivity
		sqlDB, err := db.DB()
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "database connection unavailable",
			})
			return
		}

		// Ping the database to verify connectivity
		if err := sqlDB.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "database ping failed",
			})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
		})
	})

	// Apply auth middleware
	router.Use(middleware.AuthMiddleware(settingsService, sessionService))

	// Routes
	setupRoutes(router, subscriptionHandler, settingsHandler, settingsService, categoryHandler, authHandler, i18nCatalog)

	// Seed sample data if database is empty
	// Commented out - no sample data by default
	// if subscriptionService.Count() == 0 {
	// 	seedSampleData(subscriptionService)
	// }

	// Start renewal reminder scheduler
	go startRenewalReminderScheduler(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)

	// Start cancellation reminder scheduler
	go startCancellationReminderScheduler(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("SubTrackr server starting on port %s", port)
	log.Fatal(router.Run(":" + port))
}

// statusLabelFunc returns a template func that translates a subscription status
// string (e.g. "Active") to its localized label, falling back to the raw value.
func statusLabelFunc(catalog *i18n.Catalog) func(interface{}, string) string {
	return func(lang interface{}, status string) string {
		if status == "" {
			return ""
		}
		langStr, _ := lang.(string)
		key := "status." + strings.ToLower(status)
		if v := catalog.T(langStr, key); v != key {
			return v
		}
		return status
	}
}

// scheduleLabelFunc returns a template func that renders a localized schedule
// label. interval==1 → "Monthly"; interval>1 → "Every N Months".
func scheduleLabelFunc(catalog *i18n.Catalog) func(interface{}, string, int) string {
	pluralKey := map[string]string{
		"Daily":     "schedule.every_n_days",
		"Weekly":    "schedule.every_n_weeks",
		"Monthly":   "schedule.every_n_months",
		"Quarterly": "schedule.every_n_quarters",
		"Annual":    "schedule.every_n_years",
	}
	return func(lang interface{}, schedule string, interval int) string {
		if schedule == "" {
			return ""
		}
		langStr, _ := lang.(string)
		if interval <= 1 {
			key := "schedule." + strings.ToLower(schedule)
			if v := catalog.T(langStr, key); v != key {
				return v
			}
			return schedule
		}
		if key, ok := pluralKey[schedule]; ok {
			return fmt.Sprintf(catalog.T(langStr, key), interval)
		}
		return schedule
	}
}

// loadTemplates loads HTML templates with better error handling for arm64 compatibility
func loadTemplates(catalog *i18n.Catalog) *template.Template {
	tmpl := template.New("")

	// Add template functions
	tmpl.Funcs(template.FuncMap{
		"t": func(lang interface{}, key string) string {
			langStr, _ := lang.(string)
			return catalog.T(langStr, key)
		},
		"statusLabel":   statusLabelFunc(catalog),
		"scheduleLabel": scheduleLabelFunc(catalog),
		"add": func(a, b float64) float64 { return a + b },
		"sub": func(a, b float64) float64 { return a - b },
		"mul": func(a, b float64) float64 { return a * b },
		"div": func(a, b float64) float64 {
			if b == 0 {
				log.Printf("Warning: Division by zero attempted in template")
				return math.NaN()
			}
			return a / b
		},
		"int": func(v interface{}) int {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			case time.Month:
				return int(val)
			default:
				return 0
			}
		},
		"fmtDate": func(t *time.Time, format string) string {
			if t == nil {
				return ""
			}
			return t.Format(format)
		},
		"fmtTime": func(t time.Time, format string) string {
			return t.Format(format)
		},
	})

	// Critical templates required for basic functionality
	criticalTemplates := []string{
		"templates/dashboard.html",
		"templates/subscriptions.html",
		"templates/error.html",
	}

	// All template files to load
	templateFiles := []string{
		"templates/dashboard.html",
		"templates/subscriptions.html",
		"templates/analytics.html",
		"templates/calendar.html",
		"templates/settings.html",
		"templates/subscription-form.html",
		"templates/subscription-list.html",
		"templates/categories-list.html",
		"templates/api-keys-list.html",
		"templates/smtp-message.html",
		"templates/form-errors.html",
		"templates/error.html",
		"templates/login.html",
		"templates/login-error.html",
		"templates/forgot-password.html",
		"templates/forgot-password-error.html",
		"templates/forgot-password-success.html",
		"templates/reset-password.html",
		"templates/reset-password-error.html",
		"templates/reset-password-success.html",
		"templates/auth-message.html",
	}

	var parsedCount int
	var failedCount int
	var missingCritical []string

	// Load templates individually to catch arm64-specific issues
	for _, file := range templateFiles {
		if _, err := os.Stat(file); err != nil {
			log.Printf("Warning: Template file not found: %s", file)
			// Check if this is a critical template
			for _, critical := range criticalTemplates {
				if critical == file {
					missingCritical = append(missingCritical, file)
				}
			}
			continue
		}

		if _, err := tmpl.ParseFiles(file); err != nil {
			log.Printf("Error: Failed to parse template %s: %v", file, err)
			failedCount++
			// Check if this is a critical template
			for _, critical := range criticalTemplates {
				if critical == file {
					missingCritical = append(missingCritical, file)
				}
			}
		} else {
			parsedCount++
		}
	}

	// Log template loading summary
	log.Printf("Template loading summary: %d parsed, %d failed, %d total", parsedCount, failedCount, len(templateFiles))

	// Fatal error if critical templates are missing
	if len(missingCritical) > 0 {
		log.Fatalf("Critical templates failed to load: %v. Application cannot continue.", missingCritical)
	}

	// Warn if too many templates failed
	if failedCount > len(templateFiles)/2 {
		log.Printf("Warning: More than half of templates failed to load (%d/%d). Application may not function correctly.", failedCount, len(templateFiles))
	}

	return tmpl
}

func setupRoutes(router *gin.Engine, handler *handlers.SubscriptionHandler, settingsHandler *handlers.SettingsHandler, settingsService *service.SettingsService, categoryHandler *handlers.CategoryHandler, authHandler *handlers.AuthHandler, i18nCatalog *i18n.Catalog) {
	// Auth routes (public)
	router.GET("/login", authHandler.ShowLoginPage)
	router.GET("/forgot-password", authHandler.ShowForgotPasswordPage)
	router.GET("/reset-password", authHandler.ShowResetPasswordPage)

	// iCal subscription route (public, token-validated)
	router.GET("/ical/:token", handler.ServeICalSubscription)

	// Web routes
	router.GET("/", handler.Dashboard)
	router.GET("/dashboard", handler.Dashboard)
	router.GET("/subscriptions", handler.SubscriptionsList)
	router.GET("/analytics", handler.Analytics)
	router.GET("/calendar", handler.Calendar)
	router.GET("/settings", handler.Settings)

	// Form routes for HTMX modals
	form := router.Group("/form")
	{
		form.GET("/subscription", handler.GetSubscriptionForm)
		form.GET("/subscription/:id", handler.GetSubscriptionForm)
	}

	// API routes for HTMX
	api := router.Group("/api")
	{
		api.GET("/subscriptions", handler.GetSubscriptions)
		api.POST("/subscriptions", handler.CreateSubscription)
		api.GET("/subscriptions/:id", handler.GetSubscription)
		api.PUT("/subscriptions/:id", handler.UpdateSubscription)
		api.DELETE("/subscriptions/:id", handler.DeleteSubscription)
		api.POST("/subscriptions/:id/duplicate", handler.DuplicateSubscription)
		api.GET("/stats", handler.GetStats)

		// Export and data management routes
		api.GET("/export/csv", handler.ExportCSV)
		api.GET("/export/json", handler.ExportJSON)
		api.GET("/export/ical", handler.ExportICal)
		api.GET("/backup", handler.BackupData)
		api.POST("/restore", handler.RestoreData)
		api.POST("/import/wallos", handler.ImportWallos)
		api.DELETE("/clear-all", handler.ClearAllData)

		// Settings routes
		api.POST("/settings/smtp", settingsHandler.SaveSMTPSettings)
		api.POST("/settings/smtp/test", settingsHandler.TestSMTPConnection)
		api.POST("/settings/pushover", settingsHandler.SavePushoverSettings)
		api.POST("/settings/pushover/test", settingsHandler.TestPushoverConnection)
		api.GET("/settings/pushover", settingsHandler.GetPushoverConfig)
		api.POST("/settings/webhook", settingsHandler.SaveWebhookSettings)
		api.POST("/settings/webhook/test", settingsHandler.TestWebhookConnection)
		api.POST("/settings/telegram", settingsHandler.SaveTelegramSettings)
		api.POST("/settings/telegram/test", settingsHandler.TestTelegramConnection)
		api.GET("/settings/telegram", settingsHandler.GetTelegramConfig)
		api.POST("/settings/notifications/:setting", settingsHandler.UpdateNotificationSetting)
		api.GET("/settings/notifications", settingsHandler.GetNotificationSettings)
		api.GET("/settings/smtp", settingsHandler.GetSMTPConfig)

		// API Key management routes
		api.GET("/settings/apikeys", settingsHandler.ListAPIKeys)
		api.POST("/settings/apikeys", settingsHandler.CreateAPIKey)
		api.DELETE("/settings/apikeys/:id", settingsHandler.DeleteAPIKey)

		// Currency setting
		api.POST("/settings/currency", settingsHandler.UpdateCurrency)

		// Date format setting
		api.POST("/settings/date-format", settingsHandler.UpdateDateFormat)

		// Dark mode setting
		api.POST("/settings/dark-mode", settingsHandler.ToggleDarkMode)

		// Category management routes
		api.GET("/categories", categoryHandler.ListCategories)
		api.POST("/categories", categoryHandler.CreateCategory)
		api.PUT("/categories/:id", categoryHandler.UpdateCategory)
		api.DELETE("/categories/:id", categoryHandler.DeleteCategory)

		// Auth routes
		api.POST("/auth/login", authHandler.Login)
		api.GET("/auth/logout", authHandler.Logout)
		api.POST("/auth/forgot-password", authHandler.ForgotPassword)
		api.POST("/auth/reset-password", authHandler.ResetPassword)

		// Auth settings routes
		api.POST("/settings/auth/setup", settingsHandler.SetupAuth)
		api.POST("/settings/auth/disable", settingsHandler.DisableAuth)
		api.GET("/settings/auth/status", settingsHandler.GetAuthStatus)

		// Theme settings routes
		api.GET("/settings/theme", settingsHandler.GetTheme)
		api.POST("/settings/theme", settingsHandler.SetTheme)

		// Language setting
		api.POST("/settings/language", func(c *gin.Context) {
			settingsHandler.SetLanguage(c, i18nCatalog.HasLanguage)
		})

		// iCal subscription settings
		api.POST("/settings/ical/toggle", settingsHandler.ToggleICalSubscription)
		api.GET("/settings/ical/url", settingsHandler.GetICalSubscriptionURL)
		api.POST("/settings/ical/regenerate", settingsHandler.RegenerateICalToken)

		// Base URL setting
		api.POST("/settings/base-url", settingsHandler.UpdateBaseURL)
	}

	// Public API routes (require API key authentication)
	v1 := router.Group("/api/v1")
	v1.Use(middleware.APIKeyAuth(settingsService))
	{
		// Subscription endpoints
		v1.GET("/subscriptions", handler.GetSubscriptionsAPI)
		v1.POST("/subscriptions", handler.CreateSubscription)
		v1.GET("/subscriptions/:id", handler.GetSubscription)
		v1.PUT("/subscriptions/:id", handler.UpdateSubscription)
		v1.DELETE("/subscriptions/:id", handler.DeleteSubscription)
		v1.POST("/subscriptions/:id/duplicate", handler.DuplicateSubscription)

		// Stats and export endpoints
		v1.GET("/stats", handler.GetStats)
		v1.GET("/export/csv", handler.ExportCSV)
		v1.GET("/export/json", handler.ExportJSON)
	}
}

// startRenewalReminderScheduler starts a background goroutine that checks for
// upcoming renewals and sends reminder emails and Pushover notifications daily
func startRenewalReminderScheduler(subscriptionService *service.SubscriptionService, emailService *service.EmailService, pushoverService *service.PushoverService, webhookService *service.WebhookService, telegramService *service.TelegramService, settingsService *service.SettingsService) {
	// Run immediately on startup (after a short delay to let server initialize)
	go func() {
		time.Sleep(30 * time.Second) // Wait 30 seconds for server to fully start
		checkAndSendRenewalReminders(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)
	}()

	// Then run daily at midnight
	// Note: Ticker is intentionally not stopped as this is a long-running server process.
	// The ticker will run for the lifetime of the application, which is the desired behavior.
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		defer ticker.Stop() // Clean up ticker if goroutine exits (defensive programming)
		for range ticker.C {
			// Recover from any panics in the reminder check to keep the scheduler running
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Panic in renewal reminder check: %v", r)
					}
				}()
				checkAndSendRenewalReminders(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)
			}()
		}
	}()
}

// checkAndSendRenewalReminders checks for subscriptions needing reminders and sends emails and Pushover notifications
func checkAndSendRenewalReminders(subscriptionService *service.SubscriptionService, emailService *service.EmailService, pushoverService *service.PushoverService, webhookService *service.WebhookService, telegramService *service.TelegramService, settingsService *service.SettingsService) {
	// Check if renewal reminders are enabled
	enabled, err := settingsService.GetBoolSetting("renewal_reminders", false)
	if err != nil || !enabled {
		return // Silently skip if disabled or error
	}

	// Resolve reminder windows: multi-value CSV takes precedence; fall back to single-int setting.
	windowsCSV := settingsService.GetStringSettingWithDefault("reminder_days_list", "")
	fallback := settingsService.GetIntSettingWithDefault("reminder_days", 7)
	windows := service.ParseReminderWindows(windowsCSV, fallback)
	if len(windows) == 0 {
		return // No reminders configured
	}

	// Get subscriptions needing reminders
	subscriptions, err := subscriptionService.GetSubscriptionsNeedingReminders(windows)
	if err != nil {
		log.Printf("Error getting subscriptions for renewal reminders: %v", err)
		return
	}

	if len(subscriptions) == 0 {
		log.Printf("No subscriptions need renewal reminders today")
		return
	}

	log.Printf("Checking %d subscription(s) for renewal reminders", len(subscriptions))

	// Send reminder for each subscription (both email and Pushover)
	sentCount := 0
	failedCount := 0
	for sub, daysUntil := range subscriptions {
		emailErr := emailService.SendRenewalReminder(sub, daysUntil)
		pushoverErr := pushoverService.SendRenewalReminder(sub, daysUntil)
		webhookErr := webhookService.SendRenewalReminder(sub, daysUntil)
		telegramErr := telegramService.SendRenewalReminder(sub, daysUntil)

		// If all fail, count as failed; otherwise consider it sent
		if emailErr != nil && pushoverErr != nil && webhookErr != nil && telegramErr != nil {
			log.Printf("Error sending renewal reminder for subscription %s (ID: %d): email=%v, pushover=%v, webhook=%v, telegram=%v", sub.Name, sub.ID, emailErr, pushoverErr, webhookErr, telegramErr)
			failedCount++
		} else {
			// Mark reminder as sent for this renewal date and remember the window we just fired.
			// Renewal-date changes are handled by the sameRenewal check in
			// GetSubscriptionsNeedingReminders, so no explicit reset is needed here.
			now := time.Now()
			sub.LastReminderSent = &now
			if sub.RenewalDate != nil {
				renewalDateCopy := *sub.RenewalDate
				sub.LastReminderRenewalDate = &renewalDateCopy
			}
			sub.LastReminderWindow = daysUntil

			// Update the subscription in the database
			_, updateErr := subscriptionService.Update(sub.ID, sub)
			if updateErr != nil {
				log.Printf("Warning: Failed to update last reminder sent for subscription %s (ID: %d): %v", sub.Name, sub.ID, updateErr)
			}

			var failed []string
			if emailErr != nil {
				failed = append(failed, fmt.Sprintf("email=%v", emailErr))
			}
			if pushoverErr != nil {
				failed = append(failed, fmt.Sprintf("pushover=%v", pushoverErr))
			}
			if webhookErr != nil {
				failed = append(failed, fmt.Sprintf("webhook=%v", webhookErr))
			}
			if telegramErr != nil {
				failed = append(failed, fmt.Sprintf("telegram=%v", telegramErr))
			}
			if len(failed) > 0 {
				log.Printf("Sent renewal reminder for subscription %s (renews in %d days) - some channels failed: %s", sub.Name, daysUntil, strings.Join(failed, ", "))
			} else {
				log.Printf("Sent renewal reminders for subscription %s (renews in %d days)", sub.Name, daysUntil)
			}
			sentCount++
		}
	}

	log.Printf("Renewal reminder check complete: %d sent, %d failed", sentCount, failedCount)
}

// startCancellationReminderScheduler starts a background goroutine that checks for
// upcoming cancellations and sends reminder emails and Pushover notifications daily
func startCancellationReminderScheduler(subscriptionService *service.SubscriptionService, emailService *service.EmailService, pushoverService *service.PushoverService, webhookService *service.WebhookService, telegramService *service.TelegramService, settingsService *service.SettingsService) {
	// Run immediately on startup (after a short delay to let server initialize)
	go func() {
		time.Sleep(30 * time.Second) // Wait 30 seconds for server to fully start
		checkAndSendCancellationReminders(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)
	}()

	// Then run daily at midnight
	// Note: Ticker is intentionally not stopped as this is a long-running server process.
	// The ticker will run for the lifetime of the application, which is the desired behavior.
	ticker := time.NewTicker(24 * time.Hour)
	go func() {
		defer ticker.Stop() // Clean up ticker if goroutine exits (defensive programming)
		for range ticker.C {
			// Recover from any panics in the reminder check to keep the scheduler running
			func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("Panic in cancellation reminder check: %v", r)
					}
				}()
				checkAndSendCancellationReminders(subscriptionService, emailService, pushoverService, webhookService, telegramService, settingsService)
			}()
		}
	}()
}

// checkAndSendCancellationReminders checks for subscriptions needing cancellation reminders and sends emails and Pushover notifications
func checkAndSendCancellationReminders(subscriptionService *service.SubscriptionService, emailService *service.EmailService, pushoverService *service.PushoverService, webhookService *service.WebhookService, telegramService *service.TelegramService, settingsService *service.SettingsService) {
	// Check if cancellation reminders are enabled
	enabled, err := settingsService.GetBoolSetting("cancellation_reminders", false)
	if err != nil || !enabled {
		return // Silently skip if disabled or error
	}

	// Resolve cancellation reminder windows: multi-value CSV takes precedence; fall back to single-int setting.
	windowsCSV := settingsService.GetStringSettingWithDefault("cancellation_reminder_days_list", "")
	fallback := settingsService.GetIntSettingWithDefault("cancellation_reminder_days", 7)
	windows := service.ParseReminderWindows(windowsCSV, fallback)
	if len(windows) == 0 {
		return // No reminders configured
	}

	// Get subscriptions needing cancellation reminders
	subscriptions, err := subscriptionService.GetSubscriptionsNeedingCancellationReminders(windows)
	if err != nil {
		log.Printf("Error getting subscriptions for cancellation reminders: %v", err)
		return
	}

	if len(subscriptions) == 0 {
		log.Printf("No subscriptions need cancellation reminders today")
		return
	}

	log.Printf("Checking %d subscription(s) for cancellation reminders", len(subscriptions))

	// Send reminder for each subscription (both email and Pushover)
	sentCount := 0
	failedCount := 0
	for sub, daysUntil := range subscriptions {
		emailErr := emailService.SendCancellationReminder(sub, daysUntil)
		pushoverErr := pushoverService.SendCancellationReminder(sub, daysUntil)
		webhookErr := webhookService.SendCancellationReminder(sub, daysUntil)
		telegramErr := telegramService.SendCancellationReminder(sub, daysUntil)

		// If all fail, count as failed; otherwise consider it sent
		if emailErr != nil && pushoverErr != nil && webhookErr != nil && telegramErr != nil {
			log.Printf("Error sending cancellation reminder for subscription %s (ID: %d): email=%v, pushover=%v, webhook=%v, telegram=%v", sub.Name, sub.ID, emailErr, pushoverErr, webhookErr, telegramErr)
			failedCount++
		} else {
			// Mark reminder as sent for this cancellation date and remember the window we just fired.
			// Cancellation-date changes are handled by the sameDate check in
			// GetSubscriptionsNeedingCancellationReminders, so no explicit reset is needed here.
			now := time.Now()
			sub.LastCancellationReminderSent = &now
			if sub.CancellationDate != nil {
				cancellationDateCopy := *sub.CancellationDate
				sub.LastCancellationReminderDate = &cancellationDateCopy
			}
			sub.LastCancellationReminderWindow = daysUntil

			// Update the subscription in the database
			_, updateErr := subscriptionService.Update(sub.ID, sub)
			if updateErr != nil {
				log.Printf("Warning: Failed to update last cancellation reminder sent for subscription %s (ID: %d): %v", sub.Name, sub.ID, updateErr)
			}

			var failed []string
			if emailErr != nil {
				failed = append(failed, fmt.Sprintf("email=%v", emailErr))
			}
			if pushoverErr != nil {
				failed = append(failed, fmt.Sprintf("pushover=%v", pushoverErr))
			}
			if webhookErr != nil {
				failed = append(failed, fmt.Sprintf("webhook=%v", webhookErr))
			}
			if telegramErr != nil {
				failed = append(failed, fmt.Sprintf("telegram=%v", telegramErr))
			}
			if len(failed) > 0 {
				log.Printf("Sent cancellation reminder for subscription %s (ends in %d days) - some channels failed: %s", sub.Name, daysUntil, strings.Join(failed, ", "))
			} else {
				log.Printf("Sent cancellation reminders for subscription %s (ends in %d days)", sub.Name, daysUntil)
			}
			sentCount++
		}
	}

	log.Printf("Cancellation reminder check complete: %d sent, %d failed", sentCount, failedCount)
}

// handleResetPassword handles the --reset-password CLI command
func handleResetPassword(settingsService *service.SettingsService, newPassword string) {
	var password string

	if newPassword != "" {
		// Non-interactive mode
		password = newPassword
	} else {
		// Interactive mode - prompt for password
		fmt.Print("Enter new admin password: ")
		passwordBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			log.Fatal("Failed to read password:", err)
		}
		fmt.Println()

		fmt.Print("Confirm password: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			log.Fatal("Failed to read confirmation:", err)
		}
		fmt.Println()

		// Use constant-time comparison to prevent timing attacks
		if subtle.ConstantTimeCompare(passwordBytes, confirmBytes) != 1 {
			log.Fatal("Passwords do not match")
		}

		password = string(passwordBytes)
	}

	// Validate password length
	if len(password) < 8 {
		log.Fatal("Password must be at least 8 characters long")
	}

	// Update password
	if err := settingsService.SetAuthPassword(password); err != nil {
		log.Fatal("Failed to update password:", err)
	}

	fmt.Println("✓ Admin password reset successfully")
	os.Exit(0)
}

// handleDisableAuth handles the --disable-auth CLI command
func handleDisableAuth(settingsService *service.SettingsService) {
	if err := settingsService.DisableAuth(); err != nil {
		log.Fatal("Failed to disable authentication:", err)
	}

	fmt.Println("✓ Authentication disabled successfully")
	fmt.Println("  Note: Credentials are preserved and can be re-enabled from Settings")
	os.Exit(0)
}

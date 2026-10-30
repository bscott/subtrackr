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
	notifier := service.NewNotificationDispatcher(settingsService,
		emailService,
		service.NewPushoverService(settingsService),
		service.NewWebhookService(settingsService),
		service.NewTelegramService(settingsService),
		service.NewNtfyService(settingsService),
		service.NewGotifyService(settingsService),
	)
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
	subscriptionHandler := handlers.NewSubscriptionHandler(subscriptionService, settingsService, currencyService, notifier, logoService, categoryService, tagService, i18nCatalog)
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
		"add":          func(a, b float64) float64 { return a + b },
		"sub":          func(a, b float64) float64 { return a - b },
		"mul":          func(a, b float64) float64 { return a * b },
		"formatAmount": service.FormatAmount,
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
	startReminderScheduler(subscriptionService, notifier)

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
		"add":           func(a, b float64) float64 { return a + b },
		"sub":           func(a, b float64) float64 { return a - b },
		"mul":           func(a, b float64) float64 { return a * b },
		"formatAmount":  service.FormatAmount,
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
	router.GET("/auth/oidc/login", authHandler.OIDCLogin)
	router.GET("/auth/oidc/callback", authHandler.OIDCCallback)

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
		api.POST("/settings/ntfy", settingsHandler.SaveNtfySettings)
		api.POST("/settings/ntfy/test", settingsHandler.TestNtfyConnection)
		api.GET("/settings/ntfy", settingsHandler.GetNtfyConfig)
		api.POST("/settings/gotify", settingsHandler.SaveGotifySettings)
		api.POST("/settings/gotify/test", settingsHandler.TestGotifyConnection)
		api.GET("/settings/gotify", settingsHandler.GetGotifyConfig)
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
		api.POST("/settings/auth/oidc", settingsHandler.SaveOIDCSettings)
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

// startReminderScheduler runs reminder passes sequentially so they cannot
// overwrite each other's delivery state for the same subscription.
func startReminderScheduler(subscriptionService *service.SubscriptionService, notifier *service.NotificationDispatcher) {
	runDailyReminderCheck(func() { runReminderPasses(subscriptionService, notifier) })
}

// runReminderPasses runs every reminder pass in turn. A panic ends only its own
// pass, so a misbehaving channel cannot cost the passes after it.
func runReminderPasses(subscriptionService *service.SubscriptionService, notifier *service.NotificationDispatcher) {
	runReminderCheck("renewal", func() { notifier.RunRenewalReminders(subscriptionService) })
	runReminderCheck("cancellation", func() { notifier.RunCancellationReminders(subscriptionService) })
	runReminderCheck("to-cancel", func() { notifier.RunToCancelReminders(subscriptionService) })
}

// runReminderCheck protects both startup and daily passes from a channel panic.
func runReminderCheck(kind string, check func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Panic in %s reminder check: %v", kind, r)
		}
	}()
	check()
}

// runDailyReminderCheck runs once after startup and then every 24 hours, with
// only one pass in flight even when a provider responds slowly.
func runDailyReminderCheck(check func()) {
	go func() {
		time.Sleep(30 * time.Second)
		check()
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			check()
		}
	}()
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

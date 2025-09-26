package main

import (
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"subtrackr/internal/config"
	"subtrackr/internal/database"
	"subtrackr/internal/handlers"
	"subtrackr/internal/middleware"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

func main() {
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

	// Initialize services
	categoryService := service.NewCategoryService(categoryRepo)
	currencyService := service.NewCurrencyService(exchangeRateRepo)
	subscriptionService := service.NewSubscriptionService(subscriptionRepo, categoryService)
	settingsService := service.NewSettingsService(settingsRepo)

	// Initialize handlers
	subscriptionHandler := handlers.NewSubscriptionHandler(subscriptionService, settingsService, currencyService)
	settingsHandler := handlers.NewSettingsHandler(settingsService)
	categoryHandler := handlers.NewCategoryHandler(categoryService)

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
	})

	// Load HTML templates with error handling
	tmpl := loadTemplates()
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

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Routes
	setupRoutes(router, subscriptionHandler, settingsHandler, settingsService, categoryHandler)

	// Seed sample data if database is empty
	// Commented out - no sample data by default
	// if subscriptionService.Count() == 0 {
	// 	seedSampleData(subscriptionService)
	// }


	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("SubTrackr server starting on port %s", port)
	log.Fatal(router.Run(":" + port))
}

// loadTemplates loads HTML templates with better error handling for arm64 compatibility
func loadTemplates() *template.Template {
	tmpl := template.New("")
	
	// Add template functions
	tmpl.Funcs(template.FuncMap{
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
		"templates/settings.html",
		"templates/subscription-form.html",
		"templates/subscription-list.html",
		"templates/categories-list.html",
		"templates/api-keys-list.html",
		"templates/smtp-message.html",
		"templates/form-errors.html",
		"templates/error.html",
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

func setupRoutes(router *gin.Engine, handler *handlers.SubscriptionHandler, settingsHandler *handlers.SettingsHandler, settingsService *service.SettingsService, categoryHandler *handlers.CategoryHandler) {
	// Web routes
	router.GET("/", handler.Dashboard)
	router.GET("/dashboard", handler.Dashboard)
	router.GET("/subscriptions", handler.SubscriptionsList)
	router.GET("/analytics", handler.Analytics)
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
		api.GET("/stats", handler.GetStats)

		// Export and data management routes
		api.GET("/export/csv", handler.ExportCSV)
		api.GET("/export/json", handler.ExportJSON)
		api.GET("/backup", handler.BackupData)
		api.DELETE("/clear-all", handler.ClearAllData)

		// Settings routes
		api.POST("/settings/smtp", settingsHandler.SaveSMTPSettings)
		api.POST("/settings/smtp/test", settingsHandler.TestSMTPConnection)
		api.POST("/settings/notifications/:setting", settingsHandler.UpdateNotificationSetting)
		api.GET("/settings/notifications", settingsHandler.GetNotificationSettings)
		api.GET("/settings/smtp", settingsHandler.GetSMTPConfig)

		// API Key management routes
		api.GET("/settings/apikeys", settingsHandler.ListAPIKeys)
		api.POST("/settings/apikeys", settingsHandler.CreateAPIKey)
		api.DELETE("/settings/apikeys/:id", settingsHandler.DeleteAPIKey)

		// Currency setting
		api.POST("/settings/currency", settingsHandler.UpdateCurrency)

		// Dark mode setting
		api.POST("/settings/dark-mode", settingsHandler.ToggleDarkMode)

		// Category management routes
		api.GET("/categories", categoryHandler.ListCategories)
		api.POST("/categories", categoryHandler.CreateCategory)
		api.PUT("/categories/:id", categoryHandler.UpdateCategory)
		api.DELETE("/categories/:id", categoryHandler.DeleteCategory)
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

		// Stats and export endpoints
		v1.GET("/stats", handler.GetStats)
		v1.GET("/export/csv", handler.ExportCSV)
		v1.GET("/export/json", handler.ExportJSON)
	}
}

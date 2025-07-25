package main

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"subtrackr/internal/config"
	"subtrackr/internal/database"
	"subtrackr/internal/handlers"
	"subtrackr/internal/middleware"
	"subtrackr/internal/models"
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

	// Auto-migrate the schema
	err = db.AutoMigrate(&models.Subscription{}, &models.Settings{}, &models.APIKey{})
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Initialize repositories
	subscriptionRepo := repository.NewSubscriptionRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	// Initialize services
	subscriptionService := service.NewSubscriptionService(subscriptionRepo)
	settingsService := service.NewSettingsService(settingsRepo)

	// Initialize handlers
	subscriptionHandler := handlers.NewSubscriptionHandler(subscriptionService, settingsService)
	settingsHandler := handlers.NewSettingsHandler(settingsService)

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
			if b == 0 { return 0 }
			return a / b 
		},
	})

	// Load HTML templates
	router.LoadHTMLGlob("templates/*")

	// Serve static files
	router.Static("/static", "./web/static")
	router.StaticFile("/favicon.ico", "./web/static/favicon.ico")

	// Health check endpoint
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Routes
	setupRoutes(router, subscriptionHandler, settingsHandler, settingsService)

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

func setupRoutes(router *gin.Engine, handler *handlers.SubscriptionHandler, settingsHandler *handlers.SettingsHandler, settingsService *service.SettingsService) {
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
	}

	// Public API routes (require API key authentication)
	v1 := router.Group("/api/v1")
	v1.Use(middleware.APIKeyAuth(settingsService))
	{
		// Subscription endpoints
		v1.GET("/subscriptions", handler.GetSubscriptions)
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

func seedSampleData(service *service.SubscriptionService) {
	log.Println("Seeding sample data...")

	sampleSubscriptions := []models.Subscription{
		{
			Name:     "Netflix",
			Cost:     15.49,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Entertainment",
			Account:  "user@example.com",
		},
		{
			Name:     "GitHub Copilot",
			Cost:     10.00,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Productivity",
			Account:  "dev@example.com",
		},
		{
			Name:     "Dropbox",
			Cost:     9.99,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Storage",
			Account:  "user@example.com",
		},
		{
			Name:     "Spotify",
			Cost:     9.99,
			Schedule: "Monthly",
			Status:   "Cancelled",
			Category: "Entertainment",
			Account:  "music@example.com",
		},
		{
			Name:     "Adobe Creative Cloud",
			Cost:     52.99,
			Schedule: "Monthly",
			Status:   "Active",
			Category: "Productivity",
			Account:  "creative@example.com",
		},
	}

	for _, sub := range sampleSubscriptions {
		_, err := service.Create(&sub)
		if err != nil {
			log.Printf("Failed to create sample subscription %s: %v", sub.Name, err)
		}
	}

	log.Println("Sample data seeded successfully")
}
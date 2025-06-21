package middleware

import (
	"net/http"
	"strings"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

// APIKeyAuth creates a middleware that validates API keys
func APIKeyAuth(settingsService *service.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for API key in Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Check for API key in X-API-Key header
			authHeader = c.GetHeader("X-API-Key")
		}

		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API key required"})
			c.Abort()
			return
		}

		// Extract the API key
		apiKey := authHeader
		if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
			apiKey = authHeader[7:]
		}

		// Validate the API key
		key, err := settingsService.ValidateAPIKey(apiKey)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		// Store the API key info in context for later use
		c.Set("api_key", key)
		c.Next()
	}
}

// OptionalAPIKeyAuth creates a middleware that optionally validates API keys
// If an API key is provided, it validates it. If not, the request continues.
func OptionalAPIKeyAuth(settingsService *service.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check for API key in Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			// Check for API key in X-API-Key header
			authHeader = c.GetHeader("X-API-Key")
		}

		if authHeader != "" {
			// Extract the API key
			apiKey := authHeader
			if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
				apiKey = authHeader[7:]
			}

			// Validate the API key
			key, err := settingsService.ValidateAPIKey(apiKey)
			if err == nil {
				// Store the API key info in context
				c.Set("api_key", key)
				c.Set("authenticated", true)
			}
		}

		c.Next()
	}
}
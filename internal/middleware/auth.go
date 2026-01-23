package middleware

import (
	"net/http"
	"net/url"
	"strings"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware creates middleware that requires authentication
func AuthMiddleware(settingsService *service.SettingsService, sessionService *service.SessionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if auth is enabled
		if !settingsService.IsAuthEnabled() {
			c.Next()
			return
		}

		// Skip auth for certain routes
		path := c.Request.URL.Path
		if isPublicRoute(path) {
			c.Next()
			return
		}

		// Check if user is authenticated
		if !sessionService.IsAuthenticated(c.Request) {
			// Redirect to login page for HTML requests
			if isHTMLRequest(c.Request) {
				c.Redirect(http.StatusFound, "/login?redirect="+url.QueryEscape(c.Request.URL.Path))
				c.Abort()
				return
			}

			// Return 401 for API requests
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authentication required"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// isPublicRoute checks if a route should be accessible without authentication
func isPublicRoute(path string) bool {
	publicRoutes := []string{
		"/login",
		"/forgot-password",
		"/reset-password",
		"/api/auth/login",
		"/api/auth/logout",
		"/api/auth/forgot-password",
		"/api/auth/reset-password",
		"/static/",
		"/favicon.ico",
		"/healthz",
	}

	// API v1 routes use API keys, not session auth
	if strings.HasPrefix(path, "/api/v1/") {
		return true
	}

	for _, route := range publicRoutes {
		if strings.HasPrefix(path, route) {
			return true
		}
	}

	return false
}

// isHTMLRequest checks if the request is for HTML content
func isHTMLRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html") || accept == ""
}

// APIKeyAuth creates middleware that requires API key authentication
func APIKeyAuth(settingsService *service.SettingsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")

		// Also check Authorization: Bearer header
		if apiKey == "" {
			authHeader := c.GetHeader("Authorization")
			if strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimPrefix(authHeader, "Bearer ")
			}
		}

		if apiKey == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "API key required"})
			c.Abort()
			return
		}

		// Validate API key
		_, err := settingsService.ValidateAPIKey(apiKey)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		c.Next()
	}
}

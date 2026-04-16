package handlers

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// buildBaseURL returns the external base URL for the application.
// Priority: configured base URL > X-Forwarded headers > request Host.
func buildBaseURL(c *gin.Context, configuredBaseURL string) string {
	if configuredBaseURL != "" {
		return strings.TrimRight(configuredBaseURL, "/")
	}

	scheme := "http"
	host := c.Request.Host

	// Check X-Forwarded-Proto / X-Forwarded-Host (reverse proxy headers)
	if fwdProto := c.GetHeader("X-Forwarded-Proto"); fwdProto != "" {
		scheme = fwdProto
	} else if c.Request.TLS != nil {
		scheme = "https"
	}

	if fwdHost := c.GetHeader("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}

	return scheme + "://" + host
}

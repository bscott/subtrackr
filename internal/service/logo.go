package service

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LogoService handles fetching logos/icons for subscriptions
type LogoService struct {
	httpClient *http.Client
}

// NewLogoService creates a new logo service
func NewLogoService() *LogoService {
	return &LogoService{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchLogoFromURL extracts the domain from a website URL and returns a favicon URL
// Uses Google's favicon service as the primary source
func (s *LogoService) FetchLogoFromURL(websiteURL string) (string, error) {
	if websiteURL == "" {
		return "", fmt.Errorf("empty URL provided")
	}

	// Normalize URL - add https:// if no protocol is specified
	normalizedURL := strings.TrimSpace(websiteURL)
	if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
		normalizedURL = "https://" + normalizedURL
	}

	// Parse the URL to extract domain
	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	// Get the domain (hostname without port)
	domain := parsedURL.Hostname()
	if domain == "" {
		// If hostname is empty, try using the path as domain (for cases like "netflix.com")
		if parsedURL.Path != "" {
			domain = strings.TrimPrefix(parsedURL.Path, "/")
		} else {
			return "", fmt.Errorf("could not extract domain from URL")
		}
	}

	// Remove www. prefix for cleaner lookups
	domain = strings.TrimPrefix(domain, "www.")
	// Remove trailing slashes
	domain = strings.TrimSuffix(domain, "/")

	// Try Google's favicon service first (most reliable)
	faviconURL := fmt.Sprintf("https://www.google.com/s2/favicons?domain=%s&sz=64", url.QueryEscape(domain))

	return faviconURL, nil
}


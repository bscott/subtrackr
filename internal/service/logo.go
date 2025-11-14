package service

import (
	"fmt"
	"io"
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

// GetLogoURL returns the logo URL for a subscription
// Returns the stored IconURL if available, otherwise tries to fetch from URL
func (s *LogoService) GetLogoURL(iconURL, websiteURL string) string {
	// If icon URL is already set, return it
	if iconURL != "" {
		return iconURL
	}

	// If no website URL, return empty
	if websiteURL == "" {
		return ""
	}

	// Try to fetch logo from website URL
	fetchedURL, err := s.FetchLogoFromURL(websiteURL)
	if err != nil {
		return ""
	}

	return fetchedURL
}

// ValidateLogoURL checks if a logo URL is accessible
func (s *LogoService) ValidateLogoURL(logoURL string) bool {
	if logoURL == "" {
		return false
	}

	resp, err := s.httpClient.Head(logoURL)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Check if response is successful (2xx) and is an image
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// FetchAndValidateLogo fetches a logo and validates it's accessible
func (s *LogoService) FetchAndValidateLogo(websiteURL string) (string, error) {
	logoURL, err := s.FetchLogoFromURL(websiteURL)
	if err != nil {
		return "", err
	}

	// Validate the logo URL (check if it's accessible)
	if !s.ValidateLogoURL(logoURL) {
		// Still return the URL even if validation fails
		// The browser will handle broken images gracefully
		return logoURL, nil
	}

	return logoURL, nil
}

// ExtractDomain extracts the domain from a URL string
// This is a helper method that reuses the domain extraction logic from FetchLogoFromURL
func (s *LogoService) ExtractDomain(websiteURL string) string {
	if websiteURL == "" {
		return ""
	}

	// Normalize URL - add https:// if no protocol is specified
	normalizedURL := strings.TrimSpace(websiteURL)
	if !strings.HasPrefix(normalizedURL, "http://") && !strings.HasPrefix(normalizedURL, "https://") {
		normalizedURL = "https://" + normalizedURL
	}

	parsedURL, err := url.Parse(normalizedURL)
	if err != nil {
		return ""
	}

	domain := parsedURL.Hostname()
	if domain == "" {
		// If hostname is empty, try using the path as domain
		if parsedURL.Path != "" {
			domain = strings.TrimPrefix(parsedURL.Path, "/")
		} else {
			return ""
		}
	}

	domain = strings.TrimPrefix(domain, "www.")
	domain = strings.TrimSuffix(domain, "/")
	return domain
}

// DownloadLogo downloads a logo from a URL and returns the image data
// This is for future use if we want to store logos locally
func (s *LogoService) DownloadLogo(logoURL string) ([]byte, error) {
	resp, err := s.httpClient.Get(logoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to download logo: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download logo: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read logo data: %w", err)
	}

	return data, nil
}


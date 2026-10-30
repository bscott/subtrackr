package handlers

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

// Settings handlers for the ntfy and Gotify push channels.
//
// Saved tokens are never rendered back into the page, so a blank token field
// keeps the stored token, but only while the server URL is unchanged: a
// stored credential is never sent to a server it was not entered for.
// "Test" sends with the submitted values directly and never writes them to
// the database.

const pushTestTitle = "SubTrackr Test"

// NtfySettingsView is the secret-free ntfy configuration rendered on /settings.
type NtfySettingsView struct {
	ServerURL      string
	Topic          string
	Priority       int
	HasAccessToken bool
}

// GotifySettingsView is the secret-free Gotify configuration rendered on /settings.
type GotifySettingsView struct {
	ServerURL   string
	Priority    int
	HasAppToken bool
}

// ntfySettingsView returns the view of the saved ntfy configuration, with
// defaults when nothing has been saved yet.
func ntfySettingsView(settings *service.SettingsService) (NtfySettingsView, bool) {
	view := NtfySettingsView{ServerURL: service.NtfyDefaultServerURL, Priority: service.NtfyDefaultPriority}
	config, err := settings.GetNtfyConfig()
	if err != nil || config.Topic == "" {
		return view, false
	}
	if config.ServerURL != "" {
		view.ServerURL = config.ServerURL
	}
	view.Topic = config.Topic
	view.Priority = service.NormalizeNtfyPriority(config.Priority)
	view.HasAccessToken = config.AccessToken != ""
	return view, true
}

// gotifySettingsView returns the view of the saved Gotify configuration.
func gotifySettingsView(settings *service.SettingsService) (GotifySettingsView, bool) {
	view := GotifySettingsView{Priority: service.GotifyDefaultPriority}
	config, err := settings.GetGotifyConfig()
	if err != nil || config.ServerURL == "" {
		return view, false
	}
	view.ServerURL = config.ServerURL
	view.Priority = service.NormalizeGotifyPriority(config.Priority)
	view.HasAppToken = config.AppToken != ""
	return view, config.AppToken != ""
}

func (h *SettingsHandler) tr(key string) string {
	if h.i18nCatalog == nil {
		return key
	}
	return h.i18nCatalog.T(h.activeLang(), key)
}

func (h *SettingsHandler) pushMessage(c *gin.Context, status int, errKey, msgKey string) {
	if errKey != "" {
		c.HTML(status, "smtp-message.html", gin.H{"Error": h.tr(errKey), "Type": "error"})
		return
	}
	c.HTML(status, "smtp-message.html", gin.H{"Message": h.tr(msgKey), "Type": "success"})
}

// parsePriority reads an integer priority form field, using def when blank.
func parsePriority(raw string, def, min, max int) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def, true
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p < min || p > max {
		return 0, false
	}
	return p, true
}

// sameServer reports whether a stored server URL normalizes to serverURL.
func sameServer(stored, serverURL string) bool {
	normalized, err := service.NormalizePushServerURL(stored)
	return err == nil && normalized == serverURL
}

func formChecked(c *gin.Context, name string) bool {
	switch c.PostForm(name) {
	case "on", "true", "1":
		return true
	}
	return false
}

// ntfyConfigFromForm builds the effective ntfy configuration from the form.
func (h *SettingsHandler) ntfyConfigFromForm(c *gin.Context) (config *models.NtfyConfig, errKey string) {
	rawServer := strings.TrimSpace(c.PostForm("ntfy_server_url"))
	if rawServer == "" {
		rawServer = service.NtfyDefaultServerURL
	}
	serverURL, err := service.NormalizePushServerURL(rawServer)
	if err != nil {
		return nil, "settings.push_error_server_url"
	}

	topic := strings.TrimSpace(c.PostForm("ntfy_topic"))
	if topic == "" {
		return nil, "settings.ntfy_error_topic_required"
	}
	if service.ValidateNtfyTopic(topic) != nil {
		return nil, "settings.ntfy_error_topic_invalid"
	}

	priority, ok := parsePriority(c.PostForm("ntfy_priority"), service.NtfyDefaultPriority, 1, 5)
	if !ok {
		return nil, "settings.ntfy_error_priority"
	}

	token := strings.TrimSpace(c.PostForm("ntfy_access_token"))
	switch {
	case token != "":
		if service.ValidatePushToken(token) != nil {
			return nil, "settings.push_error_token_invalid"
		}
	case formChecked(c, "ntfy_clear_token"):
		// Leave the token empty to remove the saved one.
	default:
		if existing, err := h.service.GetNtfyConfig(); err == nil && existing.AccessToken != "" {
			if !sameServer(existing.ServerURL, serverURL) {
				return nil, "settings.push_error_token_reenter"
			}
			token = existing.AccessToken
		}
	}

	return &models.NtfyConfig{ServerURL: serverURL, Topic: topic, AccessToken: token, Priority: priority}, ""
}

// gotifyConfigFromForm builds the effective Gotify configuration from the form.
func (h *SettingsHandler) gotifyConfigFromForm(c *gin.Context) (config *models.GotifyConfig, errKey string) {
	rawServer := strings.TrimSpace(c.PostForm("gotify_server_url"))
	if rawServer == "" {
		return nil, "settings.gotify_error_server_url_required"
	}
	serverURL, err := service.NormalizePushServerURL(rawServer)
	if err != nil {
		return nil, "settings.push_error_server_url"
	}

	priority, ok := parsePriority(c.PostForm("gotify_priority"), service.GotifyDefaultPriority, 0, service.GotifyMaxPriority)
	if !ok {
		return nil, "settings.gotify_error_priority"
	}

	token := strings.TrimSpace(c.PostForm("gotify_app_token"))
	if token != "" {
		if service.ValidatePushToken(token) != nil {
			return nil, "settings.push_error_token_invalid"
		}
	} else if existing, err := h.service.GetGotifyConfig(); err == nil && existing.AppToken != "" {
		if !sameServer(existing.ServerURL, serverURL) {
			return nil, "settings.push_error_token_reenter"
		}
		token = existing.AppToken
	}
	if token == "" {
		return nil, "settings.gotify_error_token_required"
	}

	return &models.GotifyConfig{ServerURL: serverURL, AppToken: token, Priority: priority}, ""
}

// testFailed reports a failed test send. The error text names only the
// channel and the HTTP status or network failure, never URLs, tokens or
// provider response bodies.
func (h *SettingsHandler) testFailed(c *gin.Context, err error) {
	c.HTML(http.StatusBadRequest, "smtp-message.html", gin.H{
		"Error": h.tr("settings.push_test_failed") + " " + err.Error(),
		"Type":  "error",
	})
}

// SaveNtfySettings saves ntfy configuration.
func (h *SettingsHandler) SaveNtfySettings(c *gin.Context) {
	config, errKey := h.ntfyConfigFromForm(c)
	if errKey != "" {
		h.pushMessage(c, http.StatusBadRequest, errKey, "")
		return
	}
	if err := h.service.SaveNtfyConfig(config); err != nil {
		log.Printf("Failed to save ntfy settings: %v", err)
		h.pushMessage(c, http.StatusInternalServerError, "settings.push_error_save", "")
		return
	}
	h.pushMessage(c, http.StatusOK, "", "settings.ntfy_saved")
}

// TestNtfyConnection sends a test message with the submitted ntfy settings.
func (h *SettingsHandler) TestNtfyConnection(c *gin.Context) {
	config, errKey := h.ntfyConfigFromForm(c)
	if errKey != "" {
		h.pushMessage(c, http.StatusBadRequest, errKey, "")
		return
	}
	err := service.NewNtfyService(h.service).Send(config, pushTestTitle,
		"This is a test notification from SubTrackr. If you received this, your ntfy configuration is working correctly!")
	if err != nil {
		h.testFailed(c, err)
		return
	}
	h.pushMessage(c, http.StatusOK, "", "settings.ntfy_test_success")
}

// GetNtfyConfig returns the ntfy configuration without the access token.
func (h *SettingsHandler) GetNtfyConfig(c *gin.Context) {
	view, configured := ntfySettingsView(h.service)
	c.JSON(http.StatusOK, gin.H{
		"configured":       configured,
		"server_url":       view.ServerURL,
		"has_topic":        view.Topic != "",
		"has_access_token": view.HasAccessToken,
		"priority":         view.Priority,
	})
}

// SaveGotifySettings saves Gotify configuration.
func (h *SettingsHandler) SaveGotifySettings(c *gin.Context) {
	config, errKey := h.gotifyConfigFromForm(c)
	if errKey != "" {
		h.pushMessage(c, http.StatusBadRequest, errKey, "")
		return
	}
	if err := h.service.SaveGotifyConfig(config); err != nil {
		log.Printf("Failed to save Gotify settings: %v", err)
		h.pushMessage(c, http.StatusInternalServerError, "settings.push_error_save", "")
		return
	}
	h.pushMessage(c, http.StatusOK, "", "settings.gotify_saved")
}

// TestGotifyConnection sends a test message with the submitted Gotify settings.
func (h *SettingsHandler) TestGotifyConnection(c *gin.Context) {
	config, errKey := h.gotifyConfigFromForm(c)
	if errKey != "" {
		h.pushMessage(c, http.StatusBadRequest, errKey, "")
		return
	}
	err := service.NewGotifyService(h.service).Send(config, pushTestTitle,
		"This is a test notification from SubTrackr. If you received this, your Gotify configuration is working correctly!")
	if err != nil {
		h.testFailed(c, err)
		return
	}
	h.pushMessage(c, http.StatusOK, "", "settings.gotify_test_success")
}

// GetGotifyConfig returns the Gotify configuration without the app token.
func (h *SettingsHandler) GetGotifyConfig(c *gin.Context) {
	view, configured := gotifySettingsView(h.service)
	c.JSON(http.StatusOK, gin.H{
		"configured":    configured,
		"server_url":    view.ServerURL,
		"has_app_token": view.HasAppToken,
		"priority":      view.Priority,
	})
}

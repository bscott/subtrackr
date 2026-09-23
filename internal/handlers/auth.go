package handlers

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"subtrackr/internal/i18n"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

type OIDCAuthenticator interface {
	AuthorizationURL(ctx context.Context, redirectURI, state, nonce, codeVerifier string) (string, error)
	ExchangeAndVerify(ctx context.Context, redirectURI, code, codeVerifier, expectedNonce string) (*service.OIDCIdentity, error)
}

type AuthHandler struct {
	settingsService *service.SettingsService
	sessionService  *service.SessionService
	emailService    *service.EmailService
	i18nCatalog     *i18n.Catalog
	oidcService     OIDCAuthenticator
}

func NewAuthHandler(settingsService *service.SettingsService, sessionService *service.SessionService, emailService *service.EmailService, i18nCatalog *i18n.Catalog) *AuthHandler {
	return &AuthHandler{
		settingsService: settingsService,
		sessionService:  sessionService,
		emailService:    emailService,
		i18nCatalog:     i18nCatalog,
		oidcService:     service.NewOIDCService(settingsService, nil),
	}
}

// activeLang resolves the user-preferred language code, defaulting to "en" when unset
// or when the requested language has no loaded translations.
func (h *AuthHandler) activeLang() string {
	lang := h.settingsService.GetStringSettingWithDefault("lang", "en")
	if h.i18nCatalog != nil && !h.i18nCatalog.HasLanguage(lang) {
		return "en"
	}
	return lang
}

// isValidRedirect validates that a redirect URL is safe (relative URL only)
func isValidRedirect(redirect string) bool {
	if redirect == "" || len(redirect) > 2048 || strings.ContainsAny(redirect, "#\\\r\n") {
		return false
	}
	parsed, err := url.ParseRequestURI(redirect)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" {
		return false
	}
	return strings.HasPrefix(parsed.Path, "/") && !strings.HasPrefix(parsed.Path, "//")
}

// ShowLoginPage displays the login page
func (h *AuthHandler) ShowLoginPage(c *gin.Context) {
	// If already authenticated, redirect to dashboard
	if h.sessionService.IsAuthenticated(c.Request) {
		c.Redirect(http.StatusFound, "/")
		return
	}

	redirect := c.Query("redirect")
	if redirect == "" || !isValidRedirect(redirect) {
		redirect = "/"
	}
	oidcConfig, _ := h.settingsService.GetOIDCConfig()
	oidcDisplayName := "OpenID Connect"
	if oidcConfig != nil && strings.TrimSpace(oidcConfig.DisplayName) != "" {
		oidcDisplayName = strings.TrimSpace(oidcConfig.DisplayName)
	}

	c.HTML(http.StatusOK, "login.html", gin.H{
		"Redirect":         redirect,
		"Error":            c.Query("error"),
		"Lang":             h.activeLang(),
		"LocalAuthEnabled": h.settingsService.IsAuthEnabled(),
		"OIDCEnabled":      h.settingsService.IsOIDCEnabled(),
		"OIDCDisplayName":  oidcDisplayName,
	})
}

// Login handles login form submission
func (h *AuthHandler) Login(c *gin.Context) {
	if !h.settingsService.IsAuthEnabled() {
		c.HTML(http.StatusForbidden, "login-error.html", gin.H{
			"Error": "Local authentication is disabled",
		})
		return
	}

	username := c.PostForm("username")
	password := c.PostForm("password")
	rememberMe := c.PostForm("remember_me") == "on"
	redirect := c.PostForm("redirect")

	if redirect == "" || !isValidRedirect(redirect) {
		redirect = "/"
	}

	// Validate credentials using constant-time comparison to prevent timing attacks
	storedUsername, err := h.settingsService.GetAuthUsername()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "login-error.html", gin.H{
			"Error": "Authentication system error",
		})
		return
	}

	// Always validate password even for invalid usernames (constant time)
	validUsername := subtle.ConstantTimeCompare([]byte(storedUsername), []byte(username)) == 1

	var validPassword bool
	if err := h.settingsService.ValidatePassword(password); err == nil {
		validPassword = true
	}

	// Only fail after both checks to prevent username enumeration via timing
	if !validUsername || !validPassword {
		c.HTML(http.StatusUnauthorized, "login-error.html", gin.H{
			"Error": "Invalid username or password",
		})
		return
	}

	// Create session
	secureCookie := strings.HasPrefix(strings.ToLower(strings.TrimSpace(h.settingsService.GetBaseURL())), "https://")
	if err := h.sessionService.CreateSession(c.Writer, c.Request, rememberMe, secureCookie); err != nil {
		c.HTML(http.StatusInternalServerError, "login-error.html", gin.H{
			"Error": "Failed to create session",
		})
		return
	}

	// Redirect to original destination or dashboard
	c.Header("HX-Redirect", redirect)
	c.Status(http.StatusOK)
}

// OIDCLogin starts an OpenID Connect authorization-code flow.
func (h *AuthHandler) OIDCLogin(c *gin.Context) {
	if !h.settingsService.IsOIDCEnabled() {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("OIDC login is not configured"))
		return
	}

	redirect := c.Query("redirect")
	if redirect == "" || !isValidRedirect(redirect) {
		redirect = "/"
	}
	state, err := randomOIDCValue()
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Unable to start OIDC login"))
		return
	}
	nonce, err := randomOIDCValue()
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Unable to start OIDC login"))
		return
	}
	codeVerifier := oauth2.GenerateVerifier()
	baseURL := strings.TrimRight(h.settingsService.GetBaseURL(), "/")
	redirectURI := baseURL + "/auth/oidc/callback"
	secureCookie := strings.HasPrefix(strings.ToLower(baseURL), "https://")
	authorizationURL, err := h.oidcService.AuthorizationURL(c.Request.Context(), redirectURI, state, nonce, codeVerifier)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Unable to connect to the OIDC provider"))
		return
	}
	if err := h.sessionService.SaveOIDCFlow(c.Writer, c.Request, state, &service.OIDCFlow{
		Nonce:        nonce,
		CodeVerifier: codeVerifier,
		Redirect:     redirect,
	}, secureCookie); err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Unable to save OIDC login state"))
		return
	}
	c.Redirect(http.StatusFound, authorizationURL)
}

// OIDCCallback completes an OpenID Connect authorization-code flow.
func (h *AuthHandler) OIDCCallback(c *gin.Context) {
	baseURL := strings.TrimRight(h.settingsService.GetBaseURL(), "/")
	secureCookie := strings.HasPrefix(strings.ToLower(baseURL), "https://")
	flow, err := h.sessionService.ConsumeOIDCFlow(c.Writer, c.Request, c.Query("state"), secureCookie)
	if err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Invalid or expired OIDC login state"))
		return
	}
	if c.Query("error") != "" {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("OIDC login was denied"))
		return
	}

	redirectURI := baseURL + "/auth/oidc/callback"
	identity, err := h.oidcService.ExchangeAndVerify(
		c.Request.Context(),
		redirectURI,
		c.Query("code"),
		flow.CodeVerifier,
		flow.Nonce,
	)
	if err != nil || identity == nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("OIDC login could not be verified"))
		return
	}
	if err := h.sessionService.CreateSession(c.Writer, c.Request, false, secureCookie); err != nil {
		c.Redirect(http.StatusFound, "/login?error="+url.QueryEscape("Unable to create login session"))
		return
	}

	redirect := flow.Redirect
	if redirect == "" || !isValidRedirect(redirect) {
		redirect = "/"
	}
	c.Redirect(http.StatusFound, redirect)
}

func randomOIDCValue() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

// Logout handles logout
func (h *AuthHandler) Logout(c *gin.Context) {
	if err := h.sessionService.DestroySession(c.Writer, c.Request); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to logout"})
		return
	}

	c.Redirect(http.StatusFound, "/login")
}

// ShowForgotPasswordPage displays the forgot password page
func (h *AuthHandler) ShowForgotPasswordPage(c *gin.Context) {
	c.HTML(http.StatusOK, "forgot-password.html", gin.H{
		"Lang": h.activeLang(),
	})
}

// ForgotPassword handles forgot password request
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	// Generate reset token
	token, err := h.settingsService.GenerateResetToken()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "forgot-password-error.html", gin.H{
			"Error": "Failed to generate reset token",
		})
		return
	}

	// Check if SMTP is configured
	_, err = h.settingsService.GetSMTPConfig()
	if err != nil {
		c.HTML(http.StatusInternalServerError, "forgot-password-error.html", gin.H{
			"Error": "Email is not configured. Please contact administrator.",
		})
		return
	}

	// Build reset URL
	resetURL := buildBaseURL(c, h.settingsService.GetBaseURL()) + "/reset-password?token=" + url.QueryEscape(token)

	// Send reset email
	subject := "SubTrackr Password Reset"
	body := fmt.Sprintf(`
		<h2>Password Reset Request</h2>
		<p>You have requested to reset your SubTrackr password.</p>
		<p>Click the link below to reset your password:</p>
		<p><a href="%s">Reset Password</a></p>
		<p>This link will expire in 1 hour.</p>
		<p>If you did not request this reset, please ignore this email.</p>
	`, resetURL)

	err = h.emailService.SendEmail(subject, body)
	if err != nil {
		c.HTML(http.StatusInternalServerError, "forgot-password-error.html", gin.H{
			"Error": "Failed to send reset email: " + err.Error(),
		})
		return
	}

	c.HTML(http.StatusOK, "forgot-password-success.html", gin.H{
		"Message": "Password reset link has been sent to your email",
	})
}

// ShowResetPasswordPage displays the reset password page
func (h *AuthHandler) ShowResetPasswordPage(c *gin.Context) {
	token := c.Query("token")
	if token == "" {
		c.HTML(http.StatusBadRequest, "reset-password.html", gin.H{
			"Error": "Invalid reset token",
			"Lang":  h.activeLang(),
		})
		return
	}

	// Validate token
	if err := h.settingsService.ValidateResetToken(token); err != nil {
		c.HTML(http.StatusBadRequest, "reset-password.html", gin.H{
			"Error": "Invalid or expired reset token",
			"Lang":  h.activeLang(),
		})
		return
	}

	c.HTML(http.StatusOK, "reset-password.html", gin.H{
		"Token": token,
		"Lang":  h.activeLang(),
	})
}

// ResetPassword handles password reset
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	token := c.PostForm("token")
	newPassword := c.PostForm("new_password")
	confirmPassword := c.PostForm("confirm_password")

	// Validate password length FIRST (before checking if they match)
	if len(newPassword) < 8 {
		c.HTML(http.StatusBadRequest, "reset-password-error.html", gin.H{
			"Error": "Password must be at least 8 characters long",
		})
		return
	}

	// Then validate passwords match
	if newPassword != confirmPassword {
		c.HTML(http.StatusBadRequest, "reset-password-error.html", gin.H{
			"Error": "Passwords do not match",
		})
		return
	}

	// Validate token
	if err := h.settingsService.ValidateResetToken(token); err != nil {
		c.HTML(http.StatusBadRequest, "reset-password-error.html", gin.H{
			"Error": "Invalid or expired reset token",
		})
		return
	}

	// Update password
	if err := h.settingsService.SetAuthPassword(newPassword); err != nil {
		c.HTML(http.StatusInternalServerError, "reset-password-error.html", gin.H{
			"Error": "Failed to update password",
		})
		return
	}

	// Clear reset token
	h.settingsService.ClearResetToken()

	c.HTML(http.StatusOK, "reset-password-success.html", gin.H{
		"Message": "Password reset successfully. You can now login with your new password.",
	})
}

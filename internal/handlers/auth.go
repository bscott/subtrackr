package handlers

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"subtrackr/internal/service"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	settingsService *service.SettingsService
	sessionService  *service.SessionService
	emailService    *service.EmailService
}

func NewAuthHandler(settingsService *service.SettingsService, sessionService *service.SessionService, emailService *service.EmailService) *AuthHandler {
	return &AuthHandler{
		settingsService: settingsService,
		sessionService:  sessionService,
		emailService:    emailService,
	}
}

// isValidRedirect validates that a redirect URL is safe (relative URL only)
func isValidRedirect(redirect string) bool {
	// Check URL length to prevent DoS or log injection
	if len(redirect) > 2048 {
		return false
	}

	// Only allow relative URLs starting with / but not //
	// This prevents open redirect vulnerabilities
	if strings.HasPrefix(redirect, "/") && !strings.HasPrefix(redirect, "//") {
		return true
	}
	return false
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

	c.HTML(http.StatusOK, "login.html", gin.H{
		"Redirect": redirect,
		"Error":    c.Query("error"),
	})
}

// Login handles login form submission
func (h *AuthHandler) Login(c *gin.Context) {
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
	if err := h.sessionService.CreateSession(c.Writer, c.Request, rememberMe); err != nil {
		c.HTML(http.StatusInternalServerError, "login-error.html", gin.H{
			"Error": "Failed to create session",
		})
		return
	}

	// Redirect to original destination or dashboard
	c.Header("HX-Redirect", redirect)
	c.Status(http.StatusOK)
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
	c.HTML(http.StatusOK, "forgot-password.html", gin.H{})
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
		})
		return
	}

	// Validate token
	if err := h.settingsService.ValidateResetToken(token); err != nil {
		c.HTML(http.StatusBadRequest, "reset-password.html", gin.H{
			"Error": "Invalid or expired reset token",
		})
		return
	}

	c.HTML(http.StatusOK, "reset-password.html", gin.H{
		"Token": token,
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

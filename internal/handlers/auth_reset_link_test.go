package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type capturedEmail struct {
	subject string
	body    string
}

type fakeEmailSender struct {
	sent []capturedEmail
	err  error
}

func (f *fakeEmailSender) SendEmail(subject, body string) error {
	f.sent = append(f.sent, capturedEmail{subject: subject, body: body})
	return f.err
}

func newForgotPasswordTestRouter(t *testing.T, baseURL string) (*gin.Engine, *AuthHandler, *fakeEmailSender) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := repoRoot(t)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))

	settingsService := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settingsService.SaveSMTPConfig(&models.SMTPConfig{
		Host: "smtp.example.com", Port: 587, From: "subtrackr@example.com", To: "admin@example.com",
	}))
	if baseURL != "" {
		require.NoError(t, settingsService.SetBaseURL(baseURL))
	}

	handler := NewAuthHandler(settingsService, service.NewSessionService("test-secret-key-for-reset-link-test"), nil, nil)
	sender := &fakeEmailSender{}
	handler.emailService = sender

	router := gin.New()
	router.LoadHTMLFiles(
		filepath.Join(root, "templates", "forgot-password-error.html"),
		filepath.Join(root, "templates", "forgot-password-success.html"),
	)
	router.POST("/api/auth/forgot-password", handler.ForgotPassword)
	return router, handler, sender
}

func postForgotPassword(router *gin.Engine, host string, headers map[string]string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", nil)
	request.Host = host
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

var spoofedHostHeaders = map[string]string{
	"X-Forwarded-Host":  "attacker.example",
	"X-Forwarded-Proto": "https",
	"Forwarded":         "host=attacker.example;proto=https",
	"X-Original-Host":   "attacker.example",
}

func TestForgotPassword_IgnoresSpoofedHostHeadersWithoutBaseURL(t *testing.T) {
	router, handler, sender := newForgotPasswordTestRouter(t, "")

	response := postForgotPassword(router, "attacker.example", spoofedHostHeaders)

	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, sender.sent, 1)
	body := sender.sent[0].body
	assert.NotContains(t, body, "attacker.example")
	assert.NotContains(t, body, "href=")
	assert.Contains(t, body, "/reset-password?token=")

	// The emailed token must still be the valid one, so reset keeps working.
	token := extractResetToken(t, body)
	assert.NoError(t, handler.settingsService.ValidateResetToken(token))
}

func TestForgotPassword_UsesConfiguredBaseURLDespiteSpoofedHeaders(t *testing.T) {
	router, handler, sender := newForgotPasswordTestRouter(t, "https://subtrackr.example.com/")

	response := postForgotPassword(router, "attacker.example", spoofedHostHeaders)

	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, sender.sent, 1)
	body := sender.sent[0].body
	assert.NotContains(t, body, "attacker.example")
	assert.Contains(t, body, `href="https://subtrackr.example.com/reset-password?token=`)

	token := extractResetToken(t, body)
	assert.NoError(t, handler.settingsService.ValidateResetToken(token))
}

func TestForgotPassword_NoEmailSenderDoesNotPanic(t *testing.T) {
	router, handler, _ := newForgotPasswordTestRouter(t, "")
	handler.emailService = nil

	response := postForgotPassword(router, "subtrackr.local", nil)

	assert.Equal(t, http.StatusInternalServerError, response.Code)
}

func TestNewAuthHandler_NilEmailServiceLeavesSenderUnset(t *testing.T) {
	handler := NewAuthHandler(nil, nil, nil, nil)

	assert.Nil(t, handler.emailService)
}

func TestForgotPassword_SendFailureHidesSMTPDetails(t *testing.T) {
	router, _, sender := newForgotPasswordTestRouter(t, "https://subtrackr.example.com")
	sender.err = errors.New("dial tcp 10.0.0.25:587: connection refused")

	response := postForgotPassword(router, "subtrackr.example.com", nil)

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NotContains(t, response.Body.String(), "10.0.0.25")
}

func TestForgotPassword_UnconfiguredSMTPKeepsExistingToken(t *testing.T) {
	router, handler, _ := newForgotPasswordTestRouter(t, "")
	existing, err := handler.settingsService.GenerateResetToken()
	require.NoError(t, err)
	handler.emailService = nil

	response := postForgotPassword(router, "subtrackr.local", nil)

	assert.Equal(t, http.StatusInternalServerError, response.Code)
	assert.NoError(t, handler.settingsService.ValidateResetToken(existing))
}

func TestPasswordResetEmailBody_EscapesConfiguredBaseURL(t *testing.T) {
	body := passwordResetEmailBody(`https://example.com/"><script>alert(1)</script>`, "tok")

	assert.NotContains(t, body, "<script>")
	assert.Contains(t, body, "&lt;script&gt;")
}

func TestPasswordResetEmailBody_BlankBaseURLFallsBackToRelativePath(t *testing.T) {
	body := passwordResetEmailBody("   ", "a+b/c")

	assert.NotContains(t, body, "href=")
	assert.Contains(t, body, "<code>/reset-password?token=a%2Bb%2Fc</code>")
	// The token appears exactly once, never attached to an example host that
	// a mail client could auto-link.
	assert.Equal(t, 1, strings.Count(body, "a%2Bb%2Fc"))
}

// extractResetToken pulls the token out of the first /reset-password?token=
// occurrence in an email body.
func extractResetToken(t *testing.T, body string) string {
	t.Helper()
	const marker = "/reset-password?token="
	start := strings.Index(body, marker)
	require.NotEqual(t, -1, start, "email body has no reset path: %s", body)
	rest := body[start+len(marker):]
	end := strings.IndexAny(rest, `"<`)
	require.NotEqual(t, -1, end)
	token, err := url.QueryUnescape(rest[:end])
	require.NoError(t, err)
	return token
}

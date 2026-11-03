package service

import (
	"io"
	"mime"
	"net/mail"
	"strings"
	"subtrackr/internal/models"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSMTPConfig() *models.SMTPConfig {
	return &models.SMTPConfig{From: "subtrackr@example.com", To: "admin@example.com", FromName: "SubTrackr"}
}

// headersOf parses a rendered message and returns its headers and body.
func headersOf(t *testing.T, message string) (mail.Header, string) {
	t.Helper()
	parsed, err := mail.ReadMessage(strings.NewReader(message))
	require.NoError(t, err)
	body, err := io.ReadAll(parsed.Body)
	require.NoError(t, err)
	return parsed.Header, string(body)
}

func TestBuildEmailMessage_SubjectCannotInjectHeadersOrBody(t *testing.T) {
	// A subscription name planted through the API, as used in alert subjects.
	name := "Netflix\r\nBcc: victim@example.com\r\nContent-Type: text/plain\r\n\r\n<a href=https://attacker.example>Verify account</a>"
	subject := "High Cost Alert: " + name

	message := buildEmailMessage(testSMTPConfig(), subject, "<p>real body</p>")
	header, body := headersOf(t, message)

	assert.Empty(t, header.Get("Bcc"))
	assert.Equal(t, "text/html; charset=UTF-8", header.Get("Content-Type"))
	assert.Equal(t, "<p>real body</p>", body)
	decoded, err := new(mime.WordDecoder).DecodeHeader(header.Get("Subject"))
	require.NoError(t, err)
	assert.NotContains(t, decoded, "\r")
	assert.NotContains(t, decoded, "\n")
	assert.Contains(t, decoded, "Netflix")
}

func TestBuildEmailMessage_EncodesNonASCIISubject(t *testing.T) {
	message := buildEmailMessage(testSMTPConfig(), "Renewal Reminder: Café Société renews in 3 days", "x")
	header, _ := headersOf(t, message)

	assert.True(t, strings.HasPrefix(header.Get("Subject"), "=?utf-8?q?"), header.Get("Subject"))
	decoded, err := new(mime.WordDecoder).DecodeHeader(header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Renewal Reminder: Café Société renews in 3 days", decoded)
}

func TestBuildEmailMessage_ASCIISubjectUnchanged(t *testing.T) {
	message := buildEmailMessage(testSMTPConfig(), "Renewal Reminder: Netflix renews in 3 days", "x")
	header, _ := headersOf(t, message)

	assert.Equal(t, "Renewal Reminder: Netflix renews in 3 days", header.Get("Subject"))
	assert.Equal(t, "SubTrackr <subtrackr@example.com>", header.Get("From"))
	assert.Equal(t, "admin@example.com", header.Get("To"))
}

func TestBuildEmailMessage_SanitizesConfiguredAddresses(t *testing.T) {
	config := testSMTPConfig()
	config.FromName = "Sub\r\nX-Injected: 1"
	config.To = "admin@example.com\r\nX-Injected: 2"

	header, _ := headersOf(t, buildEmailMessage(config, "s", "x"))

	assert.Empty(t, header.Get("X-Injected"))
}

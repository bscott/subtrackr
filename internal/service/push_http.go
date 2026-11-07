package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Defaults shared by the JSON push channels (ntfy and Gotify).
const (
	pushRequestTimeout   = 10 * time.Second
	pushMaxRetries       = 3
	pushInitialBackoff   = 500 * time.Millisecond
	pushMaxResponseBytes = 64 << 10
	pushMaxTokenLength   = 512
	pushUserAgent        = "SubTrackr-Notifier/1.0"
)

// ntfyTopicPattern is the topic syntax ntfy accepts: letters, digits,
// underscores and dashes, at most 64 characters.
var ntfyTopicPattern = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// Validation errors for push channel settings. Their messages never include
// the rejected value, which may contain a credential.
var (
	ErrPushServerURLInvalid = errors.New("server URL must be an absolute http:// or https:// URL without credentials, query parameters or fragments")
	ErrNtfyTopicInvalid     = errors.New("topic may only contain letters, digits, underscores and dashes, up to 64 characters")
	ErrPushTokenInvalid     = errors.New("token must be at most 512 printable characters without spaces")
)

// NormalizePushServerURL validates a self-hostable push server address and
// returns it without a trailing slash. Plain HTTP is accepted so LAN servers
// work. Embedded credentials are rejected so secrets only travel in headers
// and never in URLs, which end up in logs and error messages.
func NormalizePushServerURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, " \t\r\n") {
		return "", ErrPushServerURLInvalid
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrPushServerURLInvalid
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", ErrPushServerURLInvalid
	}
	if port := u.Port(); port != "" {
		if _, err := net.LookupPort("tcp", port); err != nil {
			return "", ErrPushServerURLInvalid
		}
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return strings.TrimRight(u.String(), "/"), nil
}

// ValidateNtfyTopic reports whether topic is a valid ntfy topic name.
func ValidateNtfyTopic(topic string) error {
	if !ntfyTopicPattern.MatchString(topic) {
		return ErrNtfyTopicInvalid
	}
	return nil
}

// ValidatePushToken rejects tokens that could not be sent as a header value
// verbatim (whitespace, control or non-ASCII characters) or are implausibly long.
func ValidatePushToken(token string) error {
	if len(token) > pushMaxTokenLength {
		return ErrPushTokenInvalid
	}
	for i := 0; i < len(token); i++ {
		if token[i] <= ' ' || token[i] > '~' {
			return ErrPushTokenInvalid
		}
	}
	return nil
}

// pushClient sends JSON notifications with retry. It never follows redirects:
// Go forwards custom headers such as X-Gotify-Key to the redirect target, so a
// redirect could hand a credential to another host.
type pushClient struct {
	name           string
	httpClient     *http.Client
	maxRetries     int
	initialBackoff time.Duration
}

func newPushClient(name string) pushClient {
	return pushClient{
		name: name,
		httpClient: &http.Client{
			Timeout: pushRequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		maxRetries:     pushMaxRetries,
		initialBackoff: pushInitialBackoff,
	}
}

// postJSON marshals payload and POSTs it to endpoint, retrying connection
// failures, HTTP 429 and 5xx responses with exponential backoff. Timeouts are
// not retried: the server may already have accepted the message, and a retry
// would duplicate it. Errors carry only the channel name, status code or
// transport failure: never the URL, headers or response body, which may echo
// submitted data or secrets.
func (c pushClient) postJSON(endpoint string, headers map[string]string, payload any) error {
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return fmt.Errorf("%s: failed to encode message: %w", c.name, err)
	}

	attempts := c.maxRetries
	if attempts < 1 {
		attempts = 1
	}
	backoff := c.initialBackoff
	if backoff <= 0 {
		backoff = pushInitialBackoff
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		retryable, err := c.doPost(endpoint, headers, body.Bytes())
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == attempts {
			break
		}
		time.Sleep(backoff)
		backoff *= 2
	}
	return lastErr
}

func (c pushClient) doPost(endpoint string, headers map[string]string, body []byte) (retryable bool, err error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("%s: invalid server URL", c.name)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", pushUserAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return !isTimeout(err), fmt.Errorf("%s: %s", c.name, describeTransportError(err))
	}
	defer resp.Body.Close()
	// Bounded read; the body is only inspected, never surfaced.
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, pushMaxResponseBytes))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if !acknowledgesMessage(respBody) {
			return false, fmt.Errorf("%s: server did not acknowledge the message; check the server URL", c.name)
		}
		return false, nil
	}
	retryable = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return false, fmt.Errorf("%s: server rejected the credentials (HTTP %d)", c.name, resp.StatusCode)
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, fmt.Errorf("%s: server answered with a redirect (HTTP %d); enter the final server URL", c.name, resp.StatusCode)
	default:
		return retryable, fmt.Errorf("%s: server returned HTTP %d", c.name, resp.StatusCode)
	}
}

// acknowledgesMessage reports whether a 2xx body is the created message that
// ntfy and Gotify both return, which always has an id. Anything else, such as
// a proxy login page answering 200, means the message was not delivered.
func acknowledgesMessage(body []byte) bool {
	var ack struct {
		ID any `json:"id"`
	}
	return json.Unmarshal(body, &ack) == nil && ack.ID != nil && ack.ID != ""
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// describeTransportError strips the request URL that *url.Error embeds and
// reports the underlying network failure.
func describeTransportError(err error) string {
	if isTimeout(err) {
		return "request timed out"
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	return "request failed: " + err.Error()
}

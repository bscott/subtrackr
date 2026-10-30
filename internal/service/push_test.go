package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// capturedRequest is what a local sink saw for one request.
type capturedRequest struct {
	Method string
	Path   string
	Query  string
	Header http.Header
	Body   map[string]any
}

// pushSink is a local HTTP server standing in for ntfy or Gotify. Its
// statuses are scripted per request; once the script runs out it answers 200.
// Successes acknowledge with a message id, failures answer with body.
// Only POSTs are recorded or consume a status: local tools may probe a newly
// opened port with GET, and every notification is a POST.
type pushSink struct {
	*httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
	statuses []int
	body     string
}

func newPushSink(t *testing.T, statuses ...int) *pushSink {
	t.Helper()
	sink := &pushSink{statuses: statuses, body: `{"error":"failed"}`}
	sink.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		sink.mu.Lock()
		sink.requests = append(sink.requests, capturedRequest{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), body})
		status := http.StatusOK
		if len(sink.statuses) > 0 {
			status, sink.statuses = sink.statuses[0], sink.statuses[1:]
		}
		respBody := sink.body
		if status < 300 {
			respBody = `{"id":1}`
		}
		sink.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(sink.Close)
	return sink
}

func (s *pushSink) all() []capturedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]capturedRequest(nil), s.requests...)
}

func newPushTestSettings(t *testing.T) *SettingsService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}, &models.Category{}))
	return NewSettingsService(repository.NewSettingsRepository(db))
}

func fastNtfy(settings *SettingsService) *NtfyService {
	n := NewNtfyService(settings)
	n.client.initialBackoff = time.Millisecond
	return n
}

func fastGotify(settings *SettingsService) *GotifyService {
	g := NewGotifyService(settings)
	g.client.initialBackoff = time.Millisecond
	return g
}

func TestNormalizePushServerURL(t *testing.T) {
	valid := map[string]string{
		"https://ntfy.sh":                 "https://ntfy.sh",
		"https://ntfy.sh/":                "https://ntfy.sh",
		"  http://192.168.1.10:8080/  ":   "http://192.168.1.10:8080",
		"HTTP://Gotify.LAN:80/push/":      "http://gotify.lan:80/push",
		"http://[fd00::1]:2586":           "http://[fd00::1]:2586",
		"https://home.example.com/ntfy//": "https://home.example.com/ntfy",
	}
	for in, want := range valid {
		got, err := NormalizePushServerURL(in)
		if assert.NoError(t, err, in) {
			assert.Equal(t, want, got, in)
		}
	}

	invalid := []string{
		"", "   ", "ntfy.sh", "//ntfy.sh", "ftp://ntfy.sh", "javascript:alert(1)", "http://",
		"http://user:hunter2@ntfy.example", "https://:tk_secret@ntfy.example",
		"https://ntfy.sh/?auth=tk_secret", "https://ntfy.sh/?", "https://ntfy.sh/#frag",
		"http://host:99999", "http://host:port", "http://ho st", "http://host\n/x", "mailto:a@b.c",
	}
	for _, in := range invalid {
		_, err := NormalizePushServerURL(in)
		if assert.ErrorIs(t, err, ErrPushServerURLInvalid, in) {
			assert.NotContains(t, err.Error(), "hunter2")
			assert.NotContains(t, err.Error(), "tk_secret")
		}
	}
}

func TestValidateNtfyTopicAndToken(t *testing.T) {
	for _, ok := range []string{"a", "subtrackr_alerts-01", strings.Repeat("x", 64)} {
		assert.NoError(t, ValidateNtfyTopic(ok), ok)
	}
	for _, bad := range []string{"", strings.Repeat("x", 65), "with space", "slash/topic", "ümlaut", "a?b", "../x"} {
		assert.ErrorIs(t, ValidateNtfyTopic(bad), ErrNtfyTopicInvalid, bad)
	}

	for _, ok := range []string{"", "tk_AgQdq7mVBoFD37zQVN29RhuMzNIz2", "AbCdEf.123-_~"} {
		assert.NoError(t, ValidatePushToken(ok), ok)
	}
	for _, bad := range []string{"tk with space", "tk\r\nX-Injected: 1", "tök", "\x00", strings.Repeat("a", 513)} {
		assert.ErrorIs(t, ValidatePushToken(bad), ErrPushTokenInvalid)
	}
}

func TestNtfySend_PublishesJSONToServerRootWithBearerToken(t *testing.T) {
	sink := newPushSink(t)
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBaseURL("https://subtrackr.example.com/"))
	n := fastNtfy(settings)

	title := `Renewal: Ünïcødé 🎬 *bold* _x_ [y](z) <b>&amp;`
	message := "Line 1 \"quoted\"\nLine 2 `code` # not a heading \\ back"
	err := n.Send(&models.NtfyConfig{ServerURL: sink.URL + "/", Topic: "subtrackr_e2e", AccessToken: "tk_test123", Priority: 4}, title, message)
	require.NoError(t, err)

	reqs := sink.all()
	require.Len(t, reqs, 1)
	r := reqs[0]
	assert.Equal(t, http.MethodPost, r.Method)
	assert.Equal(t, "/", r.Path, "JSON publishing must target the server root, not the topic URL")
	assert.Empty(t, r.Query)
	assert.Equal(t, "Bearer tk_test123", r.Header.Get("Authorization"))
	assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
	assert.Equal(t, "subtrackr_e2e", r.Body["topic"])
	assert.Equal(t, title, r.Body["title"], "title must arrive verbatim (plain text, no escaping)")
	assert.Equal(t, message, r.Body["message"])
	assert.Equal(t, float64(4), r.Body["priority"])
	assert.Equal(t, "https://subtrackr.example.com", r.Body["click"])
	_, hasMarkdown := r.Body["markdown"]
	assert.False(t, hasMarkdown, "messages are plain text")
}

func TestNtfySend_DefaultsAndNoTokenHeader(t *testing.T) {
	sink := newPushSink(t)
	n := fastNtfy(newPushTestSettings(t))

	// A sub-path deployment publishes to the sub-path root.
	require.NoError(t, n.Send(&models.NtfyConfig{ServerURL: sink.URL + "/ntfy", Topic: "t1"}, "T", "M"))
	r := sink.all()[0]
	assert.Equal(t, "/ntfy/", r.Path)
	assert.Empty(t, r.Header.Get("Authorization"))
	assert.Equal(t, float64(NtfyDefaultPriority), r.Body["priority"], "unset priority uses ntfy's default")
	_, hasClick := r.Body["click"]
	assert.False(t, hasClick, "no click target without a base URL")
}

func TestNtfySend_RejectsInvalidConfigWithoutRequest(t *testing.T) {
	sink := newPushSink(t)
	n := fastNtfy(newPushTestSettings(t))
	cases := []*models.NtfyConfig{
		nil,
		{ServerURL: sink.URL},
		{ServerURL: sink.URL, Topic: "bad topic"},
		{ServerURL: "http://user:pw@" + strings.TrimPrefix(sink.URL, "http://"), Topic: "t"},
		{ServerURL: sink.URL, Topic: "t", AccessToken: "tk\nInjected: yes"},
	}
	for _, cfg := range cases {
		assert.Error(t, n.Send(cfg, "T", "M"))
	}
	assert.Empty(t, sink.all())
}

func TestGotifySend_PostsMessageWithHeaderToken(t *testing.T) {
	sink := newPushSink(t)
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBaseURL("http://subtrackr.lan:8080"))
	g := fastGotify(settings)

	title := "Ünïcødé 🎬 *x*"
	message := "Body with **markdown** and <html> & \"quotes\""
	require.NoError(t, g.Send(&models.GotifyConfig{ServerURL: sink.URL + "/gotify/", AppToken: "AppTok.123", Priority: 0}, title, message))

	r := sink.all()[0]
	assert.Equal(t, http.MethodPost, r.Method)
	assert.Equal(t, "/gotify/message", r.Path)
	assert.Empty(t, r.Query, "the app token must never be sent in the query string")
	assert.Equal(t, "AppTok.123", r.Header.Get("X-Gotify-Key"))
	assert.Empty(t, r.Header.Get("Authorization"))
	assert.Equal(t, title, r.Body["title"])
	assert.Equal(t, message, r.Body["message"])
	assert.Equal(t, float64(0), r.Body["priority"], "priority 0 is a valid Gotify priority and must be sent")
	extras := r.Body["extras"].(map[string]any)
	assert.Equal(t, map[string]any{"contentType": "text/plain"}, extras["client::display"])
	assert.Equal(t, map[string]any{"click": map[string]any{"url": "http://subtrackr.lan:8080"}}, extras["client::notification"])
}

func TestGotifySend_RequiresServerAndToken(t *testing.T) {
	g := fastGotify(newPushTestSettings(t))
	assert.ErrorIs(t, g.Send(&models.GotifyConfig{ServerURL: "http://gotify.lan"}, "T", "M"), ErrGotifyNotConfigured)
	assert.ErrorIs(t, g.Send(&models.GotifyConfig{AppToken: "x"}, "T", "M"), ErrGotifyNotConfigured)
	assert.ErrorIs(t, g.SendNotification("T", "M"), ErrGotifyNotConfigured, "nothing saved")
}

func TestPushFailures_AreSanitizedAndRetriedOnlyWhenTransient(t *testing.T) {
	const secret = "tk_SUPERSECRET"
	tests := []struct {
		name      string
		statuses  []int
		wantErr   string
		wantCalls int
	}{
		{"unauthorized is not retried", []int{401}, "rejected the credentials (HTTP 401)", 1},
		{"forbidden is not retried", []int{403}, "rejected the credentials (HTTP 403)", 1},
		{"bad request is not retried", []int{400}, "server returned HTTP 400", 1},
		{"server error retried until exhausted", []int{500, 502, 503}, "server returned HTTP 503", 3},
		{"rate limit then success", []int{429}, "", 2},
		{"server error then success", []int{503, 500}, "", 3},
		{"redirect is never followed", []int{302}, "redirect (HTTP 302)", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, channel := range []string{"ntfy", "gotify"} {
				sink := newPushSink(t, tt.statuses...)
				// Providers may echo request data in their error bodies; it must not leak.
				sink.body = `{"error":"echo ` + secret + ` and topic"}`
				settings := newPushTestSettings(t)
				var err error
				if channel == "ntfy" {
					err = fastNtfy(settings).Send(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t", AccessToken: secret}, "T", "M")
				} else {
					err = fastGotify(settings).Send(&models.GotifyConfig{ServerURL: sink.URL, AppToken: secret}, "T", "M")
				}
				assert.Len(t, sink.all(), tt.wantCalls, channel)
				if tt.wantErr == "" {
					assert.NoError(t, err, channel)
					continue
				}
				require.Error(t, err, channel)
				assert.Contains(t, err.Error(), tt.wantErr, channel)
				assert.NotContains(t, err.Error(), secret, channel)
				assert.NotContains(t, err.Error(), "echo", channel)
				assert.NotContains(t, err.Error(), sink.URL, channel)
			}
		})
	}
}

func TestPushRedirect_DoesNotForwardCredentialToAnotherHost(t *testing.T) {
	var leaked atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			leaked.Add(1)
		}
	}))
	defer other.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/message", http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()

	err := fastGotify(newPushTestSettings(t)).Send(&models.GotifyConfig{ServerURL: redirector.URL, AppToken: "AppSecret"}, "T", "M")
	require.Error(t, err)
	assert.Zero(t, leaked.Load(), "redirect target must never receive the request")
}

func TestPushTimeoutAndConnectionErrors(t *testing.T) {
	var attempts atomic.Int32
	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			attempts.Add(1)
		}
		<-block
	}))
	defer slow.Close()
	defer close(block)

	n := fastNtfy(newPushTestSettings(t))
	n.client.httpClient.Timeout = 50 * time.Millisecond
	n.client.maxRetries = 2
	start := time.Now()
	err := n.Send(&models.NtfyConfig{ServerURL: slow.URL, Topic: "t", AccessToken: "tk_secret"}, "T", "M")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ntfy: request timed out")
	assert.NotContains(t, err.Error(), slow.URL)
	assert.Less(t, time.Since(start), 5*time.Second)
	assert.EqualValues(t, 1, attempts.Load(), "a timed-out message may have been accepted, so it is not retried")

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	g := fastGotify(newPushTestSettings(t))
	err = g.Send(&models.GotifyConfig{ServerURL: closedURL, AppToken: "AppSecret"}, "T", "M")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Gotify: request failed")
	assert.NotContains(t, err.Error(), "/message")
	assert.NotContains(t, err.Error(), "AppSecret")
}

func TestPushSend_RequiresAcknowledgement(t *testing.T) {
	for _, body := range []string{"", "<html>login</html>", "{}", `{"id":""}`, `{"id":null}`, "[]"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		settings := newPushTestSettings(t)
		err := fastNtfy(settings).Send(&models.NtfyConfig{ServerURL: server.URL, Topic: "t"}, "T", "M")
		assert.ErrorContains(t, err, "did not acknowledge", "body %q", body)
		err = fastGotify(settings).Send(&models.GotifyConfig{ServerURL: server.URL, AppToken: "tok"}, "T", "M")
		assert.ErrorContains(t, err, "did not acknowledge", "body %q", body)
		server.Close()
	}
	// ntfy ids are strings and Gotify ids are numbers.
	for _, body := range []string{`{"id":"sPs71M8A2T"}`, `{"id":25,"appid":5}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, body)
		}))
		err := fastNtfy(newPushTestSettings(t)).Send(&models.NtfyConfig{ServerURL: server.URL, Topic: "t"}, "T", "M")
		assert.NoError(t, err, "body %q", body)
		server.Close()
	}
}

func pushTestSubscription() *models.Subscription {
	renewal := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cancel := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	return &models.Subscription{
		ID: 7, Name: "Ünïcødé *Stream* 🎬", Cost: 1234.5, OriginalCurrency: "EUR", Schedule: "Monthly",
		Status: "Active", RenewalDate: &renewal, CancellationDate: &cancel, URL: "https://example.com/a_b",
		Category: models.Category{Name: "Video & Music"},
	}
}

// TestPushChannels_CategoryMessagesMatchSharedText checks that ntfy and
// Gotify send the same title and body as the other push channels for every
// message category, with the configured date and currency formatting.
func TestPushChannels_CategoryMessagesMatchSharedText(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetCurrency("USD"))
	require.NoError(t, settings.SetDateFormat("DD/MM/YYYY"))
	for _, key := range []string{"renewal_reminders", "cancellation_reminders", "high_cost_alerts"} {
		require.NoError(t, settings.SetBoolSetting(key, true))
	}
	sub := pushTestSubscription()

	ntfySink := newPushSink(t)
	gotifySink := newPushSink(t)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: ntfySink.URL, Topic: "t", Priority: 5}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: gotifySink.URL, AppToken: "tok", Priority: 8}))
	channels := []NotificationChannel{fastNtfy(settings), fastGotify(settings)}

	renewal := buildRenewalReminderContent(sub, 3, settings)
	highTitle, highBody := highCostAlertText(sub, settings)
	cancelTitle, cancelBody := cancellationReminderText(sub, 1, settings)
	want := [][2]string{
		{renewal.Title, renewalReminderMessage(sub, renewal, settings)},
		{cancelTitle, cancelBody},
		{highTitle, highBody},
	}
	for _, ch := range channels {
		require.NoError(t, ch.SendRenewalReminder(sub, 3))
		require.NoError(t, ch.SendCancellationReminder(sub, 1))
		require.NoError(t, ch.SendHighCostAlert(sub))
	}

	for _, sink := range []*pushSink{ntfySink, gotifySink} {
		reqs := sink.all()
		require.Len(t, reqs, 3)
		for i, w := range want {
			assert.Equal(t, w[0], reqs[i].Body["title"])
			assert.Equal(t, w[1], reqs[i].Body["message"])
		}
	}
	assert.Equal(t, float64(5), ntfySink.all()[0].Body["priority"])
	assert.Equal(t, float64(8), gotifySink.all()[0].Body["priority"])

	// The shared text carries the subscription's own currency and the long date format.
	assert.Contains(t, highBody, "€1234.50 Monthly")
	assert.Contains(t, highBody, "Next Renewal: 3 November 2026")
	assert.Contains(t, cancelBody, "will end in 1 day.")
	assert.Contains(t, cancelBody, "Cancellation Date: 20 November 2026")
	assert.Contains(t, highTitle, "Ünïcødé *Stream* 🎬", "plain text keeps names verbatim")
}

func TestPushChannels_DisabledCategoriesSendNothing(t *testing.T) {
	settings := newPushTestSettings(t)
	require.NoError(t, settings.SetBoolSetting("high_cost_alerts", false))
	sink := newPushSink(t)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t"}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: sink.URL, AppToken: "tok"}))
	sub := pushTestSubscription()
	for _, ch := range []NotificationChannel{fastNtfy(settings), fastGotify(settings)} {
		assert.NoError(t, ch.SendRenewalReminder(sub, 3))
		assert.NoError(t, ch.SendCancellationReminder(sub, 3))
		assert.NoError(t, ch.SendHighCostAlert(sub))
	}
	assert.Empty(t, sink.all())
}

func TestPushConfigRoundTripAndIsConfigured(t *testing.T) {
	settings := newPushTestSettings(t)
	n, g := NewNtfyService(settings), NewGotifyService(settings)
	assert.False(t, n.IsConfigured())
	assert.False(t, g.IsConfigured())

	ntfyCfg := &models.NtfyConfig{ServerURL: "http://10.0.0.5:2586", Topic: "alerts", AccessToken: "tk_x", Priority: 2}
	gotifyCfg := &models.GotifyConfig{ServerURL: "https://gotify.example.com", AppToken: "Aabc", Priority: 0}
	require.NoError(t, settings.SaveNtfyConfig(ntfyCfg))
	require.NoError(t, settings.SaveGotifyConfig(gotifyCfg))

	gotNtfy, err := settings.GetNtfyConfig()
	require.NoError(t, err)
	assert.Equal(t, ntfyCfg, gotNtfy)
	gotGotify, err := settings.GetGotifyConfig()
	require.NoError(t, err)
	assert.Equal(t, gotifyCfg, gotGotify)
	assert.True(t, n.IsConfigured())
	assert.True(t, g.IsConfigured())
}

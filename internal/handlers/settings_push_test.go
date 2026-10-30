package handlers

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"subtrackr/internal/i18n"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newPushSettingsRouter(t *testing.T) (*gin.Engine, *service.SettingsService, *i18n.Catalog) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settings := service.NewSettingsService(repository.NewSettingsRepository(db))
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(repoRoot(t), "web", "locales")))
	handler := NewSettingsHandler(settings, catalog)

	router := gin.New()
	router.LoadHTMLFiles(filepath.Join(repoRoot(t), "templates", "smtp-message.html"))
	router.POST("/api/settings/ntfy", handler.SaveNtfySettings)
	router.POST("/api/settings/ntfy/test", handler.TestNtfyConnection)
	router.GET("/api/settings/ntfy", handler.GetNtfyConfig)
	router.POST("/api/settings/gotify", handler.SaveGotifySettings)
	router.POST("/api/settings/gotify/test", handler.TestGotifyConnection)
	router.GET("/api/settings/gotify", handler.GetGotifyConfig)
	return router, settings, catalog
}

func postPushForm(router *gin.Engine, path string, form url.Values) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

// pushTestSink records POSTs on a local server and answers with status. Other
// methods are ignored: local tools may probe a newly opened port with GET.
type pushTestSink struct {
	*httptest.Server
	mu       sync.Mutex
	requests []*http.Request
	bodies   []map[string]any
}

func newPushTestSink(t *testing.T, status int, respBody string) *pushTestSink {
	t.Helper()
	sink := &pushTestSink{}
	sink.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		sink.mu.Lock()
		sink.requests = append(sink.requests, r.Clone(r.Context()))
		sink.bodies = append(sink.bodies, body)
		sink.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(sink.Close)
	return sink
}

func (s *pushTestSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func TestSaveNtfy_DefaultsServerAndNormalizes(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)

	resp := postPushForm(router, "/api/settings/ntfy", url.Values{"ntfy_topic": {" alerts_1 "}})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.ntfy_saved"))

	saved, err := settings.GetNtfyConfig()
	require.NoError(t, err)
	assert.Equal(t, &models.NtfyConfig{ServerURL: service.NtfyDefaultServerURL, Topic: "alerts_1", Priority: service.NtfyDefaultPriority}, saved)

	resp = postPushForm(router, "/api/settings/ntfy", url.Values{
		"ntfy_server_url": {"HTTP://LAN-box:2586/"}, "ntfy_topic": {"alerts_1"}, "ntfy_priority": {"5"}, "ntfy_access_token": {"tk_abc"},
	})
	assert.Equal(t, http.StatusOK, resp.Code)
	saved, _ = settings.GetNtfyConfig()
	assert.Equal(t, &models.NtfyConfig{ServerURL: "http://lan-box:2586", Topic: "alerts_1", AccessToken: "tk_abc", Priority: 5}, saved)
}

func TestSaveNtfy_BlankTokenKeepsSecretOnlyForSameServer(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)
	stored := &models.NtfyConfig{ServerURL: "https://ntfy.example.com", Topic: "old", AccessToken: "tk_keep", Priority: 2}
	require.NoError(t, settings.SaveNtfyConfig(stored))

	// Same server (after normalization): token preserved, other fields updated.
	resp := postPushForm(router, "/api/settings/ntfy", url.Values{
		"ntfy_server_url": {"https://ntfy.example.com/"}, "ntfy_topic": {"new"}, "ntfy_priority": {"4"},
	})
	assert.Equal(t, http.StatusOK, resp.Code)
	saved, _ := settings.GetNtfyConfig()
	assert.Equal(t, "tk_keep", saved.AccessToken)
	assert.Equal(t, "new", saved.Topic)
	assert.Equal(t, 4, saved.Priority)

	// Different server: the stored token is not carried over.
	resp = postPushForm(router, "/api/settings/ntfy", url.Values{
		"ntfy_server_url": {"https://evil.example.com"}, "ntfy_topic": {"new"},
	})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.push_error_token_reenter"))
	saved, _ = settings.GetNtfyConfig()
	assert.Equal(t, "https://ntfy.example.com", saved.ServerURL, "rejected save must not change stored config")

	// A new token for the new server is accepted.
	resp = postPushForm(router, "/api/settings/ntfy", url.Values{
		"ntfy_server_url": {"https://other.example.com"}, "ntfy_topic": {"new"}, "ntfy_access_token": {"tk_new"},
	})
	assert.Equal(t, http.StatusOK, resp.Code)
	saved, _ = settings.GetNtfyConfig()
	assert.Equal(t, "tk_new", saved.AccessToken)

	// The clear checkbox removes it, also when moving to another server.
	resp = postPushForm(router, "/api/settings/ntfy", url.Values{
		"ntfy_server_url": {"https://public.example.com"}, "ntfy_topic": {"new"}, "ntfy_clear_token": {"on"},
	})
	assert.Equal(t, http.StatusOK, resp.Code)
	saved, _ = settings.GetNtfyConfig()
	assert.Empty(t, saved.AccessToken)
}

func TestPushForms_ValidationRejectsWithoutEchoingInput(t *testing.T) {
	const secret = "hunter2SECRET"
	router, settings, catalog := newPushSettingsRouter(t)
	tests := []struct {
		name string
		path string
		form url.Values
		key  string
	}{
		{"ntfy url with credentials", "/api/settings/ntfy", url.Values{"ntfy_server_url": {"https://me:" + secret + "@ntfy.example"}, "ntfy_topic": {"t"}}, "settings.push_error_server_url"},
		{"ntfy url with token in query", "/api/settings/ntfy", url.Values{"ntfy_server_url": {"https://ntfy.example/?auth=" + secret}, "ntfy_topic": {"t"}}, "settings.push_error_server_url"},
		{"ntfy unsupported scheme", "/api/settings/ntfy", url.Values{"ntfy_server_url": {"ftp://ntfy.example"}, "ntfy_topic": {"t"}}, "settings.push_error_server_url"},
		{"ntfy missing topic", "/api/settings/ntfy", url.Values{}, "settings.ntfy_error_topic_required"},
		{"ntfy bad topic", "/api/settings/ntfy", url.Values{"ntfy_topic": {"bad/" + secret}}, "settings.ntfy_error_topic_invalid"},
		{"ntfy priority too high", "/api/settings/ntfy", url.Values{"ntfy_topic": {"t"}, "ntfy_priority": {"6"}}, "settings.ntfy_error_priority"},
		{"ntfy priority not a number", "/api/settings/ntfy", url.Values{"ntfy_topic": {"t"}, "ntfy_priority": {"high"}}, "settings.ntfy_error_priority"},
		{"ntfy token with newline", "/api/settings/ntfy", url.Values{"ntfy_topic": {"t"}, "ntfy_access_token": {secret + "\r\nX: y"}}, "settings.push_error_token_invalid"},
		{"gotify missing url", "/api/settings/gotify", url.Values{"gotify_app_token": {"tok"}}, "settings.gotify_error_server_url_required"},
		{"gotify bad url", "/api/settings/gotify", url.Values{"gotify_server_url": {"gotify.lan"}, "gotify_app_token": {"tok"}}, "settings.push_error_server_url"},
		{"gotify url with credentials", "/api/settings/gotify", url.Values{"gotify_server_url": {"http://u:" + secret + "@gotify.lan"}, "gotify_app_token": {"tok"}}, "settings.push_error_server_url"},
		{"gotify missing token", "/api/settings/gotify", url.Values{"gotify_server_url": {"http://gotify.lan"}}, "settings.gotify_error_token_required"},
		{"gotify token with space", "/api/settings/gotify", url.Values{"gotify_server_url": {"http://gotify.lan"}, "gotify_app_token": {"a " + secret}}, "settings.push_error_token_invalid"},
		{"gotify priority negative", "/api/settings/gotify", url.Values{"gotify_server_url": {"http://gotify.lan"}, "gotify_app_token": {"tok"}, "gotify_priority": {"-1"}}, "settings.gotify_error_priority"},
		{"gotify priority too high", "/api/settings/gotify", url.Values{"gotify_server_url": {"http://gotify.lan"}, "gotify_app_token": {"tok"}, "gotify_priority": {"11"}}, "settings.gotify_error_priority"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, path := range []string{tt.path, tt.path + "/test"} {
				resp := postPushForm(router, path, tt.form)
				assert.Equal(t, http.StatusBadRequest, resp.Code, path)
				assert.Contains(t, resp.Body.String(), template.HTMLEscapeString(catalog.T("en", tt.key)), path)
				assert.NotContains(t, resp.Body.String(), secret, path)
			}
		})
	}
	_, err := settings.GetNtfyConfig()
	assert.Error(t, err, "nothing may be saved by a rejected form")
	_, err = settings.GetGotifyConfig()
	assert.Error(t, err)
}

func TestPushMessages_AreTranslated(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)
	require.NoError(t, settings.SetStringSetting("lang", "de"))
	resp := postPushForm(router, "/api/settings/ntfy", url.Values{})
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	assert.Contains(t, resp.Body.String(), catalog.T("de", "settings.ntfy_error_topic_required"))
	assert.NotEqual(t, catalog.T("en", "settings.ntfy_error_topic_required"), catalog.T("de", "settings.ntfy_error_topic_required"))
}

func TestNtfyTest_UsesSubmittedConfigWithoutSaving(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)
	savedSink := newPushTestSink(t, 200, `{"id":1}`)
	stored := &models.NtfyConfig{ServerURL: savedSink.URL, Topic: "saved", AccessToken: "tk_saved", Priority: 1}
	require.NoError(t, settings.SaveNtfyConfig(stored))

	submitted := newPushTestSink(t, 200, `{"id":1}`)
	resp := postPushForm(router, "/api/settings/ntfy/test", url.Values{
		"ntfy_server_url": {submitted.URL}, "ntfy_topic": {"submitted"}, "ntfy_access_token": {"tk_form"}, "ntfy_priority": {"5"},
	})
	assert.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.ntfy_test_success"))

	require.Equal(t, 1, submitted.count())
	assert.Zero(t, savedSink.count(), "the test must not go to the saved server")
	assert.Equal(t, "Bearer tk_form", submitted.requests[0].Header.Get("Authorization"))
	assert.Equal(t, "submitted", submitted.bodies[0]["topic"])
	assert.Equal(t, float64(5), submitted.bodies[0]["priority"])
	assert.Equal(t, "SubTrackr Test", submitted.bodies[0]["title"])

	got, err := settings.GetNtfyConfig()
	require.NoError(t, err)
	assert.Equal(t, stored, got, "testing must never write the submitted values to the database")
}

func TestPushTest_BlankTokenUsesStoredSecretForSameServerOnly(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)
	sink := newPushTestSink(t, 200, `{"id":1}`)
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: sink.URL, Topic: "t", AccessToken: "tk_saved"}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: sink.URL, AppToken: "AppSaved"}))

	resp := postPushForm(router, "/api/settings/ntfy/test", url.Values{"ntfy_server_url": {sink.URL + "/"}, "ntfy_topic": {"t"}})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "Bearer tk_saved", sink.requests[0].Header.Get("Authorization"))

	resp = postPushForm(router, "/api/settings/gotify/test", url.Values{"gotify_server_url": {sink.URL}})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "AppSaved", sink.requests[1].Header.Get("X-Gotify-Key"))
	assert.Empty(t, sink.requests[1].URL.RawQuery)

	// A saved credential is never sent to a server it was not saved for.
	other := newPushTestSink(t, 200, `{"id":1}`)
	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/api/settings/ntfy/test", url.Values{"ntfy_server_url": {other.URL}, "ntfy_topic": {"t"}}},
		{"/api/settings/gotify/test", url.Values{"gotify_server_url": {other.URL}}},
	} {
		resp = postPushForm(router, tc.path, tc.form)
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.push_error_token_reenter"))
	}
	assert.Zero(t, other.count())
}

func TestGotifySaveAndTest_RoundTrip(t *testing.T) {
	router, settings, catalog := newPushSettingsRouter(t)
	sink := newPushTestSink(t, 200, `{"id":1}`)

	resp := postPushForm(router, "/api/settings/gotify", url.Values{
		"gotify_server_url": {sink.URL + "/"}, "gotify_app_token": {"AppTok"}, "gotify_priority": {"0"},
	})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.gotify_saved"))
	saved, err := settings.GetGotifyConfig()
	require.NoError(t, err)
	assert.Equal(t, &models.GotifyConfig{ServerURL: sink.URL, AppToken: "AppTok", Priority: 0}, saved)

	// Blank token keeps the stored one; blank priority falls back to the default.
	resp = postPushForm(router, "/api/settings/gotify", url.Values{"gotify_server_url": {sink.URL}})
	assert.Equal(t, http.StatusOK, resp.Code)
	saved, _ = settings.GetGotifyConfig()
	assert.Equal(t, "AppTok", saved.AppToken)
	assert.Equal(t, service.GotifyDefaultPriority, saved.Priority)

	resp = postPushForm(router, "/api/settings/gotify/test", url.Values{"gotify_server_url": {sink.URL}, "gotify_priority": {"7"}})
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Contains(t, resp.Body.String(), catalog.T("en", "settings.gotify_test_success"))
	assert.Equal(t, "/message", sink.requests[0].URL.Path)
	assert.Equal(t, float64(7), sink.bodies[0]["priority"])
	saved, _ = settings.GetGotifyConfig()
	assert.Equal(t, service.GotifyDefaultPriority, saved.Priority, "test must not save the submitted priority")
}

func TestPushTest_ProviderFailureIsSanitized(t *testing.T) {
	const secret = "tk_MUSTNOTLEAK"
	router, _, catalog := newPushSettingsRouter(t)
	sink := newPushTestSink(t, http.StatusUnauthorized, `{"error":"bad token `+secret+` <script>alert(1)</script>"}`)

	for _, tc := range []struct {
		path string
		form url.Values
	}{
		{"/api/settings/ntfy/test", url.Values{"ntfy_server_url": {sink.URL}, "ntfy_topic": {"t"}, "ntfy_access_token": {secret}}},
		{"/api/settings/gotify/test", url.Values{"gotify_server_url": {sink.URL}, "gotify_app_token": {secret}}},
	} {
		resp := postPushForm(router, tc.path, tc.form)
		body := resp.Body.String()
		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, body, catalog.T("en", "settings.push_test_failed"))
		assert.Contains(t, body, "HTTP 401")
		assert.NotContains(t, body, secret)
		assert.NotContains(t, body, "<script>")
		assert.NotContains(t, body, sink.URL)
	}
}

func TestPushConfigAPI_NeverReturnsSecrets(t *testing.T) {
	router, settings, _ := newPushSettingsRouter(t)
	get := func(path string) string {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		return response.Body.String()
	}
	assert.JSONEq(t, `{"configured":false,"server_url":"https://ntfy.sh","has_topic":false,"has_access_token":false,"priority":3}`, get("/api/settings/ntfy"))
	assert.JSONEq(t, `{"configured":false,"server_url":"","has_app_token":false,"priority":5}`, get("/api/settings/gotify"))

	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: "http://ntfy.lan", Topic: "secret-topic", AccessToken: "tk_hidden", Priority: 4}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: "http://gotify.lan", AppToken: "AppHidden", Priority: 9}))
	assert.JSONEq(t, `{"configured":true,"server_url":"http://ntfy.lan","has_topic":true,"has_access_token":true,"priority":4}`, get("/api/settings/ntfy"))
	assert.JSONEq(t, `{"configured":true,"server_url":"http://gotify.lan","has_app_token":true,"priority":9}`, get("/api/settings/gotify"))
}

func TestSettingsPage_RendersPushViewsWithoutSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Settings{}))
	settings := service.NewSettingsService(repository.NewSettingsRepository(db))
	require.NoError(t, settings.SaveNtfyConfig(&models.NtfyConfig{ServerURL: "http://ntfy.lan", Topic: "alerts", AccessToken: "tk_hidden", Priority: 4}))
	require.NoError(t, settings.SaveGotifyConfig(&models.GotifyConfig{ServerURL: "http://gotify.lan", AppToken: "AppHidden", Priority: 9}))
	catalog := i18n.NewCatalog()
	handler := NewSubscriptionHandler(nil, settings, nil, nil, nil, nil, nil, catalog)

	router := gin.New()
	// JSON exposes every view field, so a secret added to a view struct would fail this test.
	router.SetHTMLTemplate(template.Must(template.New("settings.html").Funcs(template.FuncMap{
		"dump": func(v any) string {
			raw, _ := json.Marshal(v)
			return string(raw)
		},
	}).Parse(`{{dump .NtfyConfig}}|{{.NtfyConfigured}}|{{dump .GotifyConfig}}|{{.GotifyConfigured}}`)))
	router.GET("/settings", handler.Settings)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/settings", nil))

	assert.Equal(t, http.StatusOK, response.Code)
	body := template.HTMLEscapeString(response.Body.String())
	assert.NotContains(t, body, "tk_hidden")
	assert.NotContains(t, body, "AppHidden")
	assert.Contains(t, body, "alerts")
	assert.Contains(t, body, "gotify.lan")
	assert.Contains(t, response.Body.String(), "|true|")
}

func TestLocales_NtfyAndGotifyKeysTranslatedEverywhere(t *testing.T) {
	catalog := i18n.NewCatalog()
	require.NoError(t, catalog.LoadDir(filepath.Join(repoRoot(t), "web", "locales")))
	languages := catalog.AvailableLanguages()
	require.Len(t, languages, 5)

	// Words that legitimately read the same in several languages.
	sameAllowed := map[string]bool{
		"settings.ntfy_server_url":               true,
		"settings.gotify_server_url":             true,
		"settings.ntfy_topic":                    true,
		"settings.ntfy_access_token_placeholder": true,
		"settings.gotify_server_url_placeholder": true,
		"settings.ntfy_topic_placeholder":        true,
		"settings.ntfy_priority_default":         true,
	}
	keys := []string{
		"settings.ntfy_notifications", "settings.ntfy_description", "settings.ntfy_configuration", "settings.ntfy_server_url", "settings.ntfy_server_url_help",
		"settings.ntfy_topic", "settings.ntfy_topic_placeholder", "settings.ntfy_topic_help", "settings.ntfy_access_token", "settings.ntfy_access_token_placeholder",
		"settings.ntfy_access_token_help", "settings.ntfy_clear_token", "settings.ntfy_priority", "settings.ntfy_priority_min", "settings.ntfy_priority_low",
		"settings.ntfy_priority_default", "settings.ntfy_priority_high", "settings.ntfy_priority_max", "settings.save_ntfy", "settings.ntfy_saved", "settings.ntfy_test_success",
		"settings.ntfy_error_topic_required", "settings.ntfy_error_topic_invalid", "settings.ntfy_error_priority", "settings.gotify_notifications", "settings.gotify_description",
		"settings.gotify_configuration", "settings.gotify_server_url", "settings.gotify_server_url_placeholder", "settings.gotify_server_url_help", "settings.gotify_app_token",
		"settings.gotify_app_token_placeholder", "settings.gotify_app_token_help", "settings.gotify_priority", "settings.gotify_priority_help", "settings.save_gotify",
		"settings.gotify_saved", "settings.gotify_test_success", "settings.gotify_error_server_url_required", "settings.gotify_error_token_required", "settings.gotify_error_priority",
		"settings.push_error_server_url", "settings.push_error_token_invalid", "settings.push_error_token_reenter", "settings.push_error_save", "settings.push_test_failed",
	}
	for _, key := range keys {
		english := catalog.T("en", key)
		assert.NotEqual(t, key, english, "missing English value for %s", key)
		for _, lang := range languages {
			value := catalog.T(lang.Code, key)
			assert.NotEqual(t, key, value, "%s missing in %s", key, lang.Code)
			if lang.Code != "en" && !sameAllowed[key] {
				assert.NotEqual(t, english, value, "%s is untranslated in %s", key, lang.Code)
			}
		}
	}
}

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"subtrackr/internal/database"
	"subtrackr/internal/handlers"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
)

const testAPIKey = "sk_test_roundtrip_0123456789abcdef"

type apiServer struct {
	db     *gorm.DB
	router *gin.Engine
}

// newAPIServer wires the real routes, so requests pass through API-key auth
// and the JSON body middleware exactly as they do in production.
func newAPIServer(t *testing.T) *apiServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Initialize(filepath.Join(t.TempDir(), "api.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	settingsRepo := repository.NewSettingsRepository(db)
	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptionService := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	settingsService := service.NewSettingsService(settingsRepo)
	_, err = settingsService.CreateAPIKey("test", testAPIKey)
	require.NoError(t, err)

	handler := handlers.NewSubscriptionHandler(subscriptionService, settingsService,
		service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo),
		nil, nil, categoryService, service.NewTagService(repository.NewTagRepository(db)), nil)
	authHandler := handlers.NewAuthHandler(settingsService, service.NewSessionService("test-session-secret-0123456789abcdef"), nil, nil)

	router := gin.New()
	setupRoutes(router, handler, handlers.NewSettingsHandler(settingsService, nil), settingsService,
		handlers.NewCategoryHandler(categoryService), authHandler, nil)
	return &apiServer{db: db, router: router}
}

func (s *apiServer) do(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, err := json.Marshal(b)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("X-API-Key", testAPIKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	s.router.ServeHTTP(response, request)
	return response
}

func (s *apiServer) create(t *testing.T, body map[string]any) uint {
	t.Helper()
	response := s.do(t, http.MethodPost, "/api/v1/subscriptions", body)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var created models.Subscription
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &created))
	return created.ID
}

func (s *apiServer) get(t *testing.T, id uint) []byte {
	t.Helper()
	response := s.do(t, http.MethodGet, fmt.Sprintf("/api/v1/subscriptions/%d", id), nil)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return response.Body.Bytes()
}

func (s *apiServer) tagNames(t *testing.T, id uint) []string {
	t.Helper()
	var sub models.Subscription
	require.NoError(t, json.Unmarshal(s.get(t, id), &sub))
	names := []string{}
	for _, tag := range sub.Tags {
		names = append(names, tag.Name)
	}
	sort.Strings(names)
	return names
}

func (s *apiServer) allTagNames(t *testing.T) []string {
	t.Helper()
	var names []string
	require.NoError(t, s.db.Model(&models.Tag{}).Order("name").Pluck("name", &names).Error)
	return names
}

// stable drops the fields an update is expected to change.
func stable(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var fields map[string]any
	require.NoError(t, json.Unmarshal(body, &fields))
	delete(fields, "updated_at")
	return fields
}

// A subscription fetched with GET and sent back unchanged with PUT stays
// exactly as it was: same tags, dates and every other field.
func TestAPIGetThenPutUnchangedKeepsSubscription(t *testing.T) {
	server := newAPIServer(t)
	category := models.Category{Name: "Entertainment"}
	require.NoError(t, server.db.Create(&category).Error)
	id := server.create(t, map[string]any{
		"name": "Netflix", "cost": 15.99, "schedule": "Monthly", "status": "Active",
		"category_id": category.ID, "cancellation_date": "2027-01-31",
		"original_currency": "USD", "payment_method": "Visa", "account": "family@example.com",
		"start_date": "2025-03-14", "renewal_date": "2026-12-14", "cancellation_notice_days": 3,
		"url": "https://netflix.com", "notes": "shared plan", "usage": "high",
		"reminder_enabled": true, "autopay": true, "share_count": 2,
		"tags": "streaming, family",
	})
	before := server.get(t, id)
	require.Equal(t, []string{"family", "streaming"}, server.tagNames(t, id))

	response := server.do(t, http.MethodPut, fmt.Sprintf("/api/v1/subscriptions/%d", id), before)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	assert.Equal(t, stable(t, before), stable(t, server.get(t, id)))
	assert.Equal(t, []string{"family", "streaming"}, server.allTagNames(t), "no junk tags")
}

func TestAPIAcceptsTagsInEveryDocumentedForm(t *testing.T) {
	server := newAPIServer(t)
	other := server.create(t, map[string]any{"name": "Gym", "cost": 30, "schedule": "Monthly", "status": "Active", "tags": "personal"})
	var personal models.Tag
	require.NoError(t, server.db.Where("name = ?", "personal").First(&personal).Error)

	id := server.create(t, map[string]any{
		"name": "Slack", "cost": 8, "schedule": "Monthly", "status": "Active",
		// The objects GET returns; the id is another tag's and must be ignored.
		"tags": []any{map[string]any{"id": personal.ID, "name": "work"}, map[string]any{"name": "chat"}},
	})
	assert.Equal(t, []string{"chat", "work"}, server.tagNames(t, id))
	assert.Equal(t, []string{"personal"}, server.tagNames(t, other))

	put := func(body map[string]any) *httptest.ResponseRecorder {
		return server.do(t, http.MethodPut, fmt.Sprintf("/api/v1/subscriptions/%d", id), body)
	}

	require.Equal(t, http.StatusOK, put(map[string]any{"tags": []any{"Team", " work "}}).Code)
	assert.Equal(t, []string{"Team", "work"}, server.tagNames(t, id), "a list of names")

	require.Equal(t, http.StatusOK, put(map[string]any{"tags": "billing, work"}).Code)
	assert.Equal(t, []string{"billing", "work"}, server.tagNames(t, id), "the comma-separated string")

	require.Equal(t, http.StatusOK, put(map[string]any{"notes": "no tags field"}).Code)
	assert.Equal(t, []string{"billing", "work"}, server.tagNames(t, id), "absent leaves tags alone")

	require.Equal(t, http.StatusOK, put(map[string]any{"tags": nil}).Code)
	assert.Equal(t, []string{"billing", "work"}, server.tagNames(t, id), "null leaves tags alone")

	require.Equal(t, http.StatusOK, put(map[string]any{"tags": []any{}}).Code)
	assert.Equal(t, []string{}, server.tagNames(t, id), "an empty list clears tags")

	assert.Equal(t, []string{"personal"}, server.allTagNames(t), "orphaned tags are removed, no junk tags")
}

func TestAPIRejectsTagsItCannotMatchByName(t *testing.T) {
	server := newAPIServer(t)
	for name, tags := range map[string]any{
		"tag ids":                  []any{1, 2},
		"object without name":      []any{map[string]any{"id": 1}},
		"non-string name":          []any{map[string]any{"name": 7}},
		"nested list":              []any{[]any{"work"}},
		"comma inside a name":      []any{"work, home"},
		"object instead of a list": map[string]any{"name": "work"},
		"boolean":                  true,
	} {
		t.Run(name, func(t *testing.T) {
			// Each case gets its own subscription, so one failure cannot
			// cascade into the others.
			id := server.create(t, map[string]any{"name": "Slack", "cost": 8, "schedule": "Monthly", "status": "Active", "tags": "work"})
			response := server.do(t, http.MethodPut, fmt.Sprintf("/api/v1/subscriptions/%d", id),
				map[string]any{"name": "Renamed", "tags": tags})
			assert.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), "tag")

			var sub models.Subscription
			require.NoError(t, json.Unmarshal(server.get(t, id), &sub))
			assert.Equal(t, "Slack", sub.Name, "a rejected request changes nothing")
			assert.Equal(t, []string{"work"}, server.tagNames(t, id))
		})
	}

	response := server.do(t, http.MethodPost, "/api/v1/subscriptions",
		map[string]any{"name": "Zoom", "cost": 15, "schedule": "Monthly", "status": "Active", "tags": []any{3}})
	assert.Equal(t, http.StatusBadRequest, response.Code)
	var count int64
	require.NoError(t, server.db.Model(&models.Subscription{}).Where("name = ?", "Zoom").Count(&count).Error)
	assert.Zero(t, count, "a rejected create stores nothing")
}

package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"subtrackr/internal/database"
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
)

// Restore in replace mode deletes every existing subscription, tagged ones
// included, then recreates the backup, uncategorized entries included.
func TestRestoreReplaceWithTagsAndUncategorizedEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := database.Initialize(filepath.Join(t.TempDir(), "restore.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	settingsRepo := repository.NewSettingsRepository(db)
	handler := NewSubscriptionHandler(
		subscriptions,
		service.NewSettingsService(settingsRepo),
		service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo),
		nil, // notifier
		nil, // logoService
		categoryService,
		nil, // tagService
		nil, // i18nCatalog
	)
	router := gin.New()
	router.POST("/api/restore", handler.RestoreData)

	existing, err := subscriptions.Create(&models.Subscription{Name: "Old tagged", Cost: 1, Schedule: "Monthly", Status: "Active"})
	require.NoError(t, err)
	tagRepo := repository.NewTagRepository(db)
	tags, err := tagRepo.FindOrCreateByNames([]string{"old"})
	require.NoError(t, err)
	require.NoError(t, tagRepo.ReplaceSubscriptionTags(existing.ID, tags))

	backup, err := json.Marshal(map[string]any{
		"version": "test",
		"subscriptions": []map[string]any{
			{"name": "Restored plain", "cost": 2, "schedule": "Monthly", "status": "Active", "category_id": 0, "category": map[string]any{"id": 0, "name": ""}},
			{"name": "Restored filed", "cost": 3, "schedule": "Monthly", "status": "Active", "category_id": 42, "category": map[string]any{"id": 42, "name": "Backup category"}},
		},
	})
	require.NoError(t, err)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("backup_file", "backup.json")
	require.NoError(t, err)
	_, err = file.Write(backup)
	require.NoError(t, err)
	require.NoError(t, form.WriteField("mode", "replace"))
	require.NoError(t, form.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/restore", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	all, err := subscriptions.GetAll()
	require.NoError(t, err)
	byName := map[string]models.Subscription{}
	for _, sub := range all {
		byName[sub.Name] = sub
	}
	require.Len(t, byName, 2)
	assert.Zero(t, byName["Restored plain"].CategoryID)
	assert.Equal(t, "Backup category", byName["Restored filed"].Category.Name)
}

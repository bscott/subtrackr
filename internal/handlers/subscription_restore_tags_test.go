package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
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
	"subtrackr/internal/models"
	"subtrackr/internal/repository"
	"subtrackr/internal/service"
)

// Regression tests for restored tags (#167). A backup stores each tag's id and
// name; the id belongs to the instance the backup came from, so only the name
// may decide which tag a restored subscription gets.

type restoreInstance struct {
	db            *gorm.DB
	router        *gin.Engine
	subscriptions *service.SubscriptionService
	tags          *repository.TagRepository
}

func newRestoreInstance(t *testing.T) *restoreInstance {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := database.Initialize(filepath.Join(t.TempDir(), "subtrackr.db"))
	require.NoError(t, err)
	require.NoError(t, database.RunMigrations(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })

	categoryService := service.NewCategoryService(repository.NewCategoryRepository(db))
	subscriptions := service.NewSubscriptionService(repository.NewSubscriptionRepository(db), categoryService)
	settingsRepo := repository.NewSettingsRepository(db)
	tagRepo := repository.NewTagRepository(db)
	handler := NewSubscriptionHandler(
		subscriptions,
		service.NewSettingsService(settingsRepo),
		service.NewCurrencyService(repository.NewExchangeRateRepository(db), settingsRepo),
		nil, // notifier
		nil, // logoService
		categoryService,
		service.NewTagService(tagRepo),
		nil, // i18nCatalog
	)
	router := gin.New()
	router.GET("/api/backup", handler.BackupData)
	router.POST("/api/restore", handler.RestoreData)
	return &restoreInstance{db: db, router: router, subscriptions: subscriptions, tags: tagRepo}
}

// add creates a subscription and attaches tags by name, as the UI does.
func (i *restoreInstance) add(t *testing.T, name string, tagNames ...string) {
	t.Helper()
	created, err := i.subscriptions.Create(&models.Subscription{Name: name, Cost: 9.99, Schedule: "Monthly", Status: "Active", OriginalCurrency: "USD"})
	require.NoError(t, err)
	if len(tagNames) == 0 {
		return
	}
	tags, err := i.tags.FindOrCreateByNames(tagNames)
	require.NoError(t, err)
	require.NoError(t, i.tags.ReplaceSubscriptionTags(created.ID, tags))
}

func (i *restoreInstance) backup(t *testing.T) []byte {
	t.Helper()
	response := httptest.NewRecorder()
	i.router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/backup", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	return response.Body.Bytes()
}

type restoreResult struct {
	Imported int      `json:"imported_count"`
	Errors   []string `json:"errors"`
}

func (i *restoreInstance) restore(t *testing.T, backup []byte, mode string) restoreResult {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	file, err := form.CreateFormFile("backup_file", "backup.json")
	require.NoError(t, err)
	_, err = file.Write(backup)
	require.NoError(t, err)
	require.NoError(t, form.WriteField("mode", mode))
	require.NoError(t, form.Close())

	request := httptest.NewRequest(http.MethodPost, "/api/restore", &body)
	request.Header.Set("Content-Type", form.FormDataContentType())
	response := httptest.NewRecorder()
	i.router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())

	var result restoreResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	return result
}

// tagsBySubscription maps each subscription name to its sorted tag names.
func (i *restoreInstance) tagsBySubscription(t *testing.T) map[string][]string {
	t.Helper()
	all, err := i.subscriptions.GetAll()
	require.NoError(t, err)
	out := make(map[string][]string, len(all))
	for _, sub := range all {
		names := []string{}
		for _, tag := range sub.Tags {
			names = append(names, tag.Name)
		}
		sort.Strings(names)
		out[sub.Name] = names
	}
	return out
}

func (i *restoreInstance) tagID(t *testing.T, name string) uint {
	t.Helper()
	var tag models.Tag
	require.NoError(t, i.db.Where("name = ?", name).First(&tag).Error)
	return tag.ID
}

func (i *restoreInstance) tagCount(t *testing.T) int64 {
	t.Helper()
	var count int64
	require.NoError(t, i.db.Model(&models.Tag{}).Count(&count).Error)
	return count
}

func backupWith(t *testing.T, subscriptions ...map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": "test", "subscriptions": subscriptions})
	require.NoError(t, err)
	return data
}

func backupEntry(name string, tags ...map[string]any) map[string]any {
	return map[string]any{"name": name, "cost": 5, "schedule": "Monthly", "status": "Active", "tags": tags}
}

// A real backup from one instance, restored into another whose tag ids name
// different tags, keeps every subscription's tags by name, in both modes.
func TestBackupRestoreRoundTripKeepsTagsByName(t *testing.T) {
	for _, mode := range []string{"merge", "replace"} {
		t.Run(mode, func(t *testing.T) {
			source := newRestoreInstance(t)
			source.add(t, "Netflix", "work", "family")
			source.add(t, "Spotify", "streaming")
			source.add(t, "Domain", "work")
			source.add(t, "Untagged")
			backup := source.backup(t)

			target := newRestoreInstance(t)
			target.add(t, "Gym", "family", "streaming", "work")
			// Ids collide with different names, which is the case #167 fixed.
			require.Equal(t, source.tagID(t, "work"), target.tagID(t, "family"))
			require.Equal(t, source.tagID(t, "family"), target.tagID(t, "streaming"))
			require.Equal(t, source.tagID(t, "streaming"), target.tagID(t, "work"))

			result := target.restore(t, backup, mode)
			assert.Equal(t, 4, result.Imported)
			assert.Empty(t, result.Errors)

			want := source.tagsBySubscription(t)
			if mode == "merge" {
				want["Gym"] = []string{"family", "streaming", "work"}
			}
			assert.Equal(t, want, target.tagsBySubscription(t))
			assert.EqualValues(t, 3, target.tagCount(t), "restoring must reuse tags with the same name")
		})
	}
}

// A backup tag whose id does not exist here is recreated, not dropped.
func TestRestoreRecreatesTagWhoseBackupIDIsUnknown(t *testing.T) {
	target := newRestoreInstance(t)
	result := target.restore(t, backupWith(t,
		backupEntry("Airline", map[string]any{"id": 999, "name": "travel"}),
	), "merge")

	assert.Equal(t, 1, result.Imported)
	assert.Empty(t, result.Errors)
	assert.Equal(t, map[string][]string{"Airline": {"travel"}}, target.tagsBySubscription(t))
}

// Names match case-insensitively, so a restore does not split one tag in two.
func TestRestoreReusesExistingTagIgnoringCase(t *testing.T) {
	target := newRestoreInstance(t)
	target.add(t, "Gym", "Work")

	result := target.restore(t, backupWith(t,
		backupEntry("Slack", map[string]any{"id": 1, "name": "work"}, map[string]any{"id": 2, "name": " WORK "}),
	), "merge")

	assert.Equal(t, 1, result.Imported)
	assert.Equal(t, map[string][]string{"Gym": {"Work"}, "Slack": {"Work"}}, target.tagsBySubscription(t))
	assert.EqualValues(t, 1, target.tagCount(t))
}

// Tag entries without a usable name are skipped; the subscription is still
// imported with the tags that do have one.
func TestRestoreSkipsTagsWithoutAName(t *testing.T) {
	target := newRestoreInstance(t)
	target.add(t, "Gym", "personal")

	result := target.restore(t, backupWith(t,
		backupEntry("Hosting",
			map[string]any{"id": target.tagID(t, "personal")}, // id only: must not link "personal"
			map[string]any{"id": 7, "name": "   "},
			map[string]any{"name": "infra"},
		),
		backupEntry("Bare", map[string]any{"id": target.tagID(t, "personal"), "name": ""}),
	), "merge")

	assert.Equal(t, 2, result.Imported)
	assert.Empty(t, result.Errors)
	assert.Equal(t, map[string][]string{
		"Gym":     {"personal"},
		"Hosting": {"infra"},
		"Bare":    {},
	}, target.tagsBySubscription(t))
}

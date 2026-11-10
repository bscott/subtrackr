package apidocs

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"

	"subtrackr/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type document struct {
	Components struct {
		Schemas map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func jsonFields(model any) []string {
	t := reflect.TypeOf(model)
	fields := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		if name != "" && name != "-" {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	return fields
}

// The documented response schemas must list exactly the fields the models
// serialize, so adding or renaming a JSON field fails here until the spec follows.
func TestResponseSchemasMatchModels(t *testing.T) {
	var doc document
	require.NoError(t, json.Unmarshal(Spec(), &doc))

	for name, model := range map[string]any{
		"Subscription":   models.Subscription{},
		"Category":       models.Category{},
		"Tag":            models.Tag{},
		"Stats":          models.Stats{},
		"CurrencyTotals": models.CurrencyTotals{},
	} {
		schema, ok := doc.Components.Schemas[name]
		require.True(t, ok, "schema %s is missing", name)
		documented := make([]string, 0, len(schema.Properties))
		for field := range schema.Properties {
			documented = append(documented, field)
		}
		sort.Strings(documented)
		assert.Equal(t, jsonFields(model), documented, "schema %s", name)
	}
}

func TestHandlerServesSpec(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET(Path, Handler)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, Path, nil))

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json; charset=utf-8", recorder.Header().Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	assert.Equal(t, "3.0.3", body["openapi"])
}

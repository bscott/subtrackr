package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestJSONBodyAsForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", JSONBodyAsForm(), func(c *gin.Context) {
		c.String(http.StatusOK, c.PostForm("name")+"|"+c.PostForm("cost"))
	})

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"Netflix","cost":15.99}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if got := recorder.Body.String(); got != "Netflix|15.99" {
		t.Errorf("got %q", got)
	}
}

func TestJSONBodyAsFormRejectsInvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", JSONBodyAsForm(), func(c *gin.Context) { c.Status(http.StatusOK) })

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("got status %d", recorder.Code)
	}
}

func TestJSONBodyAsFormRejectsOversizedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/", JSONBodyAsForm(), func(c *gin.Context) { c.Status(http.StatusOK) })

	body := `{"notes":"` + strings.Repeat("a", maxJSONBodyBytes) + `"}`
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got status %d", recorder.Code)
	}
}

func TestTagsFormValue(t *testing.T) {
	valid := map[string]struct {
		in   any
		want string
	}{
		"comma-separated string":  {"streaming, family", "streaming, family"},
		"list of names":           {[]any{"streaming", "family"}, "streaming,family"},
		"objects from GET":        {[]any{map[string]any{"id": json.Number("9"), "name": "work", "created_at": "2026-01-01T00:00:00Z"}}, "work"},
		"mixed names and objects": {[]any{"a", map[string]any{"name": "b"}}, "a,b"},
		"empty list clears":       {[]any{}, ""},
	}
	for name, tc := range valid {
		t.Run(name, func(t *testing.T) {
			got, err := tagsFormValue(tc.in)
			if err != nil || got != tc.want {
				t.Errorf("tagsFormValue(%v) = %q, %v; want %q", tc.in, got, err, tc.want)
			}
		})
	}

	invalid := map[string]any{
		"tag ids":             []any{json.Number("1")},
		"object without name": []any{map[string]any{"id": json.Number("1")}},
		"non-string name":     []any{map[string]any{"name": json.Number("7")}},
		"nested list":         []any{[]any{"a"}},
		"comma in a name":     []any{"a,b"},
		"comma in an object":  []any{map[string]any{"name": "a,b"}},
		"single object":       map[string]any{"name": "a"},
		"number":              json.Number("3"),
		"boolean":             true,
	}
	for name, in := range invalid {
		t.Run(name, func(t *testing.T) {
			if got, err := tagsFormValue(in); err == nil {
				t.Errorf("tagsFormValue(%v) = %q, want an error", in, got)
			}
		})
	}
}

// A request with unusable tags is stopped before the handler runs.
func TestJSONBodyAsFormRejectsInvalidTags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	reached := false
	router.PUT("/", JSONBodyAsForm(), func(c *gin.Context) { reached = true })

	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"name":"x","tags":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest || reached {
		t.Errorf("got status %d, handler reached %v", recorder.Code, reached)
	}
}

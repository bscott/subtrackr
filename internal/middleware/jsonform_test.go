package middleware

import (
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

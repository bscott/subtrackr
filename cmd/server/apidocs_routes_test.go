package main

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"testing"

	"subtrackr/internal/apidocs"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var ginParam = regexp.MustCompile(`:([A-Za-z_]+)`)

// Every /api/v1 route must be documented in the OpenAPI spec, and the spec must
// not describe routes that do not exist.
func TestOpenAPISpecMatchesRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	setupRoutes(router, nil, nil, nil, nil, nil, nil)

	var registered []string
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/") {
			registered = append(registered, route.Method+" "+ginParam.ReplaceAllString(route.Path, "{$1}"))
		}
	}
	sort.Strings(registered)

	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	require.NoError(t, json.Unmarshal(apidocs.Spec(), &doc))
	var documented []string
	for path, operations := range doc.Paths {
		for method := range operations {
			if method != "parameters" {
				documented = append(documented, strings.ToUpper(method)+" "+path)
			}
		}
	}
	sort.Strings(documented)

	assert.Equal(t, registered, documented)
}

// Package apidocs serves the OpenAPI description of the public /api/v1 API.
//
// openapi.json is the source of truth for the API documentation. Tests keep it
// in step with the registered routes and with the JSON the models produce.
package apidocs

import (
	_ "embed"
	"net/http"

	"github.com/gin-gonic/gin"
)

// Path is where the specification is served. It needs no authentication so
// scripts and agents can discover the API before they hold an API key.
const Path = "/api/v1/openapi.json"

//go:embed openapi.json
var spec []byte

// Spec returns the OpenAPI document.
func Spec() []byte {
	return spec
}

// Handler serves the OpenAPI document.
func Handler(c *gin.Context) {
	c.Data(http.StatusOK, "application/json; charset=utf-8", spec)
}

package middleware

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxJSONBodyBytes caps a JSON request body. A subscription is a handful of
// short fields, so 1 MB is far above any legitimate request.
const maxJSONBodyBytes = 1 << 20

// JSONBodyAsForm lets handlers that read form values serve a JSON request body.
func JSONBodyAsForm() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.ContentType(), "application/json") {
			c.Next()
			return
		}

		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxJSONBodyBytes)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.UseNumber()
		var body map[string]any
		if err := decoder.Decode(&body); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		form := url.Values{}
		for key, value := range body {
			if value != nil {
				form.Set(key, fmt.Sprint(value))
			}
		}
		c.Request.PostForm = form
		c.Request.Form = form
		c.Next()
	}
}

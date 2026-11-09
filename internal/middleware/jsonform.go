package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// JSONBodyAsForm lets handlers that read form values serve a JSON request body.
func JSONBodyAsForm() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.ContentType(), "application/json") {
			c.Next()
			return
		}

		decoder := json.NewDecoder(c.Request.Body)
		decoder.UseNumber()
		var body map[string]any
		if err := decoder.Decode(&body); err != nil {
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

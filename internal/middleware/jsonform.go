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
			if value == nil {
				continue
			}
			if key == "tags" {
				tags, err := tagsFormValue(value)
				if err != nil {
					c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				form.Set(key, tags)
				continue
			}
			form.Set(key, fmt.Sprint(value))
		}
		c.Request.PostForm = form
		c.Request.Form = form
		c.Next()
	}
}

var errInvalidTags = errors.New(`tags must be a comma-separated string, a list of names, or a list of tag objects with a "name"`)

// tagsFormValue turns a JSON tags value into the comma-separated list the
// form handlers parse. Besides the string form it accepts a list of names and
// the list of tag objects that GET returns, so a subscription fetched from the
// API can be sent back unchanged. Tag ids are ignored: tags are matched by
// name. An empty list clears the tags.
func tagsFormValue(value any) (string, error) {
	switch v := value.(type) {
	case string:
		return v, nil
	case []any:
		names := make([]string, 0, len(v))
		for _, item := range v {
			var name string
			switch tag := item.(type) {
			case string:
				name = tag
			case map[string]any:
				n, ok := tag["name"].(string)
				if !ok {
					return "", errInvalidTags
				}
				name = n
			default:
				return "", errInvalidTags
			}
			// The form value is split on commas, so a comma would turn one
			// tag into two.
			if strings.Contains(name, ",") {
				return "", fmt.Errorf("tag name %q contains a comma", name)
			}
			names = append(names, name)
		}
		return strings.Join(names, ","), nil
	default:
		return "", errInvalidTags
	}
}

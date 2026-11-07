package handlers

import "strings"

// escapeICalText escapes TEXT property values, including line breaks, so user
// content cannot truncate a value or inject another calendar property/event.
func escapeICalText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\n", "\\n").Replace(value)
}

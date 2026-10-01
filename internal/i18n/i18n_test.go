package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogLoadAndLookup(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{"lang.name":"English","hello":"Hello"}`), 0644))
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "es.json"), []byte(`{"lang.name":"Español","hello":"Hola"}`), 0644))

	c := NewCatalog()
	assert.NoError(t, c.LoadDir(dir))

	assert.Equal(t, "Hello", c.T("en", "hello"))
	assert.Equal(t, "Hola", c.T("es", "hello"))

	// Missing key falls back to English
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "de.json"), []byte(`{"lang.name":"Deutsch"}`), 0644))
	c2 := NewCatalog()
	assert.NoError(t, c2.LoadDir(dir))
	assert.Equal(t, "Hello", c2.T("de", "hello"))

	// Unknown lang falls back to English
	assert.Equal(t, "Hello", c.T("fr", "hello"))

	// Unknown key returns key as-is
	assert.Equal(t, "missing.key", c.T("en", "missing.key"))

	langs := c2.AvailableLanguages()
	assert.GreaterOrEqual(t, len(langs), 3)
}

func TestShippedLocalesHaveMatchingKeys(t *testing.T) {
	dir := filepath.Join("..", "..", "web", "locales")
	readKeys := func(name string) map[string]bool {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		var translations map[string]string
		require.NoError(t, json.Unmarshal(contents, &translations))
		keys := make(map[string]bool, len(translations))
		for key, value := range translations {
			assert.NotEmpty(t, value, "%s: %s", name, key)
			keys[key] = true
		}
		return keys
	}

	englishKeys := readKeys("en.json")
	for _, name := range []string{"de.json", "es.json", "nl.json", "zh.json"} {
		assert.Equal(t, englishKeys, readKeys(name), name)
	}
}

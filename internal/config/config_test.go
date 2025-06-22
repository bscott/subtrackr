package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_LoadDefaults(t *testing.T) {
	// Clear environment variables
	os.Unsetenv("PORT")
	os.Unsetenv("DATABASE_PATH")
	os.Unsetenv("GIN_MODE")

	cfg := Load()

	// Check defaults
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "./data/subtrackr.db", cfg.DatabasePath)
	assert.Equal(t, "debug", cfg.Environment)
}

func TestConfig_LoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("PORT", "3000")
	os.Setenv("DATABASE_PATH", "/tmp/test.db")
	os.Setenv("GIN_MODE", "release")

	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DATABASE_PATH")
		os.Unsetenv("GIN_MODE")
	}()

	cfg := Load()

	// Check loaded values
	assert.Equal(t, "3000", cfg.Port)
	assert.Equal(t, "/tmp/test.db", cfg.DatabasePath)
	assert.Equal(t, "release", cfg.Environment)
}

func TestConfig_PartialEnv(t *testing.T) {
	// Set only some environment variables
	os.Setenv("PORT", "4000")
	// DATABASE_PATH and GIN_MODE should use defaults

	defer os.Unsetenv("PORT")

	cfg := Load()

	// Check mixed values
	assert.Equal(t, "4000", cfg.Port)
	assert.Equal(t, "./data/subtrackr.db", cfg.DatabasePath)
	assert.Equal(t, "debug", cfg.GinMode)
}

func TestConfig_EmptyEnvValues(t *testing.T) {
	// Set empty environment variables
	os.Setenv("PORT", "")
	os.Setenv("DATABASE_PATH", "")
	os.Setenv("GIN_MODE", "")

	defer func() {
		os.Unsetenv("PORT")
		os.Unsetenv("DATABASE_PATH")
		os.Unsetenv("GIN_MODE")
	}()

	cfg := Load()

	// Should use defaults when env vars are empty
	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "./data/subtrackr.db", cfg.DatabasePath)
	assert.Equal(t, "debug", cfg.Environment)
}

func TestConfig_ValidatePort(t *testing.T) {
	tests := []struct {
		name     string
		port     string
		expected string
	}{
		{
			name:     "Valid port",
			port:     "8080",
			expected: "8080",
		},
		{
			name:     "Port without leading zero",
			port:     "80",
			expected: "80",
		},
		{
			name:     "Maximum port",
			port:     "65535",
			expected: "65535",
		},
		{
			name:     "Minimum valid port",
			port:     "1",
			expected: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("PORT", tt.port)
			defer os.Unsetenv("PORT")

			cfg := Load()
			assert.Equal(t, tt.expected, cfg.Port)
		})
	}
}

func TestConfig_DatabasePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "Absolute path",
			path:     "/var/lib/subtrackr/data.db",
			expected: "/var/lib/subtrackr/data.db",
		},
		{
			name:     "Relative path",
			path:     "./data/subtrackr.db",
			expected: "./data/subtrackr.db",
		},
		{
			name:     "Path with spaces",
			path:     "/path with spaces/data.db",
			expected: "/path with spaces/data.db",
		},
		{
			name:     "Memory database",
			path:     ":memory:",
			expected: ":memory:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("DATABASE_PATH", tt.path)
			defer os.Unsetenv("DATABASE_PATH")

			cfg := Load()
			assert.Equal(t, tt.expected, cfg.DatabasePath)
		})
	}
}

func TestConfig_Environment(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		expected string
	}{
		{
			name:     "Debug mode",
			mode:     "debug",
			expected: "debug",
		},
		{
			name:     "Release mode",
			mode:     "release",
			expected: "release",
		},
		{
			name:     "Test mode",
			mode:     "test",
			expected: "test",
		},
		{
			name:     "Case insensitive",
			mode:     "RELEASE",
			expected: "RELEASE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("GIN_MODE", tt.mode)
			defer os.Unsetenv("GIN_MODE")

			cfg := Load()
			assert.Equal(t, tt.expected, cfg.Environment)
		})
	}
}
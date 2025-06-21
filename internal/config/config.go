package config

import (
	"os"
)

type Config struct {
	DatabasePath string
	Port         string
	Environment  string
}

func Load() *Config {
	return &Config{
		DatabasePath: getEnv("DATABASE_PATH", "./data/subtrackr.db"),
		Port:         getEnv("PORT", "8080"),
		Environment:  getEnv("GIN_MODE", "debug"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
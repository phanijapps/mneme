// Package config loads mneme runtime configuration from the environment.
package config

import "os"

// Config is the env-driven runtime configuration (Step 8 plan §Config).
type Config struct {
	DatabaseURL string
	APIPort     string
	MCPMode     string
	MCPPort     string
	LogLevel    string
}

// Load reads configuration with v1 defaults.
func Load() Config {
	return Config{
		DatabaseURL: envOr("DATABASE_URL", "postgres://localhost:5432/mneme?sslmode=disable"),
		APIPort:     envOr("PORT", envOr("API_PORT", "8080")),
		MCPMode:     envOr("MCP_MODE", "stdio"),
		MCPPort:     envOr("MCP_PORT", "8081"),
		LogLevel:    envOr("LOG_LEVEL", "info"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

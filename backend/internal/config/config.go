package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	Port            string
	AllowedOrigins  []string
	PingInterval    time.Duration
	SessionTimeout  time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
    CleanupInterval time.Duration
	ValidColors     []string
}

func LoadConfig() *Config {
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:5174")
	var origins []string
	if allowedOrigins == "*" {
		// NOTE: Wildcard origin is NOT allowed with credentials enabled.
		origins = []string{}
	} else {
		origins = strings.Split(allowedOrigins, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
	}

	config := &Config{
		Port:            getEnv("PORT", "8080"),
		AllowedOrigins:  origins,
		PingInterval:    30 * time.Second,
		SessionTimeout:  24 * time.Hour,
		ReadTimeout:     15 * time.Second,
		WriteTimeout:    15 * time.Second,
		IdleTimeout:     60 * time.Second,
		ShutdownTimeout: 5 * time.Second,
        CleanupInterval: 10 * time.Minute,
		ValidColors: []string{
			"rouge", "vert", "bleu", "jaune",  
			"orange", "violet", "rose", "gris",
		},
	}

	if envColors := os.Getenv("VALID_COLORS"); envColors != "" {
		colors := strings.Split(envColors, ",")
		for i := range colors {
			colors[i] = strings.TrimSpace(colors[i])
		}
		if len(colors) > 0 {
			config.ValidColors = colors
		}
	}

	return config
}

func (c *Config) IsOriginAllowed(origin string) bool {
	if len(c.AllowedOrigins) == 0 || c.AllowedOrigins[0] == "*" {
		return true
	}
	for _, allowed := range c.AllowedOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

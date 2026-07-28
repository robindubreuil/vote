package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host             string
	Port             string
	AllowedOrigins   []string
	AllowCredentials bool
	TrustedProxies   []string
	PingInterval     time.Duration
	SessionTimeout   time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	IdleTimeout      time.Duration
	ShutdownTimeout  time.Duration
	CleanupInterval  time.Duration
	ValidColors      []string
	// DashboardSecret gates the /dashboard route via an HMAC-signed cookie.
	// Empty => dashboard is disabled entirely (fail-closed).
	DashboardSecret string
	DashboardMaxAge time.Duration
	// DataDir is the FHS location for persistent stats (default /var/lib/vote
	// in prod, ./data for dev). Holds counters.json + stats.jsonl.
	DataDir string
	// StatsSampleInterval is how often the server flushes counters to disk.
	StatsSampleInterval time.Duration
	MaxSessionCreations int
	// Resource caps (S7). Zero or negative means "no cap" — useful for
	// tests and single-tenant dev. Production defaults are set in
	// LoadConfig and sized for a school-wide deployment: generous enough
	// that a building full of trainers and a lecture hall full of
	// stagiaires never hit them, tight enough that a flood (buggy client,
	// scripted abuse, replay storm) can't exhaust memory or fan-out
	// bandwidth.
	MaxSessionsGlobal    int
	MaxClientsPerSession int
	MaxConnectionsPerIP  int
}

func LoadConfig() *Config {
	allowedOrigins := getEnv("ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:5174,http://localhost:5177,http://localhost:5178")
	var origins []string
	allowCreds := true
	if allowedOrigins == "*" {
		origins = []string{}
		allowCreds = false
	} else {
		origins = strings.Split(allowedOrigins, ",")
		for i := range origins {
			origins[i] = strings.TrimSpace(origins[i])
		}
	}

	trustedProxies := []string{}
	if tp := os.Getenv("TRUSTED_PROXIES"); tp != "" {
		trustedProxies = strings.Split(tp, ",")
		for i := range trustedProxies {
			trustedProxies[i] = strings.TrimSpace(trustedProxies[i])
		}
	}

	config := &Config{
		Host:                getEnv("HOST", ""),
		Port:                getEnv("PORT", "8080"),
		AllowedOrigins:      origins,
		AllowCredentials:    allowCreds,
		TrustedProxies:      trustedProxies,
		PingInterval:        30 * time.Second,
		SessionTimeout:      getEnvDuration("SESSION_TIMEOUT", 1*time.Hour),
		ReadTimeout:         15 * time.Second,
		WriteTimeout:        15 * time.Second,
		IdleTimeout:         60 * time.Second,
		ShutdownTimeout:     5 * time.Second,
		CleanupInterval:     getEnvDuration("CLEANUP_INTERVAL", 10*time.Minute),
		DashboardSecret:     os.Getenv("VOTE_DASHBOARD_SECRET"),
		DashboardMaxAge:     getEnvDuration("VOTE_DASHBOARD_MAX_AGE", 7*24*time.Hour),
		DataDir:             getEnv("VOTE_DATA_DIR", "./data"),
		StatsSampleInterval: getEnvDuration("VOTE_STATS_INTERVAL", 5*time.Minute),
		MaxSessionCreations: getEnvInt("VOTE_MAX_SESSIONS_PER_HOUR", 20),
		// S7: resource caps. Defaults:
		//   - 1000 sessions globally (~3x the largest expected school
		//     deployment, caps memory growth from orphaned sessions).
		//   - 200 clients per session (covers any lecture hall, blocks
		//     a buggy client from spawning thousands of joins against
		//     one code).
		//   - 50 connections per IP (a full classroom behind one NAT,
		//     plus headroom; blocks a botnet-style flood from one source).
		MaxSessionsGlobal:    getEnvInt("VOTE_MAX_SESSIONS", 1000),
		MaxClientsPerSession: getEnvInt("VOTE_MAX_CLIENTS_PER_SESSION", 200),
		MaxConnectionsPerIP:  getEnvInt("VOTE_MAX_CONNECTIONS_PER_IP", 50),
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

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return defaultValue
}

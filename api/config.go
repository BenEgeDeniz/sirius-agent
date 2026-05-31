package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Redis connection
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// API authentication
	APIKeys []string

	// Domain configuration
	BaseDomain  string // e.g., "agent.example.com"
	UpstreamURL string // e.g., "https://upstream-server:8443"

	// Server settings
	ListenAddr string

	// Tunnel limits
	MaxTunnels        int
	MinTunnelDuration int // default 1 minute
	MaxTunnelDuration int // default 1440 minutes, -1 for unlimited

	// Rate limiting
	RateLimitRPM int // requests per minute per IP

	// Logging
	LogLevel string
}

// LoadConfig reads configuration from environment variables with sensible defaults.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		RedisAddr:     envOrDefault("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: envOrDefault("REDIS_PASSWORD", ""),
		RedisDB:       envOrDefaultInt("REDIS_DB", 0),
		BaseDomain:    os.Getenv("BASE_DOMAIN"),
		UpstreamURL:   os.Getenv("UPSTREAM_URL"),
		ListenAddr:    envOrDefault("LISTEN_ADDR", "127.0.0.1:8181"),
		MaxTunnels:        envOrDefaultInt("MAX_TUNNELS", 50),
		MinTunnelDuration: envOrDefaultInt("MIN_TUNNEL_DURATION", 1),
		MaxTunnelDuration: envOrDefaultInt("MAX_TUNNEL_DURATION", 1440),
		RateLimitRPM:      envOrDefaultInt("RATE_LIMIT_RPM", 30),
		LogLevel:      envOrDefault("LOG_LEVEL", "info"),
	}

	// Parse API keys (comma-separated)
	keysRaw := os.Getenv("API_KEYS")
	if keysRaw == "" {
		return nil, fmt.Errorf("API_KEYS environment variable is required")
	}
	for _, k := range strings.Split(keysRaw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			cfg.APIKeys = append(cfg.APIKeys, k)
		}
	}
	if len(cfg.APIKeys) == 0 {
		return nil, fmt.Errorf("at least one API key must be configured")
	}

	// Validate required fields
	if cfg.BaseDomain == "" {
		return nil, fmt.Errorf("BASE_DOMAIN environment variable is required")
	}
	if cfg.UpstreamURL == "" {
		return nil, fmt.Errorf("UPSTREAM_URL environment variable is required")
	}

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

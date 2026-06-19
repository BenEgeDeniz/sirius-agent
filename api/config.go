package main

import (
	"fmt"
	"net"
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

	// TCP tunnel settings
	TCPPortMin        int   // default 50000
	TCPPortMax        int   // default 60000
	TCPAllowedPorts   []int // allowed upstream ports
	TCPUpstreamHost   string
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
		LogLevel:          envOrDefault("LOG_LEVEL", "info"),
		TCPPortMin:        envOrDefaultInt("TCP_PORT_MIN", 50000),
		TCPPortMax:        envOrDefaultInt("TCP_PORT_MAX", 60000),
		TCPUpstreamHost:   os.Getenv("TCP_UPSTREAM_HOST"),
	}

	// Fallback for TCPUpstreamHost from UPSTREAM_URL
	if cfg.TCPUpstreamHost == "" && cfg.UpstreamURL != "" {
		hostPort := strings.TrimPrefix(cfg.UpstreamURL, "https://")
		hostPort = strings.TrimPrefix(hostPort, "http://")
		if host, _, err := net.SplitHostPort(hostPort); err == nil {
			cfg.TCPUpstreamHost = host
		} else {
			// If SplitHostPort fails (e.g. no port), just use the whole string
			cfg.TCPUpstreamHost = hostPort
		}
	}

	// Parse TCP allowed ports
	portsRaw := os.Getenv("TCP_ALLOWED_PORTS")
	if portsRaw == "" {
		cfg.TCPAllowedPorts = []int{22}
	} else {
		for _, p := range strings.Split(portsRaw, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if p == "*" || p == "any" || p == "all" {
				cfg.TCPAllowedPorts = append(cfg.TCPAllowedPorts, 0)
				continue
			}
			if strings.Contains(p, "-") {
				parts := strings.SplitN(p, "-", 2)
				minStr := strings.TrimSpace(parts[0])
				maxStr := strings.TrimSpace(parts[1])
				minPort, err1 := strconv.Atoi(minStr)
				maxPort, err2 := strconv.Atoi(maxStr)
				if err1 != nil || err2 != nil || minPort < 1 || maxPort > 65535 || minPort > maxPort {
					return nil, fmt.Errorf("invalid TCP_ALLOWED_PORTS range: %s", p)
				}
				for i := minPort; i <= maxPort; i++ {
					cfg.TCPAllowedPorts = append(cfg.TCPAllowedPorts, i)
				}
				continue
			}
			portInt, err := strconv.Atoi(p)
			if err != nil || portInt < 1 || portInt > 65535 {
				return nil, fmt.Errorf("invalid TCP_ALLOWED_PORTS value: %s", p)
			}
			cfg.TCPAllowedPorts = append(cfg.TCPAllowedPorts, portInt)
		}
	}
	if len(cfg.TCPAllowedPorts) == 0 {
		cfg.TCPAllowedPorts = []int{22}
	}

	// Validate TCP port range
	if cfg.TCPPortMin < 1024 || cfg.TCPPortMax <= cfg.TCPPortMin || cfg.TCPPortMax > 65535 {
		return nil, fmt.Errorf("invalid TCP port range: %d-%d", cfg.TCPPortMin, cfg.TCPPortMax)
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

const (
	// MaxDurationMinutes is the maximum allowed tunnel duration (24 hours).
	// -1 means unlimited (no expiration).
	MaxDurationMinutes = 1440
)

// TunnelInfo represents an active tunnel stored in Redis.
type TunnelInfo struct {
	Subdomain   string   `json:"subdomain"`
	URL         string   `json:"url"`
	CreatedAt   string   `json:"created_at"`
	ExpiresAt   string   `json:"expires_at"` // empty string if unlimited
	Duration    int      `json:"duration"`   // minutes, -1 = unlimited
	CreatedByIP string   `json:"created_by_ip"`
	AllowedIPs  []string `json:"allowed_ips"` // list of IPs or ["any"]
}

// TunnelService handles tunnel lifecycle operations.
type TunnelService struct {
	redis  *RedisClient
	config *Config
	logger *Logger
}

// NewTunnelService creates a new tunnel service.
func NewTunnelService(rdb *RedisClient, cfg *Config, logger *Logger) *TunnelService {
	return &TunnelService{
		redis:  rdb,
		config: cfg,
		logger: logger,
	}
}

// ValidateAllowedIPs checks that all entries are valid IPv4/IPv6 addresses or "any".
func ValidateAllowedIPs(ips []string) error {
	if len(ips) == 0 {
		return nil // empty = defaults to "any"
	}
	for _, ip := range ips {
		if ip == "any" || ip == "*" {
			continue
		}
		if net.ParseIP(ip) == nil {
			return fmt.Errorf("invalid IP address: %s", ip)
		}
	}
	return nil
}

// NormalizeAllowedIPs cleans up the allowed IPs list.
func NormalizeAllowedIPs(ips []string) []string {
	if len(ips) == 0 {
		return []string{"any"}
	}
	for _, ip := range ips {
		if ip == "any" || ip == "*" {
			return []string{"any"}
		}
	}
	return ips
}

// CreateTunnel generates a new ephemeral tunnel.
func (s *TunnelService) CreateTunnel(ctx context.Context, durationMinutes int, allowedIPs []string, clientIP string) (*TunnelInfo, error) {
	// Validate duration: -1 = unlimited, otherwise 1..MaxDurationMinutes
	if durationMinutes != -1 {
		if durationMinutes < 1 {
			return nil, fmt.Errorf("invalid duration: must be -1 (unlimited) or between 1 and %d minutes", MaxDurationMinutes)
		}
		if durationMinutes > MaxDurationMinutes {
			return nil, fmt.Errorf("invalid duration: maximum is %d minutes (24 hours)", MaxDurationMinutes)
		}
	}

	// Validate allowed IPs
	if err := ValidateAllowedIPs(allowedIPs); err != nil {
		return nil, err
	}
	allowedIPs = NormalizeAllowedIPs(allowedIPs)

	// Check tunnel count limit
	count, err := s.redis.CountKeys(ctx, "tunnel:*")
	if err != nil {
		return nil, fmt.Errorf("failed to count tunnels: %w", err)
	}
	if count >= s.config.MaxTunnels {
		return nil, fmt.Errorf("maximum tunnel limit reached (%d)", s.config.MaxTunnels)
	}

	// Generate unique subdomain
	subdomain, err := GenerateSubdomain(ctx, s.redis)
	if err != nil {
		return nil, fmt.Errorf("failed to generate subdomain: %w", err)
	}

	now := time.Now().UTC()
	tunnel := &TunnelInfo{
		Subdomain:   subdomain,
		URL:         fmt.Sprintf("https://%s.%s", subdomain, s.config.BaseDomain),
		CreatedAt:   now.Format(time.RFC3339),
		Duration:    durationMinutes,
		CreatedByIP: clientIP,
		AllowedIPs:  allowedIPs,
	}

	// Calculate TTL: -1 = no expiration (0 in Redis means persist forever)
	var ttl time.Duration
	if durationMinutes == -1 {
		ttl = 0 // Redis: 0 = no expiration
		tunnel.ExpiresAt = ""
	} else {
		ttl = time.Duration(durationMinutes) * time.Minute
		tunnel.ExpiresAt = now.Add(ttl).Format(time.RFC3339)
	}

	// Serialize and store in Redis
	data, err := json.Marshal(tunnel)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tunnel: %w", err)
	}

	if err := s.redis.Set(ctx, "tunnel:"+subdomain, string(data), ttl); err != nil {
		return nil, fmt.Errorf("failed to store tunnel: %w", err)
	}

	s.logger.Info("tunnel created",
		"subdomain", subdomain,
		"duration", durationMinutes,
		"allowed_ips", allowedIPs,
		"expires_at", tunnel.ExpiresAt,
		"client_ip", clientIP,
	)

	return tunnel, nil
}

// ListTunnels returns all active tunnels.
func (s *TunnelService) ListTunnels(ctx context.Context) ([]TunnelInfo, error) {
	keys, err := s.redis.ScanKeys(ctx, "tunnel:*")
	if err != nil {
		return nil, fmt.Errorf("failed to scan tunnels: %w", err)
	}

	var tunnels []TunnelInfo
	for _, key := range keys {
		data, err := s.redis.Get(ctx, key)
		if err != nil {
			continue
		}
		if data == "" {
			continue
		}

		var tunnel TunnelInfo
		if err := json.Unmarshal([]byte(data), &tunnel); err != nil {
			continue
		}

		tunnels = append(tunnels, tunnel)
	}

	if tunnels == nil {
		tunnels = []TunnelInfo{}
	}

	return tunnels, nil
}

// DeleteTunnel removes a tunnel before expiration.
func (s *TunnelService) DeleteTunnel(ctx context.Context, subdomain string) error {
	if !ValidateSubdomain(subdomain) {
		return fmt.Errorf("invalid subdomain format")
	}

	exists, err := s.redis.Exists(ctx, "tunnel:"+subdomain)
	if err != nil {
		return fmt.Errorf("failed to check tunnel: %w", err)
	}
	if !exists {
		return fmt.Errorf("tunnel not found: %s", subdomain)
	}

	if err := s.redis.Delete(ctx, "tunnel:"+subdomain); err != nil {
		return fmt.Errorf("failed to delete tunnel: %w", err)
	}

	s.logger.Info("tunnel deleted",
		"subdomain", subdomain,
	)

	return nil
}

// GetTunnel retrieves a single tunnel by subdomain.
func (s *TunnelService) GetTunnel(ctx context.Context, subdomain string) (*TunnelInfo, error) {
	if !ValidateSubdomain(subdomain) {
		return nil, fmt.Errorf("invalid subdomain format")
	}

	data, err := s.redis.Get(ctx, "tunnel:"+subdomain)
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel: %w", err)
	}
	if data == "" {
		return nil, nil
	}

	var tunnel TunnelInfo
	if err := json.Unmarshal([]byte(data), &tunnel); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel data: %w", err)
	}

	return &tunnel, nil
}

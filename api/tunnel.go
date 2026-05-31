package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
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
	// Validate duration: -1 = unlimited
	if durationMinutes == -1 {
		if s.config.MaxTunnelDuration != -1 {
			return nil, fmt.Errorf("invalid duration: unlimited (-1) is not permitted, maximum is %d minutes", s.config.MaxTunnelDuration)
		}
	} else {
		if durationMinutes < s.config.MinTunnelDuration {
			return nil, fmt.Errorf("invalid duration: minimum is %d minutes", s.config.MinTunnelDuration)
		}
		if s.config.MaxTunnelDuration != -1 && durationMinutes > s.config.MaxTunnelDuration {
			return nil, fmt.Errorf("invalid duration: maximum is %d minutes", s.config.MaxTunnelDuration)
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

// ExtendTunnel extends the expiration of an active tunnel by the given additional minutes.
// The total tunnel lifetime (from creation) must not exceed MaxTunnelDuration unless it is -1 (unlimited).
func (s *TunnelService) ExtendTunnel(ctx context.Context, subdomain string, additionalMinutes int) (*TunnelInfo, error) {
	if !ValidateSubdomain(subdomain) {
		return nil, fmt.Errorf("invalid subdomain format")
	}

	if additionalMinutes <= 0 {
		return nil, fmt.Errorf("invalid extension: additional minutes must be a positive integer")
	}

	// Retrieve existing tunnel
	data, err := s.redis.Get(ctx, "tunnel:"+subdomain)
	if err != nil {
		return nil, fmt.Errorf("failed to get tunnel: %w", err)
	}
	if data == "" {
		return nil, fmt.Errorf("tunnel not found: %s", subdomain)
	}

	var tunnel TunnelInfo
	if err := json.Unmarshal([]byte(data), &tunnel); err != nil {
		return nil, fmt.Errorf("failed to parse tunnel data: %w", err)
	}

	// Cannot extend an already-unlimited tunnel
	if tunnel.Duration == -1 {
		return nil, fmt.Errorf("tunnel is already unlimited, no extension needed")
	}

	now := time.Now().UTC()
	createdAt, err := time.Parse(time.RFC3339, tunnel.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tunnel creation time: %w", err)
	}

	// Current expiration
	currentExpiresAt, err := time.Parse(time.RFC3339, tunnel.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tunnel expiration time: %w", err)
	}

	// The new expiration time
	newExpiresAt := currentExpiresAt.Add(time.Duration(additionalMinutes) * time.Minute)

	// Enforce max tunnel duration: total lifetime from creation cannot exceed max
	if s.config.MaxTunnelDuration != -1 {
		maxExpiresAt := createdAt.Add(time.Duration(s.config.MaxTunnelDuration) * time.Minute)
		if newExpiresAt.After(maxExpiresAt) {
			return nil, fmt.Errorf("invalid extension: total tunnel duration would exceed maximum of %d minutes", s.config.MaxTunnelDuration)
		}
	}

	// If the tunnel has already expired, reject
	if currentExpiresAt.Before(now) {
		return nil, fmt.Errorf("tunnel has already expired")
	}

	// Calculate the new total duration in minutes (from creation)
	newDuration := int(newExpiresAt.Sub(createdAt).Minutes())

	// Update tunnel info
	tunnel.ExpiresAt = newExpiresAt.Format(time.RFC3339)
	tunnel.Duration = newDuration

	// Serialize and store
	updatedData, err := json.Marshal(&tunnel)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize tunnel: %w", err)
	}

	// Calculate new Redis TTL (time remaining from now)
	newTTL := newExpiresAt.Sub(now)
	if err := s.redis.Set(ctx, "tunnel:"+subdomain, string(updatedData), newTTL); err != nil {
		return nil, fmt.Errorf("failed to update tunnel: %w", err)
	}

	s.logger.Info("tunnel extended",
		"subdomain", subdomain,
		"additional_minutes", additionalMinutes,
		"new_duration", newDuration,
		"new_expires_at", tunnel.ExpiresAt,
	)

	return &tunnel, nil
}

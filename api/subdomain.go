package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
)

// Curated word lists for subdomain generation.
// These are intentionally safe, neutral words - no offensive or confusing terms.
var adjectives = []string{
	"brave", "calm", "cool", "dark", "deep",
	"fair", "fast", "firm", "free", "gold",
	"good", "gray", "keen", "kind", "late",
	"lean", "live", "long", "loud", "main",
	"mild", "neat", "next", "nice", "open",
	"pale", "pure", "rare", "real", "rich",
	"safe", "soft", "sure", "tall", "thin",
	"true", "vast", "warm", "wide", "wild",
	"bold", "dear", "dry", "dual", "easy",
	"even", "fine", "flat", "full", "glad",
}

var nouns = []string{
	"arch", "bark", "bay", "beam", "bell",
	"bird", "bolt", "book", "cape", "cave",
	"clay", "cove", "crow", "dale", "dawn",
	"deer", "dove", "dune", "echo", "edge",
	"fawn", "fern", "fish", "flag", "foam",
	"ford", "fox", "gate", "glen", "glow",
	"hawk", "haze", "hill", "isle", "jade",
	"kite", "lake", "leaf", "lynx", "mist",
	"moss", "nest", "nova", "opal", "orca",
	"palm", "peak", "pine", "pond", "rain",
	"reed", "reef", "rock", "rose", "sage",
	"seal", "snow", "star", "surf", "teal",
	"tide", "vale", "vine", "wave", "wolf",
}

var subdomainRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$`)

// ValidateSubdomain checks if a subdomain matches the allowed format.
func ValidateSubdomain(subdomain string) bool {
	if len(subdomain) < 3 || len(subdomain) > 40 {
		return false
	}
	return subdomainRegex.MatchString(subdomain)
}

// GenerateSubdomain creates a random, human-readable subdomain.
// Format: {adjective}-{noun}-{4hex} (e.g., "brave-fox-a3f1")
func GenerateSubdomain(ctx context.Context, rdb *RedisClient) (string, error) {
	const maxAttempts = 10

	for i := 0; i < maxAttempts; i++ {
		adj, err := randomChoice(adjectives)
		if err != nil {
			return "", fmt.Errorf("random adjective: %w", err)
		}

		noun, err := randomChoice(nouns)
		if err != nil {
			return "", fmt.Errorf("random noun: %w", err)
		}

		suffix, err := randomHex(2) // 2 bytes = 4 hex chars
		if err != nil {
			return "", fmt.Errorf("random hex: %w", err)
		}

		subdomain := fmt.Sprintf("%s-%s-%s", adj, noun, suffix)

		if !ValidateSubdomain(subdomain) {
			continue
		}

		// Check for collision in Redis
		exists, err := rdb.Exists(ctx, "tunnel:"+subdomain)
		if err != nil {
			return "", fmt.Errorf("redis check: %w", err)
		}
		if !exists {
			return subdomain, nil
		}
	}

	return "", fmt.Errorf("failed to generate unique subdomain after %d attempts", maxAttempts)
}

// randomChoice picks a random element from a string slice using crypto/rand.
func randomChoice(choices []string) (string, error) {
	b := make([]byte, 1)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return choices[int(b[0])%len(choices)], nil
}

// randomHex generates n random bytes and returns them as hex string.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

// Logger wraps slog for structured logging with tunnel-specific context.
type Logger struct {
	*slog.Logger
}

// NewLogger creates a structured JSON logger.
func NewLogger(level string) *Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})

	return &Logger{slog.New(handler)}
}

// AuthMiddleware validates API key authentication.
// Uses constant-time comparison to prevent timing attacks.
func AuthMiddleware(apiKeys []string, logger *Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			logger.Warn("authentication failed: missing header",
				"ip", clientIP(r),
				"path", r.URL.Path,
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "missing Authorization header",
			})
			return
		}

		// Expect "Bearer <key>"
		if !strings.HasPrefix(auth, "Bearer ") {
			logger.Warn("authentication failed: invalid format",
				"ip", clientIP(r),
				"path", r.URL.Path,
			)
			writeJSON(w, http.StatusUnauthorized, map[string]string{
				"error": "invalid Authorization format, expected: Bearer <key>",
			})
			return
		}

		providedKey := strings.TrimPrefix(auth, "Bearer ")

		authenticated := false
		for _, validKey := range apiKeys {
			if subtle.ConstantTimeCompare([]byte(providedKey), []byte(validKey)) == 1 {
				authenticated = true
				break
			}
		}

		if !authenticated {
			logger.Warn("authentication failed: invalid key",
				"ip", clientIP(r),
				"path", r.URL.Path,
			)
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "invalid API key",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimitMiddleware enforces per-IP request rate limiting using Redis.
func RateLimitMiddleware(rdb *RedisClient, rpm int, logger *Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		key := fmt.Sprintf("ratelimit:api:%s", ip)

		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		count, err := rdb.Increment(ctx, key, time.Minute)
		if err != nil {
			// Fail open — don't block requests if Redis is having issues
			logger.Error("rate limit check failed", "error", err, "ip", ip)
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rpm))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, int64(rpm)-count)))

		if count > int64(rpm) {
			logger.Warn("rate limit exceeded",
				"ip", ip,
				"count", count,
				"limit", rpm,
			)
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "rate limit exceeded, try again later",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs every request with timing information.
func LoggingMiddleware(logger *Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &statusResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
			"user_agent", r.UserAgent(),
		)
	})
}

// SecurityHeadersMiddleware adds security headers to all responses.
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Type", "application/json")
		next.ServeHTTP(w, r)
	})
}

// statusResponseWriter wraps http.ResponseWriter to capture status code.
type statusResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusResponseWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

// clientIP extracts the real client IP, respecting X-Forwarded-For from trusted proxy (OpenResty).
func clientIP(r *http.Request) string {
	// Trust X-Forwarded-For since we're behind OpenResty on localhost
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (client IP)
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	// Fallback to direct connection
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx != -1 {
		ip = ip[:idx]
	}
	return ip
}

// writeJSON sends a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

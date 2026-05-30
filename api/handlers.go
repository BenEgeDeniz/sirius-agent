package main

import (
	"encoding/json"
	"net/http"
	"strings"
)

// CreateTunnelRequest is the expected JSON body for tunnel creation.
type CreateTunnelRequest struct {
	Duration   int      `json:"duration"`             // minutes, -1 = unlimited
	AllowedIPs []string `json:"allowed_ips,omitempty"` // IPs or ["any"], defaults to ["any"]
}

// RegisterHandlers sets up all HTTP routes on the provided mux.
func RegisterHandlers(mux *http.ServeMux, svc *TunnelService, rdb *RedisClient, cfg *Config, logger *Logger) {
	// Health check — no auth required
	mux.HandleFunc("GET /api/health", handleHealth(rdb))

	// Protected tunnel endpoints
	tunnelMux := http.NewServeMux()
	tunnelMux.HandleFunc("POST /api/tunnels", handleCreateTunnel(svc, logger))
	tunnelMux.HandleFunc("GET /api/tunnels", handleListTunnels(svc))
	tunnelMux.HandleFunc("DELETE /api/tunnels/{subdomain}", handleDeleteTunnel(svc, logger))

	// Apply auth + rate limit middleware to tunnel endpoints
	protected := AuthMiddleware(cfg.APIKeys, logger,
		RateLimitMiddleware(rdb, cfg.RateLimitRPM, logger, tunnelMux))

	mux.Handle("/api/tunnels", protected)
	mux.Handle("/api/tunnels/", protected)

	// Catch-all for unknown routes
	mux.HandleFunc("/", handleNotFound())
}

// handleHealth returns system health status.
func handleHealth(rdb *RedisClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := rdb.Ping(r.Context())
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"status": "unhealthy",
				"redis":  "disconnected",
				"error":  err.Error(),
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "healthy",
			"redis":  "connected",
		})
	}
}

// handleCreateTunnel creates a new ephemeral tunnel.
func handleCreateTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON body",
			})
			return
		}

		// Duration 0 is not valid — must be -1 (unlimited) or positive minutes
		if req.Duration == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "duration is required: positive integer (minutes) or -1 for unlimited",
			})
			return
		}

		tunnel, err := svc.CreateTunnel(r.Context(), req.Duration, req.AllowedIPs, clientIP(r))
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "invalid duration") ||
				strings.Contains(errMsg, "maximum tunnel limit") ||
				strings.Contains(errMsg, "invalid IP") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tunnel creation failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusCreated, tunnel)
	}
}

// handleListTunnels returns all active tunnels.
func handleListTunnels(svc *TunnelService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tunnels, err := svc.ListTunnels(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to list tunnels",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tunnels": tunnels,
			"count":   len(tunnels),
		})
	}
}

// handleDeleteTunnel removes a tunnel before expiration.
func handleDeleteTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subdomain := r.PathValue("subdomain")
		if subdomain == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "subdomain is required",
			})
			return
		}

		err := svc.DeleteTunnel(r.Context(), subdomain)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": errMsg,
				})
				return
			}
			if strings.Contains(errMsg, "invalid subdomain") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tunnel deletion failed", "error", err, "subdomain", subdomain)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status": "deleted",
		})
	}
}

// handleNotFound returns 404 for unknown routes.
func handleNotFound() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "not found",
		})
	}
}

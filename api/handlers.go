package main

import (
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CreateTunnelRequest is the expected JSON body for tunnel creation.
type CreateTunnelRequest struct {
	Duration   int      `json:"duration"`              // minutes, -1 = unlimited
	AllowedIPs []string `json:"allowed_ips,omitempty"` // IPs or ["any"], defaults to ["any"]
}

// ExtendTunnelRequest is the expected JSON body for extending a tunnel's duration.
type ExtendTunnelRequest struct {
	AdditionalMinutes int `json:"additional_minutes"` // positive integer, minutes to add
}

// CreateTCPTunnelRequest is the expected JSON body for TCP tunnel creation.
type CreateTCPTunnelRequest struct {
	Duration     int      `json:"duration"`              // minutes, -1 = unlimited
	UpstreamPort int      `json:"upstream_port"`         // required, must be allowed
	AllowedIPs   []string `json:"allowed_ips,omitempty"` // IPs or ["any"], defaults to [caller]
}

// ExtendTCPTunnelRequest is the expected JSON body for extending a TCP tunnel.
type ExtendTCPTunnelRequest struct {
	AdditionalMinutes int `json:"additional_minutes"` // positive integer
}

// RegisterHandlers sets up all HTTP routes on the provided mux.
func RegisterHandlers(mux *http.ServeMux, svc *TunnelService, rdb *RedisClient, cfg *Config, logger *Logger) {
	// Health check - no auth required
	mux.HandleFunc("GET /api/health", handleHealth(rdb))

	// Protected tunnel endpoints
	tunnelMux := http.NewServeMux()
	tunnelMux.HandleFunc("POST /api/tunnels", handleCreateTunnel(svc, logger))
	tunnelMux.HandleFunc("GET /api/tunnels", handleListTunnels(svc))
	tunnelMux.HandleFunc("PATCH /api/tunnels/{subdomain}", handleExtendTunnel(svc, logger))
	tunnelMux.HandleFunc("DELETE /api/tunnels/{subdomain}", handleDeleteTunnel(svc, logger))

	tunnelMux.HandleFunc("POST /api/tunnels/tcp", handleCreateTCPTunnel(svc, logger))
	tunnelMux.HandleFunc("GET /api/tunnels/tcp", handleListTCPTunnels(svc))
	tunnelMux.HandleFunc("PATCH /api/tunnels/tcp/{port}", handleExtendTCPTunnel(svc, logger))
	tunnelMux.HandleFunc("DELETE /api/tunnels/tcp/{port}", handleDeleteTCPTunnel(svc, logger))

	// Apply auth + rate limit middleware to tunnel endpoints
	protected := AuthMiddleware(cfg.APIKeys, logger,
		RateLimitMiddleware(rdb, cfg.RateLimitRPM, logger, tunnelMux))

	mux.Handle("/api/tunnels", protected)
	mux.Handle("/api/tunnels/", protected)

	// Catch-all for unknown routes
	mux.HandleFunc("/", handleNotFound())
}

// checkServiceStatus returns true if the systemd service is active
func checkServiceStatus(service string) bool {
	cmd := exec.Command("systemctl", "is-active", "--quiet", service)
	return cmd.Run() == nil
}

// checkTLSCertificate checks the validity of the TLS certificate via live connection
func checkTLSCertificate() (bool, int) {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", "127.0.0.1:443", &tls.Config{
		InsecureSkipVerify: true, // We only want to read the dates, not validate the chain
	})
	if err != nil {
		return false, 0
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return false, 0
	}

	cert := certs[0]
	daysRemaining := int(time.Until(cert.NotAfter).Hours() / 24)
	return daysRemaining > 0, daysRemaining
}

// handleHealth returns system health status.
func handleHealth(rdb *RedisClient) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := "healthy"
		statusCode := http.StatusOK

		// Check Redis
		redisStatus := "connected"
		if err := rdb.Ping(r.Context()); err != nil {
			redisStatus = "disconnected"
			status = "degraded"
			statusCode = http.StatusServiceUnavailable // Redis is critical
		}

		// Check OpenResty
		openrestyStatus := "inactive"
		if checkServiceStatus("openresty") {
			openrestyStatus = "active"
		} else {
			status = "degraded"
		}

		// Check UFW
		ufwStatus := "inactive"
		if checkServiceStatus("ufw") {
			ufwStatus = "active"
		} else {
			status = "degraded" // Just degraded, API still works
		}

		// Check TLS
		tlsValid, tlsDays := checkTLSCertificate()
		if !tlsValid {
			status = "degraded"
		} else if tlsDays < 7 {
			status = "warning"
		}

		writeJSON(w, statusCode, map[string]interface{}{
			"status":    status,
			"redis":     redisStatus,
			"openresty": openrestyStatus,
			"ufw":       ufwStatus,
			"tls_cert": map[string]interface{}{
				"valid":          tlsValid,
				"days_remaining": tlsDays,
			},
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

		// Duration 0 is not valid - must be -1 (unlimited) or positive minutes
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

// handleExtendTunnel extends the duration of an active tunnel.
func handleExtendTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subdomain := r.PathValue("subdomain")
		if subdomain == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "subdomain is required",
			})
			return
		}

		var req ExtendTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON body",
			})
			return
		}

		if req.AdditionalMinutes <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "additional_minutes is required and must be a positive integer",
			})
			return
		}

		tunnel, err := svc.ExtendTunnel(r.Context(), subdomain, req.AdditionalMinutes)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": errMsg,
				})
				return
			}
			if strings.Contains(errMsg, "invalid") ||
				strings.Contains(errMsg, "already unlimited") ||
				strings.Contains(errMsg, "already expired") ||
				strings.Contains(errMsg, "would exceed") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tunnel extension failed", "error", err, "subdomain", subdomain)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusOK, tunnel)
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

// handleCreateTCPTunnel creates a new ephemeral TCP tunnel.
func handleCreateTCPTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateTCPTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON body",
			})
			return
		}

		if req.Duration == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "duration is required: positive integer (minutes) or -1 for unlimited",
			})
			return
		}

		if req.UpstreamPort == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "upstream_port is required",
			})
			return
		}

		tunnel, err := svc.CreateTCPTunnel(r.Context(), req.Duration, req.UpstreamPort, req.AllowedIPs, clientIP(r))
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "invalid duration") ||
				strings.Contains(errMsg, "maximum tunnel limit") ||
				strings.Contains(errMsg, "invalid IP") ||
				strings.Contains(errMsg, "not allowed") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": errMsg,
				})
				return
			}
			if strings.Contains(errMsg, "no available ports") {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tcp tunnel creation failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusCreated, tunnel)
	}
}

// handleListTCPTunnels returns all active TCP tunnels.
func handleListTCPTunnels(svc *TunnelService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tunnels, err := svc.ListTCPTunnels(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "failed to list tcp tunnels",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"tunnels": tunnels,
			"count":   len(tunnels),
		})
	}
}

// handleExtendTCPTunnel extends the duration of an active TCP tunnel.
func handleExtendTCPTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		portStr := r.PathValue("port")
		if portStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "port is required",
			})
			return
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid port format",
			})
			return
		}

		var req ExtendTCPTunnelRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid JSON body",
			})
			return
		}

		if req.AdditionalMinutes <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "additional_minutes is required and must be a positive integer",
			})
			return
		}

		tunnel, err := svc.ExtendTCPTunnel(r.Context(), port, req.AdditionalMinutes)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": errMsg,
				})
				return
			}
			if strings.Contains(errMsg, "invalid") ||
				strings.Contains(errMsg, "already unlimited") ||
				strings.Contains(errMsg, "already expired") ||
				strings.Contains(errMsg, "would exceed") {
				writeJSON(w, http.StatusBadRequest, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tcp tunnel extension failed", "error", err, "port", port)
			writeJSON(w, http.StatusInternalServerError, map[string]string{
				"error": "internal server error",
			})
			return
		}

		writeJSON(w, http.StatusOK, tunnel)
	}
}

// handleDeleteTCPTunnel removes a TCP tunnel.
func handleDeleteTCPTunnel(svc *TunnelService, logger *Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		portStr := r.PathValue("port")
		if portStr == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "port is required",
			})
			return
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "invalid port format",
			})
			return
		}

		err = svc.DeleteTCPTunnel(r.Context(), port)
		if err != nil {
			errMsg := err.Error()
			if strings.Contains(errMsg, "not found") {
				writeJSON(w, http.StatusNotFound, map[string]string{
					"error": errMsg,
				})
				return
			}
			logger.Error("tcp tunnel deletion failed", "error", err, "port", port)
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


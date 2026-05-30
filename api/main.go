package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Load configuration
	cfg, err := LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %s\n", err)
		os.Exit(1)
	}

	// Initialize logger
	logger := NewLogger(cfg.LogLevel)
	logger.Info("starting sirius agent API",
		"listen_addr", cfg.ListenAddr,
		"base_domain", cfg.BaseDomain,
		"max_tunnels", cfg.MaxTunnels,
		"rate_limit_rpm", cfg.RateLimitRPM,
	)

	// Connect to Redis
	rdb, err := NewRedisClient(cfg)
	if err != nil {
		logger.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer rdb.Close()
	logger.Info("connected to Redis", "addr", cfg.RedisAddr)

	// Initialize services
	tunnelSvc := NewTunnelService(rdb, cfg, logger)

	// Setup HTTP routes
	mux := http.NewServeMux()
	RegisterHandlers(mux, tunnelSvc, rdb, cfg, logger)

	// Apply global middleware
	handler := SecurityHeadersMiddleware(
		LoggingMiddleware(logger, mux),
	)

	// Create server
	server := &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		logger.Info("API server listening", "addr", cfg.ListenAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	logger.Info("shutdown signal received, draining connections...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", "error", err)
	}

	logger.Info("server stopped")
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

// TCPProxyManager handles dynamic creation of TCP proxy listeners.
type TCPProxyManager struct {
	rdb             *RedisClient
	cfg             *Config
	logger          *Logger
	activeListeners map[int]net.Listener
	mu              sync.Mutex
}

// NewTCPProxyManager creates a new TCP proxy manager.
func NewTCPProxyManager(rdb *RedisClient, cfg *Config, logger *Logger) *TCPProxyManager {
	return &TCPProxyManager{
		rdb:             rdb,
		cfg:             cfg,
		logger:          logger,
		activeListeners: make(map[int]net.Listener),
	}
}

// StartAllActiveTunnels reads all active TCP tunnels from Redis and starts listeners.
func (m *TCPProxyManager) StartAllActiveTunnels() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	keys, err := m.rdb.ScanKeys(ctx, "tcp_tunnel:*")
	if err != nil {
		m.logger.Error("failed to query active TCP tunnels on startup", "error", err)
		return
	}

	for _, key := range keys {
		data, err := m.rdb.Get(ctx, key)
		if err != nil || data == "" {
			continue
		}

		var tunnel TCPTunnelInfo
		if err := json.Unmarshal([]byte(data), &tunnel); err != nil {
			continue
		}

		if err := m.StartProxy(tunnel.Port); err != nil {
			m.logger.Error("failed to restore TCP proxy", "port", tunnel.Port, "error", err)
		} else {
			m.logger.Info("restored TCP proxy", "port", tunnel.Port, "upstream_port", tunnel.UpstreamPort)
		}
	}
}

// StartProxy starts a listener on the specified port.
func (m *TCPProxyManager) StartProxy(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.activeListeners[port]; exists {
		return nil // Already running
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", port, err)
	}

	m.activeListeners[port] = listener

	go m.acceptLoop(listener, port)
	return nil
}

// StopProxy closes the listener for the specified port.
// Note: Active connections are intentionally left open to drain naturally 
// so we do not abruptly interrupt active SSH or FTP sessions.
func (m *TCPProxyManager) StopProxy(port int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if listener, exists := m.activeListeners[port]; exists {
		listener.Close()
		delete(m.activeListeners, port)
		m.logger.Info("stopped TCP proxy", "port", port)
	}
}

func (m *TCPProxyManager) acceptLoop(listener net.Listener, port int) {
	for {
		clientConn, err := listener.Accept()
		if err != nil {
			// Listener closed or error
			return
		}

		go m.handleConnection(clientConn, port)
	}
}

func (m *TCPProxyManager) handleConnection(clientConn net.Conn, port int) {
	defer clientConn.Close()

	// 1. Get tunnel info from Redis
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	key := fmt.Sprintf("tcp_tunnel:%d", port)
	data, err := m.rdb.Get(ctx, key)
	if err != nil || data == "" {
		m.logger.Warn("TCP connection dropped: tunnel not found or expired", "port", port, "client_ip", clientConn.RemoteAddr().String())
		// Trigger cleanup just in case
		m.StopProxy(port)
		return
	}

	var tunnel TCPTunnelInfo
	if err := json.Unmarshal([]byte(data), &tunnel); err != nil {
		m.logger.Error("Failed to parse TCP tunnel info", "port", port, "error", err)
		return
	}

	// 2. Validate IP
	clientIP := extractRawIP(clientConn.RemoteAddr().String())
	if !isClientIPAllowed(clientIP, tunnel.AllowedIPs) {
		m.logger.Warn("TCP connection dropped: IP not allowed", "port", port, "client_ip", clientIP)
		return
	}

	// 3. Dial upstream
	upstreamAddr := net.JoinHostPort(m.cfg.TCPUpstreamHost, strconv.Itoa(tunnel.UpstreamPort))
	upstreamConn, err := net.DialTimeout("tcp", upstreamAddr, 5*time.Second)
	if err != nil {
		m.logger.Error("Failed to connect to upstream", "port", port, "upstream", upstreamAddr, "error", err)
		return
	}
	defer upstreamConn.Close()

	m.logger.Info("TCP proxied", "client", clientIP, "port", port, "upstream", upstreamAddr)

	// Enable TCP KeepAlives to prevent ghost connections from lingering indefinitely
	if tcpClientConn, ok := clientConn.(*net.TCPConn); ok {
		tcpClientConn.SetKeepAlive(true)
		tcpClientConn.SetKeepAlivePeriod(1 * time.Minute)
	}
	if tcpUpstreamConn, ok := upstreamConn.(*net.TCPConn); ok {
		tcpUpstreamConn.SetKeepAlive(true)
		tcpUpstreamConn.SetKeepAlivePeriod(1 * time.Minute)
	}

	// 4. Proxy traffic (with half-close support)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(upstreamConn, clientConn)
		if tcpUpstream, ok := upstreamConn.(*net.TCPConn); ok {
			tcpUpstream.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		io.Copy(clientConn, upstreamConn)
		if tcpClient, ok := clientConn.(*net.TCPConn); ok {
			tcpClient.CloseWrite()
		}
	}()

	wg.Wait()
}

func extractRawIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if parsed := net.ParseIP(host); parsed != nil {
		return parsed.String()
	}
	return host
}

func isClientIPAllowed(ip string, allowed []string) bool {
	for _, a := range allowed {
		if a == "any" || a == "*" || a == ip {
			return true
		}
	}
	return false
}

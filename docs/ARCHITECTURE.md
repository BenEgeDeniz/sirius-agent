# Architecture

## Request Flow

**HTTP Tunnels:**
```
Client → OpenResty (:443) → tunnel_lookup.lua → Redis GET tunnel:{subdomain}
                          ↓ (valid)
                     proxy_pass → Upstream (any HTTPS URL)
```

**TCP Tunnels:**
```
Client → Go API (:50000-60000) → Redis GET tcp_tunnel:{port}
                               ↓ (valid)
                          io.Copy → Upstream (UpstreamHost:Port)
```

**Management API:**
```
Admin → OpenResty (:443) → /api/* → Go API (:8181) → Redis
```

## Components

### OpenResty

- Wildcard server block matches `*.domain.xyz`, extracts subdomain via regex
- `tunnel_lookup.lua` runs in the `access` phase:
  - Per-IP rate limiting (`PROXY_RATE_LIMIT_RPM`, default 600 req/min, `lua_shared_dict`)
  - Redis lookup on `tunnel:{subdomain}`
  - IP access control against `allowed_ips`
- `rate_limit.lua` is a standalone reference - not loaded by nginx

### Go API

- Listens on `127.0.0.1:8181` for management
- Bearer key auth (constant-time comparison)
- Tunnel durations: configurable limits (default 1–1440 min), or `-1` for unlimited
- Tunnel extension: `PATCH` endpoint to add time, capped by `MAX_TUNNEL_DURATION`
- Per-IP rate limiting via Redis (`RATE_LIMIT_RPM`, default 30 req/min)
- Structured JSON logs → journald
- **Native TCP Proxy**: For TCP tunnels, the Go API binds ephemeral ports dynamically (e.g. `50000:60000`), verifies the client IP against Redis records lazily upon connection, and uses robust `io.Copy` to forward traffic to `UPSTREAM_HOST`. This fully decouples TCP from OpenResty for high-performance proxying.
- Static binary, no runtime deps

### Redis

- Keys: `tunnel:{subdomain}` (TTL = duration), `ratelimit:api:{ip}` (TTL = 60s)
- Unlimited tunnels have no TTL - deleted manually
- No persistence; bound to `127.0.0.1`; FLUSHALL/FLUSHDB/DEBUG disabled

### Upstream

Any HTTPS-reachable target: Tailscale MagicDNS, LAN IP, or public hostname. The nginx resolver tries `100.100.100.100` (Tailscale) then falls back to `8.8.8.8`.

## Data Model

**HTTP Tunnel:**
```json
{
  "subdomain": "brave-fox-a3f1",
  "url": "https://brave-fox-a3f1.agent.example.com",
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:10:00Z",
  "duration": 10,
  "created_by_ip": "203.0.113.50",
  "allowed_ips": ["198.51.100.1"]
}
```

**TCP Tunnel:**
```json
{
  "port": 52533,
  "host": "connect.agent.example.com",
  "upstream_port": 22,
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:10:00Z",
  "duration": 10,
  "created_by_ip": "203.0.113.50",
  "allowed_ips": ["any"]
}
```

## Security Boundaries

```
PUBLIC INTERNET
└── VPS
    ├── OpenResty  :80/:443  (public)
    ├── Go API     :8181     (localhost only)
    └── Redis      :6379     (localhost only)
         │ (if tunnel valid)
         └── Upstream (via network path of your choice)
```

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Single binary, no runtime deps |
| Auth | API keys | Single-admin, JWT is overkill |
| Proxy rate limit | `lua_shared_dict` | No Redis round-trip |
| API rate limit | Redis | Shared state across restarts |
| Subdomain format | `{adj}-{noun}-{hex}` | 212M+ combos, human-readable |
| Upstream target | Hardcoded at install | Prevents SSRF |
| Redis persistence | Disabled | Data is ephemeral by design |
| TCP Proxy | Native Go (`io.Copy`) | Eliminates complex `iptables` rules and NGINX stream masking issues, allowing exact port identification and IPv6 compatibility. |

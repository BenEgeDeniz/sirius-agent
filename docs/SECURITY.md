# Security

## Threat Model

| Threat | Mitigation |
|--------|-----------|
| Unauthorized tunnel creation | API key auth |
| SSRF / upstream manipulation | Upstream hardcoded at install time |
| Unauthorized tunnel usage | Per-tunnel IP allowlist |
| Subdomain enumeration | Rate limiting + random subdomains |
| Brute-force | Rate limiting + 256-bit keys |
| Expired tunnel access | Redis TTL + Lua validation |

## Controls

### Authentication
- `Authorization: Bearer <key>` - 256-bit hex keys from `/dev/urandom`
- Constant-time comparison (`crypto/subtle`)
- Multiple keys supported (comma-separated) for zero-downtime rotation
- Stored in `/etc/sirius-agent/env` (chmod 600)

### Anti-SSRF
HTTP Upstream is hardcoded in nginx at install time via `proxy_pass`. Users control only `duration` and `allowed_ips`.
TCP Upstreams are restricted by the `TCP_ALLOWED_PORTS` environment variable. Users cannot forward traffic to arbitrary ports.

### Rate Limiting

| Layer | Default | Env Var | Storage |
|-------|---------|---------|---------|
| Proxy (Lua, `tunnel_lookup.lua`) | 600 req/min/IP | `PROXY_RATE_LIMIT_RPM` | `lua_shared_dict` |
| API (Go) | 30 req/min/IP | `RATE_LIMIT_RPM` | Redis |

Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `Retry-After`

### Subdomains
`{adj}-{noun}-{4hex}` - 212M+ combinations, generated with `crypto/rand`, collision-checked against Redis.

### IP Access Control
Per-tunnel `allowed_ips` list enforced in `tunnel_lookup.lua` (for HTTP proxying via OpenResty) and natively in the Go API's TCP listener (for TCP tunnels) before any upstream connection is made. Defaults to `["any"]`.

### Network
- Go API: `127.0.0.1:8181` - localhost only
- Redis: `127.0.0.1:6379` - localhost only
- UFW: 22, 80, 443, and the ephemeral TCP port range (`TCP_PORT_MIN` to `TCP_PORT_MAX`) open; 6379 explicitly denied

### TLS
- Wildcard cert via Let's Encrypt, auto-renewed
- TLSv1.2/1.3 only, modern ciphers, HSTS, no session tickets

### Systemd Hardening
`NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`, `PrivateDevices`, `MemoryDenyWriteExecute`, `RestrictNamespaces`, dedicated `sirius-agent` user.

### Redis
FLUSHALL, FLUSHDB, DEBUG disabled. No persistence. 64 MB memory cap with LRU eviction.

## Security Headers

```
Strict-Transport-Security: max-age=63072000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY  (SAMEORIGIN on tunnel responses)
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
```

## Audit Log Events
Auth failures, rate limit hits, tunnel create/extend/delete, IP denials, proxy errors - all structured JSON via journald.

## Hardening Checklist
1. SSH key auth, disable password login
2. Fail2ban for SSH
3. Unattended security upgrades
4. Alerts on service failures and rate limit spikes
5. Rotate API keys periodically
6. Back up `/etc/sirius-agent/env` (only secrets file)

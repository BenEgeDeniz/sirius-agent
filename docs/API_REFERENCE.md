# API Reference

Base URL: `https://<BASE_DOMAIN>` (e.g., `https://agent.example.com`)

All responses are `Content-Type: application/json`.

---

## Authentication

All tunnel endpoints except health, require a Bearer token in the `Authorization` header:

```
Authorization: Bearer <API_KEY>
```

Keys are 256-bit hex strings configured via the `API_KEYS` environment variable.

---

## Common Error Responses

These responses can be returned by **any protected endpoint** (all except health check).

### `401 Unauthorized` — Missing Header

```json
{ "error": "missing Authorization header" }
```

### `401 Unauthorized` — Invalid Format

```json
{ "error": "invalid Authorization format, expected: Bearer <key>" }
```

### `403 Forbidden` — Invalid Key

```json
{ "error": "invalid API key" }
```

### `429 Too Many Requests` — Rate Limited

Headers: `Retry-After: 60`

```json
{ "error": "rate limit exceeded, try again later" }
```

---

## Rate Limit Headers

All protected endpoints include these headers on every response:

| Header | Description |
|--------|-------------|
| `X-RateLimit-Limit` | Configured requests per minute (from `RATE_LIMIT_RPM`) |
| `X-RateLimit-Remaining` | Remaining requests in the current window |
| `Retry-After` | Seconds to wait (only present on `429` responses) |

---

## Data Model — TunnelInfo

All tunnel responses use this object:

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

| Field | Type | Description |
|-------|------|-------------|
| `subdomain` | string | Generated subdomain (`{adj}-{noun}-{4hex}`) |
| `url` | string | Full tunnel URL |
| `created_at` | string | RFC 3339 UTC creation timestamp |
| `expires_at` | string | RFC 3339 UTC expiration timestamp, empty string if unlimited |
| `duration` | int | Total duration in minutes from creation, `-1` if unlimited |
| `created_by_ip` | string | IP address of the client that created the tunnel |
| `allowed_ips` | string[] | List of allowed client IPs, or `["any"]` for unrestricted |

### TCPTunnelInfo

TCP tunnels use a slightly different object structure:

```json
{
  "port": 52371,
  "host": "connect.agent.example.com",
  "upstream_port": 22,
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:10:00Z",
  "duration": 10,
  "created_by_ip": "203.0.113.50",
  "allowed_ips": ["203.0.113.50"]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `port` | int | Allocated random high port |
| `host` | string | Hostname to connect to (`connect.<BASE_DOMAIN>`) |
| `upstream_port` | int | The port on the upstream server to forward traffic to |
| `created_at` | string | RFC 3339 UTC creation timestamp |
| `expires_at` | string | RFC 3339 UTC expiration timestamp, empty string if unlimited |
| `duration` | int | Total duration in minutes from creation, `-1` if unlimited |
| `created_by_ip` | string | IP address of the client that created the tunnel |
| `allowed_ips` | string[] | List of allowed client IPs, defaults to `["any"]` for unrestricted |

---

## Endpoints

### TCP Endpoints
The API also supports identical endpoints for ephemeral TCP tunnels under the `/api/tunnels/tcp` prefix:
- `POST /api/tunnels/tcp` (uses `CreateTCPTunnelRequest` with `upstream_port`)
- `GET /api/tunnels/tcp`
- `PATCH /api/tunnels/tcp/{port}`
- `DELETE /api/tunnels/tcp/{port}`

They follow the exact same request/response patterns as the HTTP tunnel endpoints below, but operate on `TCPTunnelInfo` objects and use `port` as the identifier instead of `subdomain`.

---

### `GET /api/health`

Health check. **No authentication required.**

#### Request

No body or parameters.

#### Responses

**`200 OK`** — System fully healthy

```json
{
  "status": "healthy",
  "redis": "connected",
  "openresty": "active",
  "ufw": "active",
  "tls_cert": {
    "valid": true,
    "days_remaining": 89
  }
}
```

The `status` field can be `"healthy"`, `"warning"` (e.g., if TLS certificate expires in < 7 days), or `"degraded"` (if a non-critical component like UFW or OpenResty is inactive).

**`503 Service Unavailable`** — Critical failure (Redis unreachable)

```json
{
  "status": "degraded",
  "redis": "disconnected",
  "openresty": "active",
  "ufw": "active",
  "tls_cert": {
    "valid": true,
    "days_remaining": 89
  }
}
```

---

### `POST /api/tunnels`

Create a new ephemeral tunnel. **Requires authentication.**

#### Request Body

```json
{
  "duration": 10,
  "allowed_ips": ["203.0.113.50"]
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `duration` | int | Yes | Minutes until expiration. `-1` for unlimited (if permitted by `MAX_TUNNEL_DURATION`). Must not be `0`. |
| `allowed_ips` | string[] | No | IP allowlist. Defaults to `["any"]`. Each entry must be a valid IPv4/IPv6 address, `"any"`, or `"*"`. |

#### Responses

**`201 Created`** — Tunnel created successfully

```json
{
  "subdomain": "brave-fox-a3f1",
  "url": "https://brave-fox-a3f1.agent.example.com",
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:10:00Z",
  "duration": 10,
  "created_by_ip": "203.0.113.50",
  "allowed_ips": ["any"]
}
```

**`400 Bad Request`** — Invalid JSON body

```json
{ "error": "invalid JSON body" }
```

**`400 Bad Request`** — Missing or zero duration

```json
{ "error": "duration is required: positive integer (minutes) or -1 for unlimited" }
```

**`400 Bad Request`** — Duration below minimum

```json
{ "error": "invalid duration: minimum is <MIN_TUNNEL_DURATION> minutes" }
```

**`400 Bad Request`** — Duration above maximum

```json
{ "error": "invalid duration: maximum is <MAX_TUNNEL_DURATION> minutes" }
```

**`400 Bad Request`** — Unlimited not permitted

```json
{ "error": "invalid duration: unlimited (-1) is not permitted, maximum is <MAX_TUNNEL_DURATION> minutes" }
```

**`400 Bad Request`** — Tunnel limit reached

```json
{ "error": "maximum tunnel limit reached (<MAX_TUNNELS>)" }
```

**`400 Bad Request`** — Invalid IP in allowlist

```json
{ "error": "invalid IP address: <ip>" }
```

**`500 Internal Server Error`** — Unexpected failure

```json
{ "error": "internal server error" }
```

---

### `GET /api/tunnels`

List all active tunnels. **Requires authentication.**

#### Request

No body or parameters.

#### Responses

**`200 OK`** — Tunnel list

```json
{
  "tunnels": [
    {
      "subdomain": "brave-fox-a3f1",
      "url": "https://brave-fox-a3f1.agent.example.com",
      "created_at": "2025-01-15T10:00:00Z",
      "expires_at": "2025-01-15T10:10:00Z",
      "duration": 10,
      "created_by_ip": "203.0.113.50",
      "allowed_ips": ["any"]
    }
  ],
  "count": 1
}
```

Returns `"tunnels": []` and `"count": 0` when no tunnels are active.

**`500 Internal Server Error`** — Redis failure

```json
{ "error": "failed to list tunnels" }
```

---

### `PATCH /api/tunnels/{subdomain}`

Extend the duration of an active tunnel. **Requires authentication.**

The total tunnel lifetime (from `created_at` to new expiration) must not exceed `MAX_TUNNEL_DURATION`. If `MAX_TUNNEL_DURATION` is `-1` (unlimited), there is no cap on extensions.

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `subdomain` | string | The tunnel subdomain to extend |

#### Request Body

```json
{
  "additional_minutes": 30
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `additional_minutes` | int | Yes | Positive integer — minutes to add to the current expiration |

#### Responses

**`200 OK`** — Tunnel extended successfully

Returns the updated tunnel object with new `expires_at` and `duration` values:

```json
{
  "subdomain": "brave-fox-a3f1",
  "url": "https://brave-fox-a3f1.agent.example.com",
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:40:00Z",
  "duration": 40,
  "created_by_ip": "203.0.113.50",
  "allowed_ips": ["any"]
}
```

**`400 Bad Request`** — Missing subdomain

```json
{ "error": "subdomain is required" }
```

**`400 Bad Request`** — Invalid JSON body

```json
{ "error": "invalid JSON body" }
```

**`400 Bad Request`** — Invalid additional_minutes

```json
{ "error": "additional_minutes is required and must be a positive integer" }
```

**`400 Bad Request`** — Invalid subdomain format

```json
{ "error": "invalid subdomain format" }
```

**`400 Bad Request`** — Tunnel is already unlimited

```json
{ "error": "tunnel is already unlimited, no extension needed" }
```

**`400 Bad Request`** — Tunnel already expired

```json
{ "error": "tunnel has already expired" }
```

**`400 Bad Request`** — Extension would exceed maximum

```json
{ "error": "invalid extension: total tunnel duration would exceed maximum of <MAX_TUNNEL_DURATION> minutes" }
```

**`404 Not Found`** — Tunnel does not exist

```json
{ "error": "tunnel not found: <subdomain>" }
```

**`500 Internal Server Error`** — Unexpected failure

```json
{ "error": "internal server error" }
```

---

### `DELETE /api/tunnels/{subdomain}`

Revoke (delete) an active tunnel before its natural expiration. **Requires authentication.**

#### Path Parameters

| Parameter | Type | Description |
|-----------|------|-------------|
| `subdomain` | string | The tunnel subdomain to delete |

#### Responses

**`200 OK`** — Tunnel deleted

```json
{ "status": "deleted" }
```

**`400 Bad Request`** — Missing subdomain

```json
{ "error": "subdomain is required" }
```

**`400 Bad Request`** — Invalid subdomain format

```json
{ "error": "invalid subdomain format" }
```

**`404 Not Found`** — Tunnel does not exist

```json
{ "error": "tunnel not found: <subdomain>" }
```

**`500 Internal Server Error`** — Unexpected failure

```json
{ "error": "internal server error" }
```

---

## Unknown Routes

Any request to an unregistered path returns:

**`404 Not Found`**

```json
{ "error": "not found" }
```

---

## Security Headers

All responses include:

| Header | Value |
|--------|-------|
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `X-XSS-Protection` | `1; mode=block` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Content-Type` | `application/json` |

---

## Configuration Reference

These environment variables control the API behavior:

| Variable | Default | Description |
|----------|---------|-------------|
| `LISTEN_ADDR` | `127.0.0.1:8181` | API listen address |
| `MAX_TUNNELS` | `50` | Maximum concurrent tunnels |
| `MIN_TUNNEL_DURATION` | `1` | Minimum tunnel duration (minutes) |
| `MAX_TUNNEL_DURATION` | `1440` | Maximum tunnel duration (minutes), `-1` for unlimited |
| `RATE_LIMIT_RPM` | `30` | API requests per minute per IP |
| `API_KEYS` | — | Comma-separated Bearer keys (required) |
| `BASE_DOMAIN` | — | Wildcard base domain (required) |
| `UPSTREAM_URL` | — | Upstream HTTPS URL (required) |
| `TCP_PORT_MIN` | `50000` | Start of ephemeral TCP port range |
| `TCP_PORT_MAX` | `60000` | End of ephemeral TCP port range |
| `TCP_ALLOWED_PORTS`| `22` | Comma-separated list of allowed upstream ports and ranges (e.g., `22,4000-5000`, or `*` for any) |
| `TCP_UPSTREAM_HOST`| — | Upstream host for TCP forwarding (supports IPv4, IPv6, and hostnames) |

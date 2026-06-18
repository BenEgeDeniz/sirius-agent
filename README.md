# BenEgeDeniz Sirius Agent

A secure ephemeral reverse proxy that exposes an upstream server through temporary, authenticated tunnels.

```
Internet → OpenResty (*.domain.xyz) → Redis lookup → Upstream
```

## Architecture

| Component | Role |
|-----------|------|
| **OpenResty** | TLS termination, subdomain routing, rate limiting, IP access control (HTTP/HTTPS tunnels) |
| **Go API** | Tunnel CRUD, authentication, business logic, **Native TCP Reverse Proxy** (TCP tunnels) |
| **Redis** | Tunnel registry with TTL-based expiration |

## Quick Start

```bash
# 1. Build
bash scripts/build.sh

# 2. Copy to VPS and install
scp -r . root@your-vps:/opt/sirius-agent-src/
ssh root@your-vps "cd /opt/sirius-agent-src && bash install.sh"
```



## Usage

```bash
# Create a 10-minute tunnel
curl -X POST https://agent.example.com/api/tunnels \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"duration": 10}'

# Restricted to specific IPs
curl -X POST https://agent.example.com/api/tunnels \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"duration": 45, "allowed_ips": ["203.0.113.50"]}'

# Unlimited tunnel
curl -X POST https://agent.example.com/api/tunnels \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"duration": -1}'

# Extend a tunnel by 30 minutes
curl -X PATCH https://agent.example.com/api/tunnels/brave-fox-a3f1 \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"additional_minutes": 30}'

# Create a TCP tunnel (e.g., for SSH on port 22)
curl -X POST https://agent.example.com/api/tunnels/tcp \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"duration": 10, "upstream_port": 22}'
```

Response:

```json
{
  "subdomain": "brave-fox-a3f1",
  "url": "https://brave-fox-a3f1.agent.example.com",
  "created_at": "2025-01-15T10:00:00Z",
  "expires_at": "2025-01-15T10:10:00Z",
  "duration": 10,
  "allowed_ips": ["any"]
}
```

## API

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| `POST` | `/api/tunnels` | Bearer key | Create HTTP tunnel |
| `GET` | `/api/tunnels` | Bearer key | List active HTTP tunnels |
| `PATCH` | `/api/tunnels/{sub}` | Bearer key | Extend HTTP tunnel duration |
| `DELETE` | `/api/tunnels/{sub}` | Bearer key | Revoke HTTP tunnel |
| `POST` | `/api/tunnels/tcp` | Bearer key | Create TCP tunnel |
| `GET` | `/api/tunnels/tcp` | Bearer key | List active TCP tunnels |
| `PATCH` | `/api/tunnels/tcp/{port}`| Bearer key | Extend TCP tunnel duration |
| `DELETE` | `/api/tunnels/tcp/{port}`| Bearer key | Revoke TCP tunnel |
| `GET` | `/api/health` | None | Health check |

## Project Structure

```
sirius_agent/
├── api/                    # Go backend
├── proxy/                  # OpenResty config and Lua scripts
├── redis/                  # Redis config template
├── systemd/                # Service files
├── scripts/                # Build and utility scripts
├── docs/                   # Documentation
├── dist/                   # Compiled binary
├── install.sh
└── uninstall.sh
```

## Requirements

- Ubuntu 20.04+ or Debian 11+
- Wildcard DNS (`*.domain`) → VPS IP
- DNS provider API access (Let's Encrypt wildcard TLS)
- Go 1.22+ on dev machine (to build)
- Upstream reachable from the VPS over HTTPS (Tailscale, LAN, or public hostname)

## Docs

- [API Reference](docs/API_REFERENCE.md)
- [Setup](docs/SETUP.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Security](docs/SECURITY.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Upgrade](docs/UPGRADE.md)

## License

[MIT](LICENSE)

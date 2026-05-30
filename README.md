# BenEgeDeniz Sirius Agent

A secure ephemeral reverse proxy that exposes an upstream server through temporary, authenticated tunnels.

```
Internet → OpenResty (*.domain.xyz) → Redis lookup → Upstream
```

## Architecture

| Component | Role |
|-----------|------|
| **OpenResty** | TLS termination, subdomain routing, rate limiting, IP access control |
| **Go API** | Tunnel CRUD, authentication, business logic |
| **Redis** | Tunnel registry with TTL-based expiration |

## Quick Start

```bash
# 1. Build
bash scripts/build.sh

# 2. Copy to VPS and install
scp -r . root@your-vps:/opt/sirius-agent-src/
ssh root@your-vps "cd /opt/sirius-agent-src && bash install.sh"
```

Or non-interactively:

```bash
bash install.sh --domain agent.example.com --upstream upstream-server:8443
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
| `POST` | `/api/tunnels` | Bearer key | Create tunnel |
| `GET` | `/api/tunnels` | Bearer key | List active tunnels |
| `DELETE` | `/api/tunnels/{subdomain}` | Bearer key | Revoke tunnel |
| `GET` | `/api/health` | None | Health check |

## Project Structure

```
sirius_agent/
├── api/                    # Go backend
├── proxy/                  # OpenResty config and Lua scripts
├── redis/                  # Redis config template
├── systemd/                # Service files
├── scripts/                # Build, deploy, health-check
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

- [Setup](docs/SETUP.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Security](docs/SECURITY.md)
- [Troubleshooting](docs/TROUBLESHOOTING.md)
- [Upgrade](docs/UPGRADE.md)

## License

[MIT](LICENSE)

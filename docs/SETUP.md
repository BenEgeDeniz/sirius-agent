# Setup Guide

## Prerequisites

- VPS: Ubuntu 20.04+ or Debian 11+, root access, public IPv4
- Wildcard DNS: `*.your-domain.xyz → VPS_IP` and `your-domain.xyz → VPS_IP`
- DNS provider API access (for Let's Encrypt)
- Upstream reachable from VPS over HTTPS (Tailscale, LAN, or public)
- Go 1.22+ on dev machine

## 1. Build

```bash
bash scripts/build.sh
ls -la dist/sirius-api
```

## 2. Transfer

```bash
scp -r . root@YOUR_VPS_IP:/root/sirius-agent-src/
```

Or build + transfer in one step:

```bash
bash scripts/deploy.sh root@YOUR_VPS_IP
```

## 3. Install

```bash
ssh root@YOUR_VPS_IP
cd /root/sirius-agent-src
bash install.sh
```

Prompts for: domain, upstream host, upstream port, DNS provider, DNS credentials.

**Non-interactive:**

```bash
bash install.sh \
  --domain agent.example.com \
  --upstream upstream-server:8443 \
  --dns-provider cloudflare \
  --non-interactive
```

Place DNS credentials at `/etc/sirius-agent/dns-credentials.ini` beforehand.

**Flags:**

| Flag | Description |
|------|-------------|
| `--domain` | Wildcard base domain |
| `--upstream HOST:PORT` | Upstream host and port |
| `--dns-provider` | `cloudflare`, `digitalocean`, `route53`, `manual` |
| `--non-interactive` | Skip prompts |
| `--skip-tls` | Skip cert setup |
| `--binary PATH` | Pre-built binary path |

## 4. Verify

```bash
bash /opt/sirius-agent/scripts/health-check.sh
curl -s http://127.0.0.1:8181/api/health | jq .
journalctl -u sirius-api -f
```

## 5. Test

```bash
grep API_KEYS /etc/sirius-agent/env

curl -X POST https://your-domain.xyz/api/tunnels \
  -H 'Authorization: Bearer YOUR_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"duration": 10}'

curl -X DELETE https://your-domain.xyz/api/tunnels/SUBDOMAIN \
  -H 'Authorization: Bearer YOUR_KEY'
```

## 6. Verify Upstream

```bash
curl -k https://YOUR_UPSTREAM_HOST:YOUR_UPSTREAM_PORT

# Tailscale only
tailscale status
dig upstream-server @100.100.100.100
```

## 7. Configuration

All runtime configurations are stored in `/etc/sirius-agent/env`. If you edit this file, you must restart the relevant services to apply the changes.

### Tunnel Limits
- `MAX_TUNNELS`: Maximum number of concurrent tunnels (default: `50`)
- `MIN_TUNNEL_DURATION`: Minimum allowed tunnel duration in minutes (default: `1`)
- `MAX_TUNNEL_DURATION`: Maximum allowed tunnel duration in minutes. Set to `-1` to allow unlimited tunnels (default: `1440` which is 24 hours).

### Rate Limits
- `RATE_LIMIT_RPM`: Management API rate limit per IP. Low value prevents API spam (default: `30`)
- `PROXY_RATE_LIMIT_RPM`: Tunnel traffic proxy rate limit per IP. High value ensures web pages load correctly (default: `600`)

### Applying Changes
After saving your changes to `/etc/sirius-agent/env`:

```bash
# Apply tunnel limit and API rate limit changes
systemctl restart sirius-api

# Apply proxy rate limit changes
systemctl restart openresty
```

## DNS Examples

**Cloudflare** - add two A records (Proxy: OFF):
- `agent.example.com → VPS_IP`
- `*.agent.example.com → VPS_IP`

**DigitalOcean:**
```bash
doctl compute domain records create your-domain.xyz --record-type A --record-name "@" --record-data VPS_IP
doctl compute domain records create your-domain.xyz --record-type A --record-name "*" --record-data VPS_IP
```

## File Locations

| Path | Purpose |
|------|---------|
| `/usr/local/bin/sirius-api` | API binary |
| `/etc/sirius-agent/env` | Secrets (chmod 600) |
| `/etc/sirius-agent/nginx/` | OpenResty configs |
| `/etc/sirius-agent/tls/` | TLS cert symlinks |
| `/opt/sirius-agent/lua/` | Lua scripts |
| `/var/log/sirius-agent/` | Logs |

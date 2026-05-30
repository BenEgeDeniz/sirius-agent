#!/usr/bin/env bash
# Deploy sirius-agent to remote VPS
# Usage: ./deploy.sh [user@host]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

TARGET="${1:-}"
if [[ -z "$TARGET" ]]; then
    echo "Usage: $0 user@host"
    echo "Example: $0 root@203.0.113.10"
    exit 1
fi

echo "==> Building binary..."
bash "$SCRIPT_DIR/build.sh"

echo ""
echo "==> Deploying to $TARGET..."

# Upload binary
echo "  → Uploading binary..."
scp "$PROJECT_DIR/dist/sirius-api" "$TARGET:/usr/local/bin/sirius-api"
ssh "$TARGET" "chmod 755 /usr/local/bin/sirius-api"

# Upload config files (only if they don't exist on remote)
echo "  → Uploading configuration templates..."
ssh "$TARGET" "mkdir -p /opt/sirius-agent/lua /etc/sirius-agent/nginx/conf.d"

scp "$PROJECT_DIR/proxy/lua/tunnel_lookup.lua" "$TARGET:/opt/sirius-agent/lua/"
scp "$PROJECT_DIR/proxy/lua/rate_limit.lua" "$TARGET:/opt/sirius-agent/lua/"

# Upload nginx configs (will need template vars replaced)
scp "$PROJECT_DIR/proxy/nginx.conf" "$TARGET:/etc/sirius-agent/nginx/nginx.conf"
scp "$PROJECT_DIR/proxy/conf.d/proxy.conf" "$TARGET:/etc/sirius-agent/nginx/conf.d/proxy.conf"
scp "$PROJECT_DIR/proxy/conf.d/api.conf" "$TARGET:/etc/sirius-agent/nginx/conf.d/api.conf"

# Upload systemd service
scp "$PROJECT_DIR/systemd/sirius-api.service" "$TARGET:/etc/systemd/system/sirius-api.service"

# Restart services
echo "  → Restarting services..."
ssh "$TARGET" "systemctl daemon-reload && systemctl restart sirius-api && systemctl reload openresty"

# Health check
echo "  → Running health check..."
ssh "$TARGET" "curl -sf http://127.0.0.1:8181/api/health || echo 'WARNING: Health check failed'"

echo ""
echo "==> Deployment complete."

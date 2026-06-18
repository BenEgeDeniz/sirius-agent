#!/usr/bin/env bash
# ============================================================
# BenEgeDeniz Sirius Agent - Production Installer
# ============================================================
# Installs and configures the ephemeral reverse proxy system
# on a fresh Ubuntu/Debian server.
#
# Usage:
#   Interactive:    sudo bash install.sh
#   Non-interactive: sudo bash install.sh \
#                      --domain agent.example.com \
#                      --upstream upstream-server:8443 \
#                      --dns-provider cloudflare
#
# Requirements:
#   - Ubuntu 20.04+ or Debian 11+
#   - Root privileges
#   - Internet access
#   - Network access to the upstream target
# ============================================================
set -euo pipefail

# ---- Constants ----
INSTALL_DIR="/opt/sirius-agent"
CONFIG_DIR="/etc/sirius-agent"
LOG_DIR="/var/log/sirius-agent"
TLS_DIR="$CONFIG_DIR/tls"
NGINX_CONF_DIR="$CONFIG_DIR/nginx"
ENV_FILE="$CONFIG_DIR/env"
SERVICE_USER="sirius-agent"
BINARY_PATH="/usr/local/bin/sirius-api"

# ---- Colors ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

# ---- Helpers ----
log_info()  { echo -e "${BLUE}[INFO]${NC} $*"; }
log_ok()    { echo -e "${GREEN}[OK]${NC} $*"; }
log_warn()  { echo -e "${YELLOW}[WARN]${NC} $*"; }
log_error() { echo -e "${RED}[ERROR]${NC} $*"; }
log_step()  { echo -e "\n${BOLD}==> $*${NC}"; }

die() {
    log_error "$@"
    exit 1
}

# ---- Input Validation ----
validate_domain() {
    local domain="$1"
    if [[ ! "$domain" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]]; then
        return 1
    fi
    return 0
}

validate_hostname() {
    local host="$1"
    if [[ ! "$host" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$ ]]; then
        return 1
    fi
    return 0
}

validate_port() {
    local port="$1"
    if [[ ! "$port" =~ ^[0-9]+$ ]] || [[ "$port" -lt 1 ]] || [[ "$port" -gt 65535 ]]; then
        return 1
    fi
    return 0
}

generate_password() {
    head -c 32 /dev/urandom | xxd -p -c 64
}

# ---- Defaults ----
DOMAIN=""
UPSTREAM_HOST=""
UPSTREAM_PORT=""
TCP_PORT_MIN="50000"
TCP_PORT_MAX="60000"
TCP_ALLOWED_PORTS="22"
DNS_PROVIDER="cloudflare"
BINARY_SOURCE=""

# ---- Pre-flight Checks ----
log_step "Pre-flight checks"

# Must be root
if [[ $EUID -ne 0 ]]; then
    die "This script must be run as root (use sudo)"
fi

# Must be Debian/Ubuntu
if ! command -v apt-get &>/dev/null; then
    die "This installer requires a Debian/Ubuntu system with apt"
fi

UPGRADE_MODE=false
# Check for existing installation
if [[ -f "$ENV_FILE" ]]; then
    log_warn "Existing installation detected at $CONFIG_DIR"
    read -rp "Overwrite existing configuration? [y/N] " confirm
    if [[ ! "$confirm" =~ ^[Yy]$ ]]; then
        log_info "Preserving existing configuration. Upgrading binary and templates only."
        source "$ENV_FILE"
        DOMAIN="${BASE_DOMAIN:-}"
        API_KEY="${API_KEYS:-}"
        UPSTREAM_URL="${UPSTREAM_URL:-}"
        UPSTREAM_HOST="${TCP_UPSTREAM_HOST:-}"
        UPSTREAM_PORT=$(echo "$UPSTREAM_URL" | sed -E 's|https?://[^:]+:([0-9]+).*|\1|' || echo "")
        DOMAIN_ESCAPED=$(echo "$DOMAIN" | sed 's/\./\\\\./g')
        UPGRADE_MODE=true
    fi
fi

log_ok "Pre-flight checks passed"

if [[ "$UPGRADE_MODE" == false ]]; then
    # ---- Interactive Prompts ----
    echo ""
    echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
    echo -e "${BOLD}║       Sirius Agent - Installation Wizard     ║${NC}"
    echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
    echo ""

    # Domain
    echo -e "${BOLD}1. Wildcard Base Domain${NC}"
    echo "   This is the domain under which tunnel subdomains will be created."
    echo "   DNS should have a wildcard A record (*.domain) pointing to this server."
    echo ""
    while true; do
        read -rp "   Enter base domain (e.g., agent.example.com): " DOMAIN
        if validate_domain "$DOMAIN"; then
            break
        fi
        log_error "Invalid domain format. Use lowercase letters, numbers, dots, and hyphens."
    done

    # Upstream
    echo ""
    echo -e "${BOLD}2. Internal Upstream Target${NC}"
    echo "   This is the hostname or IP of your private server."
    echo "   The proxy will forward all tunnel traffic to this target."
    echo ""
    while true; do
        read -rp "   Enter upstream hostname (e.g., upstream-server): " UPSTREAM_HOST
        if validate_hostname "$UPSTREAM_HOST"; then
            break
        fi
        log_error "Invalid hostname. Use letters, numbers, and hyphens only."
    done

    while true; do
        read -rp "   Enter upstream port (e.g., 8443): " UPSTREAM_PORT
        if validate_port "$UPSTREAM_PORT"; then
            break
        fi
        log_error "Invalid port. Must be between 1 and 65535."
    done

    # TCP Settings
    echo ""
    echo -e "${BOLD}4. TCP Tunnel Settings${NC}"
    echo "   Ephemeral TCP tunnels allow temporary port forwarding (e.g., SSH, MySQL)."
    echo ""
    read -rp "   Enter TCP port range start [$TCP_PORT_MIN]: " input_min
    TCP_PORT_MIN="${input_min:-$TCP_PORT_MIN}"
    
    read -rp "   Enter TCP port range end [$TCP_PORT_MAX]: " input_max
    TCP_PORT_MAX="${input_max:-$TCP_PORT_MAX}"
    
    read -rp "   Enter allowed upstream ports (comma-separated, use * for any) [$TCP_ALLOWED_PORTS]: " input_ports
    TCP_ALLOWED_PORTS="${input_ports:-$TCP_ALLOWED_PORTS}"

    # DNS Provider
    echo ""
    echo -e "${BOLD}5. DNS Provider for TLS${NC}"
    echo "   Needed for Let's Encrypt wildcard certificate (DNS-01 challenge)."
    echo "   Supported: cloudflare, digitalocean, route53, manual"
    echo ""
    read -rp "   DNS provider [$DNS_PROVIDER]: " input_dns
    DNS_PROVIDER="${input_dns:-$DNS_PROVIDER}"

    # ---- Validate All Inputs ----
    log_step "Validating configuration"

    validate_domain "$DOMAIN" || die "Invalid domain: $DOMAIN"
    validate_hostname "$UPSTREAM_HOST" || die "Invalid upstream hostname: $UPSTREAM_HOST"
    validate_port "$UPSTREAM_PORT" || die "Invalid upstream port: $UPSTREAM_PORT"

    UPSTREAM_URL="https://${UPSTREAM_HOST}:${UPSTREAM_PORT}"
    TCP_UPSTREAM_HOST="$UPSTREAM_HOST"
    # Escape dots for nginx regex
    DOMAIN_ESCAPED=$(echo "$DOMAIN" | sed 's/\./\\\\./g')

    log_ok "Domain: $DOMAIN"
    log_ok "Upstream: $UPSTREAM_URL"
    log_ok "DNS Provider: $DNS_PROVIDER"
    log_ok "TCP Ports: $TCP_PORT_MIN-$TCP_PORT_MAX (Allowed upstream: $TCP_ALLOWED_PORTS)"

    # ---- Generate Secrets ----
    log_step "Generating secrets"

    API_KEY=$(generate_password)
    REDIS_PASSWORD=$(generate_password)

    log_ok "API key generated"
    log_ok "Redis password generated"
fi

# ---- Install System Packages ----
log_step "Installing system packages"

export DEBIAN_FRONTEND=noninteractive

apt-get update -qq

# Base packages
apt-get install -y -qq \
    curl \
    gnupg2 \
    ca-certificates \
    lsb-release \
    software-properties-common \
    ufw \
    xxd \
    jq \
    iptables-persistent \
    2>/dev/null

log_ok "Base packages installed"

# ---- Install Redis ----
log_step "Installing Redis"

apt-get install -y -qq redis-server 2>/dev/null
log_ok "Redis installed"

# ---- Install OpenResty ----
log_step "Installing OpenResty"

if ! command -v openresty &>/dev/null; then
    # Add OpenResty repository
    curl -fsSL https://openresty.org/package/pubkey.gpg | gpg --dearmor -o /usr/share/keyrings/openresty.gpg 2>/dev/null

    CODENAME=$(lsb_release -sc)
    DISTRO_ID=$(lsb_release -si | tr '[:upper:]' '[:lower:]')

    # OpenResty uses 'ubuntu' or 'debian' in their repo path
    if [[ "$DISTRO_ID" == "ubuntu" ]]; then
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/openresty.gpg] http://openresty.org/package/ubuntu $CODENAME main" \
            > /etc/apt/sources.list.d/openresty.list
    else
        echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/openresty.gpg] http://openresty.org/package/debian $CODENAME openresty" \
            > /etc/apt/sources.list.d/openresty.list
    fi

    apt-get update -qq
    apt-get install -y -qq openresty 2>/dev/null
fi

# Stop the default OpenResty instance (apt auto-starts it, which grabs port 443)
systemctl stop openresty 2>/dev/null || true
systemctl disable openresty 2>/dev/null || true
# Kill any stale processes holding the port
fuser -k 443/tcp 2>/dev/null || true
fuser -k 80/tcp 2>/dev/null || true

log_ok "OpenResty installed"

# ---- Install Certbot ----
log_step "Installing Certbot"

apt-get install -y -qq certbot 2>/dev/null

# Install DNS plugin
case "$DNS_PROVIDER" in
    cloudflare)
        apt-get install -y -qq python3-certbot-dns-cloudflare 2>/dev/null
        ;;
    digitalocean)
        apt-get install -y -qq python3-certbot-dns-digitalocean 2>/dev/null
        ;;
    route53)
        apt-get install -y -qq python3-certbot-dns-route53 2>/dev/null
        ;;
    manual)
        log_warn "Manual DNS mode selected - you'll need to add TXT records manually"
        ;;
    *)
        log_warn "Unknown DNS provider: $DNS_PROVIDER - certbot plugin not installed"
        ;;
esac

log_ok "Certbot installed"

# ---- Create System User ----
log_step "Creating system user"

if ! id "$SERVICE_USER" &>/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
    log_ok "Created user: $SERVICE_USER"
else
    log_ok "User already exists: $SERVICE_USER"
fi

# ---- Create Directory Structure ----
log_step "Creating directories"

mkdir -p "$INSTALL_DIR/lua"
mkdir -p "$CONFIG_DIR"
mkdir -p "$NGINX_CONF_DIR/conf.d"
mkdir -p "$TLS_DIR"
mkdir -p "$LOG_DIR"
mkdir -p /var/www/certbot
mkdir -p /run/openresty

chown -R "$SERVICE_USER:$SERVICE_USER" "$LOG_DIR"
chmod 750 "$LOG_DIR"
chmod 700 "$CONFIG_DIR"

log_ok "Directories created"

# ---- Configure Redis ----
log_step "Configuring Redis"

# Stop Redis before reconfiguring
systemctl stop redis-server 2>/dev/null || true

# Find the source config template
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Ensure Redis log directory exists with correct ownership
mkdir -p /var/log/redis
chown redis:redis /var/log/redis
chmod 750 /var/log/redis

if [[ -f "$SCRIPT_DIR/redis/redis.conf" ]]; then
    sed "s/__REDIS_PASSWORD__/$REDIS_PASSWORD/g" \
        "$SCRIPT_DIR/redis/redis.conf" \
        > /etc/redis/sirius-agent.conf

    chown redis:redis /etc/redis/sirius-agent.conf
    chmod 640 /etc/redis/sirius-agent.conf

    # Update Redis systemd to use our config
    mkdir -p /etc/systemd/system/redis-server.service.d
    cat > /etc/systemd/system/redis-server.service.d/sirius-agent.conf <<EOF
[Service]
ExecStart=
ExecStart=/usr/bin/redis-server /etc/redis/sirius-agent.conf
EOF

    systemctl daemon-reload

else
    log_warn "Redis config template not found, using defaults with password"
    # Fallback: just set password in the default config
    redis-cli CONFIG SET requirepass "$REDIS_PASSWORD" 2>/dev/null || true
fi

log_ok "Redis configured"

# ---- Configure OpenResty ----
log_step "Configuring OpenResty"

# Copy Lua scripts
if [[ -d "$SCRIPT_DIR/proxy/lua" ]]; then
    cp "$SCRIPT_DIR/proxy/lua/"*.lua "$INSTALL_DIR/lua/"
    log_ok "Lua scripts installed"
fi

# Process nginx.conf template
if [[ -f "$SCRIPT_DIR/proxy/nginx.conf" ]]; then
    cp "$SCRIPT_DIR/proxy/nginx.conf" "$NGINX_CONF_DIR/nginx.conf"
fi

# Process proxy.conf template
if [[ -f "$SCRIPT_DIR/proxy/conf.d/proxy.conf" ]]; then
    sed -e "s|__BASE_DOMAIN__|${DOMAIN}|g" \
        -e "s|__BASE_DOMAIN_ESCAPED__|${DOMAIN_ESCAPED}|g" \
        -e "s|__UPSTREAM_URL__|${UPSTREAM_URL}|g" \
        "$SCRIPT_DIR/proxy/conf.d/proxy.conf" \
        > "$NGINX_CONF_DIR/conf.d/proxy.conf"
    log_ok "Proxy config generated"
fi

# Process api.conf template
if [[ -f "$SCRIPT_DIR/proxy/conf.d/api.conf" ]]; then
    sed "s|__BASE_DOMAIN__|${DOMAIN}|g" \
        "$SCRIPT_DIR/proxy/conf.d/api.conf" \
        > "$NGINX_CONF_DIR/conf.d/api.conf"
    log_ok "API config generated"
fi

	# End API config

# Point OpenResty to our config
mkdir -p /etc/systemd/system/openresty.service.d
cat > /etc/systemd/system/openresty.service.d/sirius-agent.conf <<EOF
[Service]
PIDFile=/run/openresty/openresty.pid
ExecStartPre=
ExecStartPre=/usr/local/openresty/nginx/sbin/nginx -t -q -c $NGINX_CONF_DIR/nginx.conf
ExecStart=
ExecStart=/usr/local/openresty/bin/openresty -c $NGINX_CONF_DIR/nginx.conf
ExecReload=
ExecReload=/usr/local/openresty/nginx/sbin/nginx -c $NGINX_CONF_DIR/nginx.conf -s reload
EnvironmentFile=$ENV_FILE
EOF

log_ok "OpenResty configured"

# ---- Install Binary ----
log_step "Installing API binary"

if [[ -n "$BINARY_SOURCE" ]] && [[ -f "$BINARY_SOURCE" ]]; then
    cp "$BINARY_SOURCE" "$BINARY_PATH"
elif [[ -f "$SCRIPT_DIR/dist/sirius-api" ]]; then
    cp "$SCRIPT_DIR/dist/sirius-api" "$BINARY_PATH"
else
    log_warn "No pre-built binary found."
    log_warn "Build it with: bash scripts/build.sh"
    log_warn "Then re-run the installer with: --binary dist/sirius-api"
fi

if [[ -f "$BINARY_PATH" ]]; then
    chmod 755 "$BINARY_PATH"
    log_ok "Binary installed: $BINARY_PATH"
fi

if [[ "$UPGRADE_MODE" == false ]]; then
    # ---- Write Environment File ----
    log_step "Writing environment configuration"

    cat > "$ENV_FILE" <<EOF
# BenEgeDeniz Sirius Agent - Environment Configuration
# Generated by install.sh on $(date -u +"%Y-%m-%dT%H:%M:%SZ")
# SENSITIVE - do not share or commit this file

REDIS_ADDR=127.0.0.1:6379
REDIS_PASSWORD=${REDIS_PASSWORD}
REDIS_DB=0

API_KEYS=${API_KEY}

BASE_DOMAIN=${DOMAIN}
UPSTREAM_URL=${UPSTREAM_URL}

LISTEN_ADDR=127.0.0.1:8181
MAX_TUNNELS=50
MIN_TUNNEL_DURATION=1
MAX_TUNNEL_DURATION=1440
RATE_LIMIT_RPM=30
PROXY_RATE_LIMIT_RPM=600
LOG_LEVEL=info

TCP_PORT_MIN=${TCP_PORT_MIN}
TCP_PORT_MAX=${TCP_PORT_MAX}
TCP_ALLOWED_PORTS=${TCP_ALLOWED_PORTS}
TCP_UPSTREAM_HOST=${TCP_UPSTREAM_HOST}
EOF

    chmod 600 "$ENV_FILE"
    chown root:root "$ENV_FILE"

    log_ok "Environment file written: $ENV_FILE"

    # ---- Install Systemd Service ----
    log_step "Installing systemd service"

    if [[ -f "$SCRIPT_DIR/systemd/sirius-api.service" ]]; then
        cp "$SCRIPT_DIR/systemd/sirius-api.service" /etc/systemd/system/sirius-api.service
    fi

    systemctl daemon-reload
    log_ok "Systemd service installed"

    # ---- TLS Certificate ----
    log_step "Setting up TLS certificate"

    case "$DNS_PROVIDER" in
        cloudflare)
            echo ""
            echo -e "${BOLD}Cloudflare API Token${NC}"
            echo "   Create a token at: https://dash.cloudflare.com/profile/api-tokens"
            echo "   Required permissions: Zone:DNS:Edit"
            echo ""
            read -rsp "   Enter Cloudflare API token: " CF_TOKEN
            echo ""

            mkdir -p "$CONFIG_DIR"
            cat > "$CONFIG_DIR/dns-credentials.ini" <<EOF
dns_cloudflare_api_token = $CF_TOKEN
EOF
            chmod 600 "$CONFIG_DIR/dns-credentials.ini"

            if [[ -f "$CONFIG_DIR/dns-credentials.ini" ]]; then
                certbot certonly \
                    --dns-cloudflare \
                    --dns-cloudflare-credentials "$CONFIG_DIR/dns-credentials.ini" \
                    -d "$DOMAIN" \
                    -d "*.$DOMAIN" \
                    --non-interactive \
                    --agree-tos \
                    --email "admin@$DOMAIN" \
                    --cert-name sirius-agent \
                    2>&1 || log_warn "Certbot failed - you may need to set up TLS manually"

                # Symlink certs to our TLS directory
                if [[ -d "/etc/letsencrypt/live/sirius-agent" ]]; then
                    ln -sf /etc/letsencrypt/live/sirius-agent/fullchain.pem "$TLS_DIR/fullchain.pem"
                    ln -sf /etc/letsencrypt/live/sirius-agent/privkey.pem "$TLS_DIR/privkey.pem"
                    log_ok "TLS certificate obtained and linked"

                    # Setup auto-renewal hook to reload OpenResty
                    cat > /etc/letsencrypt/renewal-hooks/deploy/reload-openresty.sh <<'HOOK'
#!/bin/bash
systemctl reload openresty
HOOK
                    chmod +x /etc/letsencrypt/renewal-hooks/deploy/reload-openresty.sh
                    log_ok "Auto-renewal hook installed"
                fi
            else
                log_warn "No DNS credentials file found - skipping TLS"
            fi
            ;;

        digitalocean)
            read -rsp "Enter DigitalOcean API token: " DO_TOKEN
            echo ""
            cat > "$CONFIG_DIR/dns-credentials.ini" <<EOF
dns_digitalocean_token = $DO_TOKEN
EOF
            chmod 600 "$CONFIG_DIR/dns-credentials.ini"

            if [[ -f "$CONFIG_DIR/dns-credentials.ini" ]]; then
                certbot certonly \
                    --dns-digitalocean \
                    --dns-digitalocean-credentials "$CONFIG_DIR/dns-credentials.ini" \
                    -d "$DOMAIN" \
                    -d "*.$DOMAIN" \
                    --non-interactive \
                    --agree-tos \
                    --email "admin@$DOMAIN" \
                    --cert-name sirius-agent \
                    2>&1 || log_warn "Certbot failed"

                if [[ -d "/etc/letsencrypt/live/sirius-agent" ]]; then
                    ln -sf /etc/letsencrypt/live/sirius-agent/fullchain.pem "$TLS_DIR/fullchain.pem"
                    ln -sf /etc/letsencrypt/live/sirius-agent/privkey.pem "$TLS_DIR/privkey.pem"
                    log_ok "TLS certificate obtained"
                fi
            fi
            ;;

        manual)
            echo ""
            echo -e "${BOLD}Manual DNS Challenge${NC}"
            echo "   Certbot will ask you to create DNS TXT records."
            echo "   You will need access to your DNS provider's control panel."
            echo "   Wildcard certificates REQUIRE DNS validation."
            echo ""
            read -rp "   Run certbot now? [Y/n] " run_certbot
            run_certbot="${run_certbot:-Y}"

            if [[ "$run_certbot" =~ ^[Yy]$ ]]; then
                certbot certonly \
                    --manual \
                    --preferred-challenges dns \
                    -d "$DOMAIN" \
                    -d "*.$DOMAIN" \
                    --agree-tos \
                    --email "admin@$DOMAIN" \
                    --cert-name sirius-agent \
                    2>&1 || log_warn "Certbot failed - you can retry later"

                # Symlink certs if obtained
                if [[ -d "/etc/letsencrypt/live/sirius-agent" ]]; then
                    ln -sf /etc/letsencrypt/live/sirius-agent/fullchain.pem "$TLS_DIR/fullchain.pem"
                    ln -sf /etc/letsencrypt/live/sirius-agent/privkey.pem "$TLS_DIR/privkey.pem"
                    log_ok "TLS certificate obtained and linked"
                fi
            else
                log_info "Skipped. Run this when ready:"
                log_info "  certbot certonly --manual --preferred-challenges dns \\"
                log_info "    -d '$DOMAIN' -d '*.$DOMAIN' --cert-name sirius-agent"
                log_info ""
                log_info "Then link the certificates:"
                log_info "  ln -sf /etc/letsencrypt/live/sirius-agent/fullchain.pem $TLS_DIR/fullchain.pem"
                log_info "  ln -sf /etc/letsencrypt/live/sirius-agent/privkey.pem $TLS_DIR/privkey.pem"
                log_info "  systemctl start openresty"
            fi
            ;;

        *)
            log_warn "Unsupported DNS provider: $DNS_PROVIDER"
            log_warn "Please set up TLS manually"
            ;;
    esac

    # ---- Firewall ----
    log_step "Configuring firewall"

    ufw --force reset 2>/dev/null || true
    ufw default deny incoming
    ufw default allow outgoing
    ufw allow ssh
    ufw allow 80/tcp
    ufw allow 443/tcp
    ufw allow ${TCP_PORT_MIN}:${TCP_PORT_MAX}/tcp

    # Explicitly deny Redis from outside
    ufw deny 6379/tcp

    ufw --force enable

    log_ok "Firewall configured (SSH, HTTP, HTTPS allowed)"
fi

# ---- Start Services ----
log_step "Starting services"

systemctl enable redis-server
systemctl restart redis-server
log_ok "Redis started"

if [[ -f "$BINARY_PATH" ]]; then
    systemctl enable sirius-api
    systemctl start sirius-api
    log_ok "Sirius API started"
else
    log_warn "Sirius API binary not found - service not started"
fi

# Only start OpenResty if TLS certs exist
if [[ -f "$TLS_DIR/fullchain.pem" ]] && [[ -f "$TLS_DIR/privkey.pem" ]]; then
    systemctl enable openresty
    systemctl restart openresty
    log_ok "OpenResty started"
else
    log_warn "TLS certificates not found - OpenResty not started"
    log_warn "Install certificates and run: systemctl start openresty"
fi

# ---- Summary ----
echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║         Installation Complete!                ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""
echo -e "${BOLD}Configuration:${NC}"
echo "  Domain:      $DOMAIN"
echo "  Upstream:    $UPSTREAM_URL"
echo "  API Listen:  127.0.0.1:8181"
echo "  Redis:       127.0.0.1:6379"
echo ""
echo -e "${BOLD}Files:${NC}"
echo "  Config:      $ENV_FILE"
echo "  Binary:      $BINARY_PATH"
echo "  Logs:        $LOG_DIR"
echo "  TLS:         $TLS_DIR"
echo "  Nginx:       $NGINX_CONF_DIR"
echo ""
echo -e "${BOLD}API Key (save this!):${NC}"
echo -e "  ${GREEN}$API_KEY${NC}"
echo ""
echo -e "${BOLD}Usage:${NC}"
echo "  Create tunnel (10 min, any IP):"
echo "    curl -X POST https://$DOMAIN/api/tunnels \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"duration\": 10}'"
echo ""
echo "  Create tunnel (restricted IPs):"
echo "    curl -X POST https://$DOMAIN/api/tunnels \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"duration\": 30, \"allowed_ips\": [\"1.2.3.4\"]}'"
echo ""
echo "  Delete tunnel:"
echo "    curl -X DELETE https://$DOMAIN/api/tunnels/SUBDOMAIN \\"
echo "      -H 'Authorization: Bearer $API_KEY'"
echo ""
echo "  Extend tunnel (add 30 min):"
echo "    curl -X PATCH https://$DOMAIN/api/tunnels/SUBDOMAIN \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"additional_minutes\": 30}'"
echo ""
echo "  List tunnels:"
echo "    curl https://$DOMAIN/api/tunnels \\"
echo "      -H 'Authorization: Bearer $API_KEY'"
echo ""
echo "  List TCP tunnels:"
echo "    curl https://$DOMAIN/api/tunnels/tcp \\"
echo "      -H 'Authorization: Bearer $API_KEY'"
echo ""
echo "  Create TCP tunnel (SSH, 10 min, any IP):"
echo "    curl -X POST https://$DOMAIN/api/tunnels/tcp \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"duration\": 10, \"upstream_port\": 22}'"
echo ""
echo "  Create TCP tunnel (MySQL, allow specific IPs):"
echo "    curl -X POST https://$DOMAIN/api/tunnels/tcp \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"duration\": 30, \"upstream_port\": 3306, \"allowed_ips\": [\"1.2.3.4\"]}'"
echo ""
echo "  Extend TCP tunnel (add 30 min):"
echo "    curl -X PATCH https://$DOMAIN/api/tunnels/tcp/PORT \\"
echo "      -H 'Authorization: Bearer $API_KEY' \\"
echo "      -H 'Content-Type: application/json' \\"
echo "      -d '{\"additional_minutes\": 30}'"
echo ""
echo "  Delete TCP tunnel:"
echo "    curl -X DELETE https://$DOMAIN/api/tunnels/tcp/PORT \\"
echo "      -H 'Authorization: Bearer $API_KEY'"
echo ""
echo "  Health check:"
echo "    curl https://$DOMAIN/api/health"
echo ""
echo -e "${BOLD}Management:${NC}"
echo "  systemctl status sirius-api"
echo "  systemctl status openresty"
echo "  systemctl status redis-server"
echo "  journalctl -u sirius-api -f"
echo ""

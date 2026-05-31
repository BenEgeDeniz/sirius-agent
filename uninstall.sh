#!/usr/bin/env bash
# ============================================================
# BenEgeDeniz Sirius Agent - Uninstaller
# ============================================================
# Removes the sirius agent system from the server.
#
# Usage:
#   Interactive:      sudo bash uninstall.sh
#   Non-interactive:  sudo bash uninstall.sh --force
#   Full removal:     sudo bash uninstall.sh --force --purge
# ============================================================
set -euo pipefail

# ---- Constants ----
INSTALL_DIR="/opt/sirius-agent"
CONFIG_DIR="/etc/sirius-agent"
LOG_DIR="/var/log/sirius-agent"
BINARY_PATH="/usr/local/bin/sirius-api"
SERVICE_USER="sirius-agent"

# ---- Colors ----
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BOLD='\033[1m'
NC='\033[0m'

log_info()  { echo -e "  [INFO] $*"; }
log_ok()    { echo -e "  ${GREEN}[OK]${NC} $*"; }
log_warn()  { echo -e "  ${YELLOW}[WARN]${NC} $*"; }

# ---- Parse Arguments ----
FORCE=false
PURGE=false

while [[ $# -gt 0 ]]; do
    case "$1" in
        --force|-f)
            FORCE=true
            shift
            ;;
        --purge)
            PURGE=true
            shift
            ;;
        -h|--help)
            echo "Usage: sudo bash uninstall.sh [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --force, -f    Skip confirmation prompts"
            echo "  --purge        Also remove Redis, OpenResty, and logs"
            echo "  -h, --help     Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# ---- Pre-flight ----
if [[ $EUID -ne 0 ]]; then
    echo "This script must be run as root (use sudo)"
    exit 1
fi

echo ""
echo -e "${BOLD}╔══════════════════════════════════════════════╗${NC}"
echo -e "${BOLD}║       BenEgeDeniz Sirius Agent - Uninstaller             ║${NC}"
echo -e "${BOLD}╚══════════════════════════════════════════════╝${NC}"
echo ""

if [[ "$FORCE" == false ]]; then
    echo -e "${RED}WARNING: This will remove the sirius agent system.${NC}"
    echo ""
    echo "The following will be removed:"
    echo "  • Sirius API service and binary"
    echo "  • OpenResty configuration"
    echo "  • Redis configuration overrides"
    echo "  • Environment and secrets"
    echo "  • Lua scripts"
    if [[ "$PURGE" == true ]]; then
        echo "  • Redis server (package)"
        echo "  • OpenResty (package)"
        echo "  • Log files"
        echo "  • TLS certificates"
    fi
    echo ""
    read -rp "Are you sure? Type 'yes' to confirm: " confirm
    if [[ "$confirm" != "yes" ]]; then
        echo "Cancelled."
        exit 0
    fi
fi

# ---- Stop Services ----
echo ""
echo -e "${BOLD}==> Stopping services${NC}"

if systemctl is-active --quiet sirius-api 2>/dev/null; then
    systemctl stop sirius-api
    log_ok "Stopped sirius-api"
fi

if systemctl is-enabled --quiet sirius-api 2>/dev/null; then
    systemctl disable sirius-api
    log_ok "Disabled sirius-api"
fi

# ---- Remove Systemd Units ----
echo ""
echo -e "${BOLD}==> Removing systemd configuration${NC}"

rm -f /etc/systemd/system/sirius-api.service
rm -rf /etc/systemd/system/openresty.service.d/sirius-agent.conf
rm -rf /etc/systemd/system/redis-server.service.d/sirius-agent.conf

systemctl daemon-reload
log_ok "Systemd units removed"

# ---- Remove Binary ----
echo ""
echo -e "${BOLD}==> Removing binary${NC}"

rm -f "$BINARY_PATH"
log_ok "Removed $BINARY_PATH"

# ---- Remove Configuration ----
echo ""
echo -e "${BOLD}==> Removing configuration${NC}"

rm -rf "$CONFIG_DIR"
log_ok "Removed $CONFIG_DIR"

rm -rf "$INSTALL_DIR"
log_ok "Removed $INSTALL_DIR"

rm -rf /var/www/certbot
log_ok "Removed /var/www/certbot"

rm -f /etc/logrotate.d/sirius-agent
log_ok "Removed logrotate config (if any)"

# ---- Remove Redis Overrides ----
echo ""
echo -e "${BOLD}==> Removing Redis overrides${NC}"

rm -f /etc/redis/sirius-agent.conf
rm -f /etc/redis/redis-tunnel.conf
log_ok "Redis overrides removed"

# Restart Redis with default config
if systemctl is-active --quiet redis-server 2>/dev/null; then
    systemctl restart redis-server
    log_ok "Redis restarted with default config"
fi

# ---- Remove OpenResty Overrides ----
echo ""
echo -e "${BOLD}==> Removing OpenResty overrides${NC}"

# Restart OpenResty with default config if still installed
if command -v openresty &>/dev/null; then
    if systemctl is-active --quiet openresty 2>/dev/null; then
        systemctl stop openresty
        log_ok "Stopped OpenResty"
    fi
fi

# ---- Remove Firewall Rules ----
echo ""
echo -e "${BOLD}==> Removing firewall rules${NC}"

if command -v ufw &>/dev/null; then
    ufw delete allow 80/tcp 2>/dev/null || true
    ufw delete allow 443/tcp 2>/dev/null || true
    ufw delete deny 6379/tcp 2>/dev/null || true
    log_ok "Firewall rules removed"
fi

# ---- Purge (optional) ----
if [[ "$PURGE" == true ]]; then
    echo ""
    echo -e "${BOLD}==> Purging packages and data${NC}"

    # Remove logs
    rm -rf "$LOG_DIR"
    log_ok "Removed log directory"

    # Remove TLS certificates
    certbot delete --cert-name sirius-agent --non-interactive 2>/dev/null || true
    rm -f /etc/letsencrypt/renewal-hooks/deploy/reload-openresty.sh 2>/dev/null || true
    log_ok "TLS certificates removed"

    # Remove packages and repos (ask first unless --force)
    if [[ "$FORCE" == true ]] || { read -rp "Remove Redis and OpenResty packages? [y/N] " pkg_confirm && [[ "$pkg_confirm" =~ ^[Yy]$ ]]; }; then
        apt-get remove -y --purge openresty 2>/dev/null || true
        rm -f /etc/apt/sources.list.d/openresty.list 2>/dev/null || true
        rm -f /usr/share/keyrings/openresty.gpg 2>/dev/null || true
        # Don't auto-remove Redis as other services might use it
        log_ok "Packages and apt repos removed"
    fi
fi

# ---- Remove Service User ----
echo ""
echo -e "${BOLD}==> Removing service user${NC}"

if id "$SERVICE_USER" &>/dev/null; then
    userdel "$SERVICE_USER" 2>/dev/null || true
    log_ok "Removed user: $SERVICE_USER"
fi

# ---- Done ----
echo ""
echo -e "${GREEN}${BOLD}Uninstallation complete.${NC}"
echo ""

if [[ "$PURGE" == false ]]; then
    echo "Note: Redis and OpenResty packages were preserved."
    echo "Run with --purge to fully remove them."
    echo ""
    echo "Log files preserved at: $LOG_DIR"
    echo "Run with --purge to remove logs too."
fi
echo ""

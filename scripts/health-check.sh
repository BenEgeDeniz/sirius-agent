#!/usr/bin/env bash
# Health check script for sirius-agent system
# Verifies all components are running and healthy
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
NC='\033[0m'

PASS=0
FAIL=0

check() {
    local name="$1"
    local cmd="$2"

    if eval "$cmd" &>/dev/null; then
        echo -e "  ${GREEN}✓${NC} $name"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}✗${NC} $name"
        FAIL=$((FAIL + 1))
    fi
}

echo "==> BenEgeDeniz Sirius Agent Health Check"
echo ""

# System services
echo "Services:"
check "Redis is running" "systemctl is-active --quiet redis-server"
check "Sirius API is running" "systemctl is-active --quiet sirius-api"
check "OpenResty is running" "systemctl is-active --quiet openresty"
echo ""

# Connectivity
echo "Connectivity:"
check "Redis is reachable" "redis-cli -a \$(grep REDIS_PASSWORD /etc/sirius-agent/env 2>/dev/null | cut -d= -f2) ping 2>/dev/null | grep -q PONG"
check "API health endpoint" "curl -sf http://127.0.0.1:8181/api/health | grep -q healthy"
check "HTTPS is responding" "curl -sfk https://127.0.0.1:443/ -o /dev/null -w '%{http_code}' 2>/dev/null | grep -qE '(200|301|444)'"
echo ""

# TLS
echo "TLS:"
if [[ -f /etc/sirius-agent/tls/fullchain.pem ]]; then
    EXPIRY=$(openssl x509 -enddate -noout -in /etc/sirius-agent/tls/fullchain.pem 2>/dev/null | cut -d= -f2)
    EXPIRY_EPOCH=$(date -d "$EXPIRY" +%s 2>/dev/null || echo 0)
    NOW_EPOCH=$(date +%s)
    DAYS_LEFT=$(( (EXPIRY_EPOCH - NOW_EPOCH) / 86400 ))

    if [[ $DAYS_LEFT -gt 30 ]]; then
        echo -e "  ${GREEN}✓${NC} TLS certificate valid ($DAYS_LEFT days remaining)"
        PASS=$((PASS + 1))
    elif [[ $DAYS_LEFT -gt 0 ]]; then
        echo -e "  ${YELLOW}⚠${NC} TLS certificate expiring soon ($DAYS_LEFT days remaining)"
        PASS=$((PASS + 1))
    else
        echo -e "  ${RED}✗${NC} TLS certificate expired!"
        FAIL=$((FAIL + 1))
    fi
else
    echo -e "  ${YELLOW}⚠${NC} TLS certificate not found (not yet installed?)"
fi
echo ""

# Firewall
echo "Firewall:"
check "UFW is active" "ufw status | grep -q 'Status: active'"
echo ""

# Summary
echo "---"
echo -e "Results: ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi

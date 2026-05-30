#!/usr/bin/env bash
# Generate a cryptographically secure API key
set -euo pipefail

KEY=$(head -c 32 /dev/urandom | xxd -p -c 64)

echo "Generated API key:"
echo ""
echo "  $KEY"
echo ""
echo "Add this to your .env or /etc/sirius-agent/env:"
echo "  API_KEYS=$KEY"

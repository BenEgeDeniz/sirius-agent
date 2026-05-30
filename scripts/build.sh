#!/usr/bin/env bash
# Build the sirius-api binary for production deployment
# Cross-compiles for linux/amd64 from any OS
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
API_DIR="$PROJECT_DIR/api"
DIST_DIR="$PROJECT_DIR/dist"

echo "==> Building sirius-api..."

mkdir -p "$DIST_DIR"

cd "$API_DIR"

# Cross-compile static binary for Linux amd64
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
    go build -ldflags="-s -w" -o "$DIST_DIR/sirius-api" .

echo "==> Built: $DIST_DIR/sirius-api"
ls -lh "$DIST_DIR/sirius-api"
echo "==> Done."

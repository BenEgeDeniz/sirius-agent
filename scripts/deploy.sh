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

REMOTE_DIR="/root/sirius-agent"

echo "  → Creating remote directory..."
ssh "$TARGET" "mkdir -p $REMOTE_DIR"

echo "  → Uploading project files..."
rsync -avz --delete --exclude '.git' --exclude '.idea' --exclude '.vscode' "$PROJECT_DIR/" "$TARGET:$REMOTE_DIR/"

echo ""
echo "==> Deployment complete."
echo "The project files and built binary are now on the remote server."
echo "To install or update, connect to the server and run:"
echo "  ssh $TARGET"
echo "  cd $REMOTE_DIR"
echo "  bash install.sh"

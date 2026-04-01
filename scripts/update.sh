#!/bin/bash
set -e
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'
log()   { echo -e "${GREEN}[+]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
cd "$REPO_ROOT"
GO=/usr/local/go/bin/go
[ ! -f "$GO" ] && error "Go not found at $GO"
# ── Build ──────────────────────────────────────────────
log "Building fileshare-backend..."
GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-backend ./cmd/server/
log "Building fileshare-interface..."
GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-interface ./cmd/cli/
log "Build complete."
# ── Deploy (only if already installed) ─────────────
if [ -d "/opt/fileshare" ]; then
    log "Stopping Service..."
    sudo systemctl stop fileshare.service 2>/dev/null || true
    log "Deploying to /opt/fileshare..."
    sudo cp fileshare-backend   /opt/fileshare/fileshare-backend
    sudo cp fileshare-interface /opt/fileshare/fileshare-interface
    sudo rm -rf /opt/fileshare/web
    sudo cp -r assets/web /opt/fileshare/web
    sudo restorecon -v /opt/fileshare/fileshare-backend   2>/dev/null || true
    sudo restorecon -v /opt/fileshare/fileshare-interface 2>/dev/null || true
    log "Restarting Service..."
    sudo systemctl start fileshare.service
    log "Update complete."
else
    log "Build complete. /opt/fileshare not found – for initial installation, run 'sudo bash scripts/install.sh'."
fi
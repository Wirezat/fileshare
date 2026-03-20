#!/bin/bash
# Fileshare Installer
# Voraussetzung: scripts/update.sh wurde vorher ausgeführt
# Usage: sudo bash scripts/install.sh

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

[ "$EUID" -ne 0 ] && error "Please run as root: sudo bash scripts/install.sh"

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="/opt/fileshare"

# Check if binaries exist
[ ! -f "$REPO_ROOT/fileshare-backend" ]   && error "fileshare-backend not found. Please run 'bash scripts/update.sh' first."
[ ! -f "$REPO_ROOT/fileshare-interface" ] && error "fileshare-interface not found. Please run 'bash scripts/update.sh' first."

log "Creating $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"
chown -R ${SUDO_USER:-$USER}:${SUDO_USER:-$USER} "$INSTALL_DIR"

log "Copying Binaries..."
cp "$REPO_ROOT/fileshare-backend"   "$INSTALL_DIR/fileshare-backend"
cp "$REPO_ROOT/fileshare-interface" "$INSTALL_DIR/fileshare-interface"
chmod +x "$INSTALL_DIR/fileshare-backend"
chmod +x "$INSTALL_DIR/fileshare-interface"

log "Copying Assets..."
cp "$REPO_ROOT/assets/template.html" "$INSTALL_DIR/template.html"

# Create data.json only if it doesn't exist
if [ ! -f "$INSTALL_DIR/data.json" ]; then
    log "Creating initial data.json from example config..."
    cp "$REPO_ROOT/configs/data.example.json" "$INSTALL_DIR/data.json"
    warn "Please configure $INSTALL_DIR/data.json!"
else
    log "data.json already exists – not overwriting."
fi

log "Install systemd Service..."
cp "$REPO_ROOT/scripts/fileshare.service" /etc/systemd/system/fileshare.service
/sbin/restorecon -v /etc/systemd/system/fileshare.service 2>/dev/null || true
systemctl daemon-reload
systemctl enable --now fileshare.service

log "Create CLI Symlink..."
LINK="/usr/local/bin/fileshare"
[ -L "$LINK" ] && rm "$LINK"
ln -s "$INSTALL_DIR/fileshare-interface" "$LINK"
log "  $LINK → $INSTALL_DIR/fileshare-interface"

echo ""
log "Installation completed!"
warn "Don't forget to configure $INSTALL_DIR/data.json."

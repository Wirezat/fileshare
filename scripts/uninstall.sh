#!/bin/bash
# Fileshare Uninstaller

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

[ "$EUID" -ne 0 ] && error "Please run as root: sudo bash $0"

log "Stopping Service..."
systemctl stop    fileshare.service 2>/dev/null || warn "Service was not active"
systemctl disable fileshare.service 2>/dev/null || warn "Service was not enabled"
systemctl daemon-reload

log "Removing Service file..."
rm -f /etc/systemd/system/fileshare.service

log "Removing CLI symlink..."
rm -f /usr/local/bin/fileshare

log "Removing /opt/fileshare..."
rm -rf /opt/fileshare

echo ""
log "Uninstallation complete."

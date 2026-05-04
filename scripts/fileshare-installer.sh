#!/bin/bash
# fileshare.sh – Unified installer/updater/uninstaller
#
# Local:  sudo bash scripts/fileshare.sh
# Remote: bash scripts/fileshare.sh --remote user@host [--key ~/.ssh/id_ed25519]

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }
info()  { echo -e "${CYAN}[i]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="/opt/fileshare"
GO=/usr/local/go/bin/go
SERVICE_NAME="fileshare.service"
SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME"

BACKEND_BIN="$REPO_ROOT/fileshare-backend"
CLI_BIN="$REPO_ROOT/fileshare-interface"

REMOTE_HOST=""
SSH_KEY_OPT=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --remote) REMOTE_HOST="$2"; shift 2 ;;
        --key)    SSH_KEY_OPT="-i $2"; shift 2 ;;
        *)        shift ;;
    esac
done

if [ -n "$REMOTE_HOST" ]; then
    info "Syncing repo to $REMOTE_HOST:/tmp/fileshare-deploy ..."
    # shellcheck disable=SC2086
    rsync -az --exclude='.git' $SSH_KEY_OPT \
        "$REPO_ROOT/" "$REMOTE_HOST:/tmp/fileshare-deploy/"

    info "Running fileshare.sh on $REMOTE_HOST ..."
    # shellcheck disable=SC2086
    ssh $SSH_KEY_OPT -t "$REMOTE_HOST" \
        "sudo bash /tmp/fileshare-deploy/scripts/fileshare.sh"
    exit 0
fi

[ "$EUID" -ne 0 ] && error "Please run as root: sudo bash $0\n       Or remotely: bash $0 --remote user@host"

echo ""
echo -e "${BOLD}  Fileshare Management${NC}"
echo    "  ─────────────────────"
echo    "  1) Install"
echo    "  2) Update"
echo    "  3) Uninstall"
echo ""
read -rp "  Choice [1-3]: " CHOICE
echo ""

cleanup_bins() {
    if [ -f "$BACKEND_BIN" ] || [ -f "$CLI_BIN" ]; then
        log "Cleaning up binaries from $REPO_ROOT..."
        rm -f "$BACKEND_BIN" "$CLI_BIN"
    fi
}

build() {
    if [ -d "$REPO_ROOT/cmd/server" ] && [ -d "$REPO_ROOT/cmd/cli" ]; then
        [ ! -f "$GO" ] && error "Source found but Go not installed at $GO."
        log "Building fileshare-backend..."
        cd "$REPO_ROOT"
        GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-backend   ./cmd/server/
        log "Building fileshare-interface..."
        GOOS=linux GOARCH=amd64 "$GO" build -o fileshare-interface ./cmd/cli/
        log "Build complete."
    elif [ -f "$BACKEND_BIN" ] && [ -f "$CLI_BIN" ]; then
        info "No source found – using pre-built binaries."
    else
        error "Neither source code nor pre-built binaries found in $REPO_ROOT."
    fi
}

write_service() {
    cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=Fileshare Service
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/fileshare-backend
WorkingDirectory=$INSTALL_DIR
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
}

do_install() {
    build

    log "Creating $INSTALL_DIR..."
    mkdir -p "$INSTALL_DIR"
    chown -R "${SUDO_USER:-$USER}:${SUDO_USER:-$USER}" "$INSTALL_DIR"

    log "Copying binaries..."
    cp "$BACKEND_BIN" "$INSTALL_DIR/fileshare-backend"
    cp "$CLI_BIN"     "$INSTALL_DIR/fileshare-interface"
    chmod +x "$INSTALL_DIR/fileshare-backend" "$INSTALL_DIR/fileshare-interface"
    restorecon -v "$INSTALL_DIR/fileshare-backend"   2>/dev/null || true
    restorecon -v "$INSTALL_DIR/fileshare-interface" 2>/dev/null || true

    log "Copying assets..."
    cp -r "$REPO_ROOT/assets/web" "$INSTALL_DIR/web"

    if [ ! -f "$INSTALL_DIR/data.json" ]; then
        log "Creating initial data.json from example config..."
        cp "$REPO_ROOT/configs/data.example.json" "$INSTALL_DIR/data.json"
        warn "Please configure $INSTALL_DIR/data.json before use!"
    else
        log "data.json already exists – not overwriting."
    fi

    log "Installing systemd service..."
    write_service
    restorecon -v "$SERVICE_FILE" 2>/dev/null || true
    systemctl daemon-reload
    systemctl enable --now "$SERVICE_NAME"

    log "Creating CLI symlink..."
    local LINK="/usr/local/bin/fileshare"
    [ -L "$LINK" ] && rm "$LINK"
    ln -s "$INSTALL_DIR/fileshare-interface" "$LINK"
    info "$LINK → $INSTALL_DIR/fileshare-interface"

    cleanup_bins
    echo ""
    log "Installation complete!"
    warn "Don't forget to configure $INSTALL_DIR/data.json."
}

do_update() {
    [ ! -d "$INSTALL_DIR" ] && error "Fileshare is not installed. Run this script and choose Install."

    build

    log "Stopping service..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    log "Deploying to $INSTALL_DIR..."
    cp "$BACKEND_BIN" "$INSTALL_DIR/fileshare-backend"
    cp "$CLI_BIN"     "$INSTALL_DIR/fileshare-interface"
    chmod +x "$INSTALL_DIR/fileshare-backend" "$INSTALL_DIR/fileshare-interface"
    rm -rf "$INSTALL_DIR/web"
    cp -r  "$REPO_ROOT/assets/web" "$INSTALL_DIR/web"
    restorecon -v "$INSTALL_DIR/fileshare-backend"   2>/dev/null || true
    restorecon -v "$INSTALL_DIR/fileshare-interface" 2>/dev/null || true

    log "Restarting service..."
    systemctl start "$SERVICE_NAME"

    cleanup_bins
    log "Update complete."
}

do_uninstall() {
    read -rp "  Really uninstall fileshare? This deletes $INSTALL_DIR. [y/N] " CONFIRM
    [[ "$CONFIRM" != [yY] ]] && { info "Aborted."; exit 0; }
    echo ""

    log "Stopping service..."
    systemctl stop    "$SERVICE_NAME" 2>/dev/null || warn "Service was not active."
    systemctl disable "$SERVICE_NAME" 2>/dev/null || warn "Service was not enabled."
    systemctl daemon-reload

    log "Removing service file..."
    rm -f "$SERVICE_FILE"

    log "Removing CLI symlink..."
    rm -f /usr/local/bin/fileshare

    log "Removing $INSTALL_DIR..."
    rm -rf "$INSTALL_DIR"

    log "Uninstallation complete."
}

case "$CHOICE" in
    1) do_install   ;;
    2) do_update    ;;
    3) do_uninstall ;;
    *) error "Invalid choice: '$CHOICE'" ;;
esac
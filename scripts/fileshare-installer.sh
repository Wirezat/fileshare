#!/bin/bash
# fileshare-installer.sh – install, update or uninstall fileshare as a systemd service.
#
# Local:  sudo bash scripts/fileshare-installer.sh
# Remote: bash scripts/fileshare-installer.sh --remote user@host [--key ~/.ssh/id_ed25519]

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }
info()  { echo -e "${CYAN}[i]${NC} $1"; }

SCRIPT_NAME="$(basename "$0")"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_DIR="/opt/fileshare"
SERVICE_NAME="fileshare.service"
SERVICE_FILE="/etc/systemd/system/$SERVICE_NAME"
CLI_LINK="/usr/local/bin/fileshare"
CHUNK_TMP="/tmp/fileshare-chunks"

BACKEND_BIN="$REPO_ROOT/fileshare-backend"
CLI_BIN="$REPO_ROOT/fileshare-interface"
UI_MARKER="$REPO_ROOT/assets/web/ui/index.css"

OWNER="${SUDO_USER:-$USER}"

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
    [ -f "$UI_MARKER" ] || error "assets/web/ui is empty – run 'git submodule update --init' before deploying."
    info "Syncing repo to $REMOTE_HOST:/tmp/fileshare-deploy ..."
    # shellcheck disable=SC2086
    rsync -az --delete --exclude='.git' $SSH_KEY_OPT \
        "$REPO_ROOT/" "$REMOTE_HOST:/tmp/fileshare-deploy/"

    info "Running $SCRIPT_NAME on $REMOTE_HOST ..."
    # shellcheck disable=SC2086
    ssh $SSH_KEY_OPT -t "$REMOTE_HOST" \
        "sudo bash /tmp/fileshare-deploy/scripts/$SCRIPT_NAME"
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

find_go() {
    if [ -x /usr/local/go/bin/go ]; then echo /usr/local/go/bin/go
    elif command -v go >/dev/null 2>&1; then command -v go
    else return 1
    fi
}

cleanup_bins() {
    if [ -f "$BACKEND_BIN" ] || [ -f "$CLI_BIN" ]; then
        log "Cleaning up binaries from $REPO_ROOT..."
        rm -f "$BACKEND_BIN" "$CLI_BIN"
    fi
}

ensure_ui() {
    [ -f "$UI_MARKER" ] && return
    if [ -d "$REPO_ROOT/.git" ] && command -v git >/dev/null 2>&1; then
        log "Fetching web UI submodule..."
        git -C "$REPO_ROOT" submodule update --init
        [ -f "$UI_MARKER" ] && return
    fi
    error "assets/web/ui is empty – the wirezatUI submodule is missing. Run 'git submodule update --init' in the repo."
}

build() {
    if [ -d "$REPO_ROOT/cmd/server" ] && [ -d "$REPO_ROOT/cmd/cli" ]; then
        local GO
        GO="$(find_go)" || error "Source found but Go is not installed (looked in /usr/local/go/bin and PATH)."
        cd "$REPO_ROOT"
        log "Resolving Go modules..."
        "$GO" mod download
        log "Building fileshare-backend..."
        "$GO" build -o fileshare-backend   ./cmd/server/
        log "Building fileshare-interface..."
        "$GO" build -o fileshare-interface ./cmd/cli/
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
    restorecon -v "$SERVICE_FILE" 2>/dev/null || true
    systemctl daemon-reload
}

write_initial_config() {
    cat > "$INSTALL_DIR/data.json" <<'EOF'
{
  "port": 27182,
  "maxPostSize": 107374182400,
  "chunkInactivityTimeout": 3600,
  "admin_username": "",
  "admin_password": "",
  "files": {}
}
EOF
}

deploy_files() {
    log "Deploying binaries and assets to $INSTALL_DIR..."
    cp "$BACKEND_BIN" "$INSTALL_DIR/fileshare-backend"
    cp "$CLI_BIN"     "$INSTALL_DIR/fileshare-interface"
    chmod 755 "$INSTALL_DIR/fileshare-backend" "$INSTALL_DIR/fileshare-interface"
    rm -rf "$INSTALL_DIR/web"
    cp -r  "$REPO_ROOT/assets/web" "$INSTALL_DIR/web"
    restorecon -Rv "$INSTALL_DIR" 2>/dev/null || true
}

link_cli() {
    [ -L "$CLI_LINK" ] && rm "$CLI_LINK"
    ln -s "$INSTALL_DIR/fileshare-interface" "$CLI_LINK"
    info "$CLI_LINK → $INSTALL_DIR/fileshare-interface"
}

config_port() {
    grep -oE '"port"[[:space:]]*:[[:space:]]*[0-9]+' "$INSTALL_DIR/data.json" | grep -oE '[0-9]+$' || echo 27182
}

show_setup_hint() {
    local port code
    port="$(config_port)"
    sleep 2
    if grep -q '"admin_password": *""' "$INSTALL_DIR/data.json"; then
        code="$(journalctl -u "$SERVICE_NAME" --no-pager -o cat --since '-1 min' 2>/dev/null \
                | grep -oE 'setup code: [0-9a-f]+' | tail -1 | awk '{print $3}')"
        echo ""
        warn "No admin password set yet – open http://$(hostname):$port/setup to create the admin account."
        if [ -n "$code" ]; then
            info "Setup code: ${BOLD}$code${NC}"
        else
            info "The setup code is in the log: journalctl -u $SERVICE_NAME | grep 'setup code'"
        fi
    else
        info "Admin panel: http://$(hostname):$port/admin"
    fi
}

do_install() {
    ensure_ui
    build

    log "Creating $INSTALL_DIR..."
    mkdir -p "$INSTALL_DIR"
    deploy_files

    if [ ! -f "$INSTALL_DIR/data.json" ]; then
        log "Writing initial data.json..."
        write_initial_config
    else
        log "data.json already exists – not overwriting."
    fi
    chown -R "$OWNER:$OWNER" "$INSTALL_DIR"
    chmod 600 "$INSTALL_DIR/data.json"

    log "Installing systemd service..."
    write_service
    systemctl enable --now "$SERVICE_NAME"

    log "Creating CLI symlink..."
    link_cli

    cleanup_bins
    echo ""
    log "Installation complete!"
    show_setup_hint
}

do_update() {
    [ ! -d "$INSTALL_DIR" ] && error "Fileshare is not installed. Run this script and choose Install."

    ensure_ui
    build

    log "Stopping service..."
    systemctl stop "$SERVICE_NAME" 2>/dev/null || true

    deploy_files
    chown -R "$OWNER:$OWNER" "$INSTALL_DIR"
    [ -f "$INSTALL_DIR/data.json" ] && chmod 600 "$INSTALL_DIR/data.json"

    log "Refreshing systemd service..."
    write_service
    link_cli

    log "Restarting service..."
    systemctl start "$SERVICE_NAME"

    cleanup_bins
    log "Update complete."
    show_setup_hint
}

do_uninstall() {
    read -rp "  Really uninstall fileshare? This deletes $INSTALL_DIR. [y/N] " CONFIRM
    [[ "$CONFIRM" != [yY] ]] && { info "Aborted."; exit 0; }
    echo ""

    if [ -f "$INSTALL_DIR/data.json" ]; then
        local HOME_DIR BACKUP
        HOME_DIR="$(getent passwd "$OWNER" | cut -d: -f6)"
        BACKUP="${HOME_DIR:-/root}/fileshare-data-$(date +%Y%m%d-%H%M%S).json"
        read -rp "  Keep a copy of data.json at $BACKUP? [Y/n] " KEEP
        if [[ "$KEEP" != [nN] ]]; then
            cp "$INSTALL_DIR/data.json" "$BACKUP"
            chown "$OWNER:$OWNER" "$BACKUP"
            chmod 600 "$BACKUP"
            log "Saved $BACKUP"
        fi
        echo ""
    fi

    log "Stopping service..."
    systemctl stop    "$SERVICE_NAME" 2>/dev/null || warn "Service was not active."
    systemctl disable "$SERVICE_NAME" 2>/dev/null || warn "Service was not enabled."

    log "Removing service file..."
    rm -f "$SERVICE_FILE"
    systemctl daemon-reload

    log "Removing CLI symlink..."
    rm -f "$CLI_LINK"

    log "Removing $INSTALL_DIR and upload temp dir..."
    rm -rf "$INSTALL_DIR" "$CHUNK_TMP"

    log "Uninstallation complete."
}

case "$CHOICE" in
    1) do_install   ;;
    2) do_update    ;;
    3) do_uninstall ;;
    *) error "Invalid choice: '$CHOICE'" ;;
esac

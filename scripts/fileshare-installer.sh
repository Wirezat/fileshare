#!/bin/bash
# fileshare-installer.sh - install, update or uninstall fileshare as a systemd service.
#
# As root: system mode (/opt/fileshare, own service user). As a normal user: user mode (~/.local/share/fileshare).
# Flags: --install|--update|--uninstall --port N --user NAME --data-dir P --log-dir P
#        --proxy --no-autostart --yes

set -e

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'

log()   { echo -e "${GREEN}[+]${NC} $1"; }
warn()  { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }
info()  { echo -e "${CYAN}[i]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
SERVICE_NAME="fileshare.service"
LEGACY_CLI_LINK="/usr/local/bin/fileshare"
LEGACY_CHUNK_TMP="/tmp/fileshare-chunks"

BACKEND_BIN="$REPO_ROOT/fileshare-backend"
UI_MARKER="$REPO_ROOT/assets/web/ui/index.css"

ACTION=""
ASSUME_YES=0
PORT=""; SVC_USER=""; DATA_DIR=""; LOG_DIR=""; PROXY=""; AUTOSTART=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        --install)      ACTION=install; shift ;;
        --update)       ACTION=update; shift ;;
        --uninstall)    ACTION=uninstall; shift ;;
        --port)         [ $# -ge 2 ] || error "--port needs a value"; PORT="$2"; shift 2 ;;
        --user)         [ $# -ge 2 ] || error "--user needs a value"; SVC_USER="$2"; shift 2 ;;
        --data-dir)     [ $# -ge 2 ] || error "--data-dir needs a value"; DATA_DIR="$2"; shift 2 ;;
        --log-dir)      [ $# -ge 2 ] || error "--log-dir needs a value"; LOG_DIR="$2"; shift 2 ;;
        --proxy)        PROXY=yes; shift ;;
        --no-autostart) AUTOSTART=no; shift ;;
        --yes)          ASSUME_YES=1; shift ;;
        *)              error "Unknown option: $1" ;;
    esac
done

detect_mode() {
    if [ "$EUID" -eq 0 ]; then echo system; else echo user; fi
}

MODE="$(detect_mode)"
CURRENT_USER="$(id -un)"

if [ "$MODE" = system ]; then
    INSTALL_DIR="/opt/fileshare"
    UNIT_DIR="/etc/systemd/system"
    SYSTEMCTL=(systemctl)
    JOURNAL=(journalctl -u "$SERVICE_NAME")
else
    INSTALL_DIR="$HOME/.local/share/fileshare"
    UNIT_DIR="$HOME/.config/systemd/user"
    SYSTEMCTL=(systemctl --user)
    JOURNAL=(journalctl --user -u "$SERVICE_NAME")
fi
SERVICE_FILE="$UNIT_DIR/$SERVICE_NAME"

preflight_user_mode() {
    [ "$MODE" = user ] || return 0
    if [ -z "$XDG_RUNTIME_DIR" ] || ! systemctl --user show-environment >/dev/null 2>&1; then
        error "systemctl --user geht in dieser Sitzung nicht (typisch nach su). Melde dich per SSH oder mit \`machinectl shell $CURRENT_USER@\` direkt als dieser User an."
    fi
}

find_go() {
    if [ -x /usr/local/go/bin/go ]; then echo /usr/local/go/bin/go
    elif command -v go >/dev/null 2>&1; then command -v go
    else return 1
    fi
}

cleanup_bins() {
    if [ -f "$BACKEND_BIN" ]; then
        log "Cleaning up binaries from $REPO_ROOT..."
        rm -f "$BACKEND_BIN"
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
    if [ -d "$REPO_ROOT/cmd/server" ]; then
        local GO
        GO="$(find_go)" || error "Source found but Go is not installed (looked in /usr/local/go/bin and PATH)."
        cd "$REPO_ROOT"
        log "Building fileshare-backend..."
        CGO_ENABLED=0 "$GO" build -trimpath -ldflags='-s -w' -o fileshare-backend ./cmd/server/
        log "Build complete."
    elif [ -f "$BACKEND_BIN" ]; then
        info "No source found – using pre-built binary."
    else
        error "Neither source code nor a pre-built binary found in $REPO_ROOT."
    fi
}

# ask NAME DEFAULT PROMPT [bool]: sets variable NAME to DEFAULT or an override; skipped when NAME is
# already set (flag) or --yes was given. With "bool", an override flips yes/no.
ask() {
    local name="$1" default="$2" prompt="$3" kind="$4" ans
    [ -n "${!name}" ] && return 0
    if [ "$ASSUME_YES" = 1 ]; then
        printf -v "$name" '%s' "$default"
        return 0
    fi
    read -rp "  $prompt [$default] - override? y/N " ans
    if [[ "$ans" != [yY] ]]; then
        printf -v "$name" '%s' "$default"
    elif [ "$kind" = bool ]; then
        if [ "$default" = yes ]; then printf -v "$name" no; else printf -v "$name" yes; fi
    else
        read -rp "  $prompt: " ans
        printf -v "$name" '%s' "${ans:-$default}"
    fi
}

load_unit_settings() {
    [ -f "$SERVICE_FILE" ] || return 0
    local v
    v="$(sed -n 's/^Environment=FS_PORT=//p' "$SERVICE_FILE")";     [ -n "$PORT" ]     || PORT="$v"
    v="$(sed -n 's/^Environment=FS_BIND=//p' "$SERVICE_FILE")"
    if [ -z "$PROXY" ]; then
        if [ "$v" = 127.0.0.1 ]; then PROXY=yes; else PROXY=no; fi
    fi
    v="$(sed -n 's/^Environment=FS_DATA_DIR=//p' "$SERVICE_FILE")"; [ -n "$DATA_DIR" ] || DATA_DIR="$v"
    v="$(sed -n 's/^Environment=FS_LOG_DIR=//p' "$SERVICE_FILE")";  [ -n "$LOG_DIR" ]  || LOG_DIR="$v"
    v="$(sed -n 's/^User=//p' "$SERVICE_FILE")";                    [ -n "$SVC_USER" ] || SVC_USER="$v"
}

check_dir_path() {
    [[ "$1" == /* ]] && [[ ! "$1" =~ [[:space:]%\"\'\\] ]] || error "Folder must be an absolute path without whitespace, quotes, backslash or %: '$1'"
}

legacy_root_unit_check() {
    if [ "$MODE" = system ] && [ -f "$SERVICE_FILE" ] && ! grep -q '^User=' "$SERVICE_FILE"; then
        error "This installation still runs as root (v5). Stop it, back up $SERVICE_FILE and move it away, then install again; data.json stays in place."
    fi
}

validate_settings() {
    [[ "$PORT" =~ ^[0-9]+$ ]] || error "Invalid port: '$PORT'"
    PORT=$((10#$PORT))
    [ "$PORT" -le 65535 ] || error "Invalid port: '$PORT'"
    if [ "$PORT" -lt 1024 ]; then
        error "Ports below 1024 are not allowed. Run fileshare on a port of 1024 or higher and put a reverse proxy (Traefik, nginx) in front."
    fi
    if [ "$MODE" = system ]; then
        if [ "$SVC_USER" = 0 ] || { id -u "$SVC_USER" >/dev/null 2>&1 && [ "$(id -u "$SVC_USER")" = 0 ]; }; then
            error "The service user must not be root."
        fi
    fi
    check_dir_path "$DATA_DIR"
    check_dir_path "$LOG_DIR"
}

ask_settings() {
    ask PORT 27182 "Port"
    if [ "$MODE" = system ]; then
        ask SVC_USER fileshare "Service user"
    else
        SVC_USER="$CURRENT_USER"
    fi
    ask DATA_DIR "$INSTALL_DIR" "Data folder"
    ask LOG_DIR "$INSTALL_DIR/logs" "Log folder"
    validate_settings
    ask PROXY no "Behind a reverse proxy (listen on 127.0.0.1 only)" bool
    ask AUTOSTART yes "Autostart at boot" bool
}

ensure_service_user() {
    [ "$MODE" = system ] || return 0
    if id "$SVC_USER" >/dev/null 2>&1; then
        return 0
    fi
    log "Creating system user $SVC_USER..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SVC_USER"
}

private_tmp_works() {
    unshare --user --map-root-user true >/dev/null 2>&1
}

write_unit() {
    local bind=0.0.0.0 env="" hardening
    [ "$PROXY" = yes ] && bind=127.0.0.1
    env+="Environment=FS_PORT=$PORT"$'\n'
    env+="Environment=FS_BIND=$bind"$'\n'
    [ "$DATA_DIR" != "$INSTALL_DIR" ] && env+="Environment=FS_DATA_DIR=$DATA_DIR"$'\n'
    [ "$LOG_DIR" != "$INSTALL_DIR/logs" ] && env+="Environment=FS_LOG_DIR=$LOG_DIR"$'\n'

    if [ "$MODE" = system ]; then
        hardening="User=$SVC_USER
AmbientCapabilities=CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
CapabilityBoundingSet=CAP_DAC_READ_SEARCH CAP_DAC_OVERRIDE
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=full
ProtectKernelTunables=yes
InaccessiblePaths=-/etc/shadow -/etc/gshadow -/root/.ssh"
    else
        hardening="NoNewPrivileges=yes"
        if private_tmp_works; then hardening+=$'\n'"PrivateTmp=yes"; fi
    fi

    mkdir -p "$UNIT_DIR"
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
${env}${hardening}

[Install]
WantedBy=$([ "$MODE" = system ] && echo multi-user.target || echo default.target)
EOF
    [ "$MODE" = system ] && restorecon -v "$SERVICE_FILE" 2>/dev/null || true
    "${SYSTEMCTL[@]}" daemon-reload
}

deploy_files() {
    log "Deploying binary and assets to $INSTALL_DIR..."
    cp "$BACKEND_BIN" "$INSTALL_DIR/fileshare-backend"
    chmod 755 "$INSTALL_DIR/fileshare-backend"
    rm -rf "$INSTALL_DIR/web"
    cp -r  "$REPO_ROOT/assets/web" "$INSTALL_DIR/web"
    [ "$MODE" = system ] && restorecon -Rv "$INSTALL_DIR" 2>/dev/null || true
}

set_ownership() {
    local group
    group="$(id -gn "$SVC_USER")"
    chown -R "$SVC_USER:$group" "$INSTALL_DIR"
    chown "$SVC_USER:$group" "$DATA_DIR"
    [ -d "$LOG_DIR" ] && chown "$SVC_USER:$group" "$LOG_DIR"
    [ -f "$DATA_DIR/data.json" ] && chmod 600 "$DATA_DIR/data.json"
    return 0
}

remove_legacy_cli() {
    if [ -e "$LEGACY_CLI_LINK" ] || [ -L "$LEGACY_CLI_LINK" ] || [ -f "$INSTALL_DIR/fileshare-interface" ]; then
        [ "$MODE" = system ] && log "Removing the discontinued CLI..."
        [ "$MODE" = system ] && rm -f "$LEGACY_CLI_LINK"
        rm -f "$INSTALL_DIR/fileshare-interface"
    fi
    return 0
}

enable_autostart() {
    if [ "$AUTOSTART" = no ]; then
        "${SYSTEMCTL[@]}" start "$SERVICE_NAME"
        return
    fi
    "${SYSTEMCTL[@]}" enable --now "$SERVICE_NAME"
    if [ "$MODE" = user ]; then
        if ! loginctl enable-linger "$CURRENT_USER" 2>/dev/null; then
            warn "fileshare stoppt, sobald du dich ausloggst. Ein Admin kann das mit \`sudo loginctl enable-linger $CURRENT_USER\` ändern."
        fi
    fi
}

show_setup_hint() {
    local port code data_json="$DATA_DIR/data.json"
    port="$PORT"
    sleep 2
    if [ -f "$data_json" ] && grep -q '"admin_password": *""' "$data_json"; then
        code="$("${JOURNAL[@]}" --no-pager -o cat --since '-1 min' 2>/dev/null \
                | grep -oE 'setup code: [0-9a-f]+' | tail -1 | awk '{print $3}')" || true
        echo ""
        warn "No admin password set yet – open http://$(hostname):$port/setup to create the admin account."
        if [ -n "$code" ]; then
            info "Setup code: ${BOLD}$code${NC}"
        else
            info "The setup code is in the log: ${JOURNAL[*]} | grep 'setup code'"
        fi
    else
        info "Admin panel: http://$(hostname):$port/admin"
    fi
}

do_install() {
    legacy_root_unit_check
    ask_settings
    ensure_service_user
    ensure_ui
    build

    log "Creating $INSTALL_DIR..."
    mkdir -p "$INSTALL_DIR" "$DATA_DIR" "$LOG_DIR"
    deploy_files
    set_ownership

    log "Installing systemd service ($MODE mode)..."
    write_unit
    enable_autostart
    set_ownership

    remove_legacy_cli
    cleanup_bins
    echo ""
    log "Installation complete!"
    show_setup_hint
}

find_install() {
    [ -f "$SERVICE_FILE" ] && return 0
    local other=""
    if [ "$MODE" = user ]; then
        [ -f "/etc/systemd/system/$SERVICE_NAME" ] && other="system mode (/etc/systemd/system/$SERVICE_NAME); run this script as root to manage it"
    elif [ -n "$SUDO_USER" ]; then
        local h
        h="$(getent passwd "$SUDO_USER" | cut -d: -f6)"
        [ -n "$h" ] && [ -f "$h/.config/systemd/user/$SERVICE_NAME" ] && other="user mode of $SUDO_USER; run this script as $SUDO_USER to manage it"
    fi
    if [ -n "$other" ]; then
        error "No fileshare installation in $MODE mode. Found one in $other. Nothing was changed."
    fi
    error "Fileshare is not installed in $MODE mode. Run with --install to install it."
}

do_update() {
    find_install
    legacy_root_unit_check
    load_unit_settings
    : "${PORT:=27182}" "${DATA_DIR:=$INSTALL_DIR}" "${LOG_DIR:=$INSTALL_DIR/logs}"
    [ "$MODE" = system ] || SVC_USER="$CURRENT_USER"
    validate_settings

    ensure_ui
    build

    log "Stopping service..."
    "${SYSTEMCTL[@]}" stop "$SERVICE_NAME" 2>/dev/null || true

    deploy_files
    log "Refreshing systemd service..."
    write_unit
    remove_legacy_cli
    set_ownership

    log "Restarting service..."
    "${SYSTEMCTL[@]}" start "$SERVICE_NAME"

    cleanup_bins
    log "Update complete."
    show_setup_hint
}

do_uninstall() {
    local CONFIRM
    find_install
    load_unit_settings
    : "${DATA_DIR:=$INSTALL_DIR}"
    if [ "$ASSUME_YES" = 1 ]; then
        CONFIRM=y
    else
        read -rp "  Really uninstall fileshare? This deletes $INSTALL_DIR. [y/N] " CONFIRM
    fi
    [[ "$CONFIRM" != [yY] ]] && { info "Aborted."; exit 0; }
    echo ""

    if [ -f "$DATA_DIR/data.json" ]; then
        local OWNER HOME_DIR BACKUP KEEP
        OWNER="${SUDO_USER:-$CURRENT_USER}"
        HOME_DIR="$(getent passwd "$OWNER" | cut -d: -f6)"
        BACKUP="${HOME_DIR:-$HOME}/fileshare-data-$(date +%Y%m%d-%H%M%S).json"
        if [ "$ASSUME_YES" = 1 ]; then
            KEEP=y
        else
            read -rp "  Keep a copy of data.json at $BACKUP? [Y/n] " KEEP
        fi
        if [[ "$KEEP" != [nN] ]]; then
            cp "$DATA_DIR/data.json" "$BACKUP"
            chown "$OWNER:$(id -gn "$OWNER")" "$BACKUP"
            chmod 600 "$BACKUP"
            log "Saved $BACKUP"
        fi
        echo ""
    fi

    log "Stopping service..."
    "${SYSTEMCTL[@]}" stop    "$SERVICE_NAME" 2>/dev/null || warn "Service was not active."
    "${SYSTEMCTL[@]}" disable "$SERVICE_NAME" 2>/dev/null || warn "Service was not enabled."

    log "Removing service file..."
    rm -f "$SERVICE_FILE"
    "${SYSTEMCTL[@]}" daemon-reload

    [ "$MODE" = system ] && rm -f "$LEGACY_CLI_LINK"

    log "Removing $INSTALL_DIR..."
    rm -rf "$INSTALL_DIR"
    [ "$MODE" = system ] && rm -rf "$LEGACY_CHUNK_TMP"
    if [ "$DATA_DIR" != "$INSTALL_DIR" ]; then
        info "Data folder $DATA_DIR was kept."
    fi
    if [ "$LOG_DIR" != "$INSTALL_DIR/logs" ] && [ -n "$LOG_DIR" ]; then
        info "Log folder $LOG_DIR was kept."
    fi

    log "Uninstallation complete."
}

preflight_user_mode

if [ -z "$ACTION" ]; then
    echo ""
    echo -e "${BOLD}  Fileshare Management ($MODE mode)${NC}"
    echo    "  ─────────────────────"
    echo    "  1) Install"
    echo    "  2) Update"
    echo    "  3) Uninstall"
    echo ""
    read -rp "  Choice [1-3]: " CHOICE
    echo ""
    case "$CHOICE" in
        1) ACTION=install ;;
        2) ACTION=update ;;
        3) ACTION=uninstall ;;
        *) error "Invalid choice: '$CHOICE'" ;;
    esac
fi

case "$ACTION" in
    install)   do_install   ;;
    update)    do_update    ;;
    uninstall) do_uninstall ;;
esac

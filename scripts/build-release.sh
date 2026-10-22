#!/bin/bash
# build-release.sh – build the static binary and pack fileshare-v<version>-linux.tar.gz.
#
# Usage: bash scripts/build-release.sh <version> [out-dir]

set -euo pipefail

VERSION="${1:?usage: $0 <version> [out-dir]}"
OUT_DIR="$(realpath -m "${2:-.}")"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
NAME="fileshare-v$VERSION-linux"

[ -f "$REPO_ROOT/assets/web/ui/index.css" ] || { echo "assets/web/ui is empty – run 'git submodule update --init'" >&2; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT
mkdir -p "$STAGE/$NAME" "$OUT_DIR"

cd "$REPO_ROOT"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o "$STAGE/$NAME/fileshare-backend" ./cmd/server/
git ls-files --recurse-submodules assets scripts configs README.md LICENSE \
    | grep -v '/\.gitignore$' \
    | tar -cf - -T - | tar -xf - -C "$STAGE/$NAME"

tar -czf "$OUT_DIR/$NAME.tar.gz" -C "$STAGE" "$NAME"
echo "$OUT_DIR/$NAME.tar.gz"

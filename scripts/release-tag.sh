#!/bin/bash
# is-latest exits 0 if v<version> is the highest valid tag on stdin, 1 if not, 2 on broken input.

set -euo pipefail

TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+$'

usage() {
    echo "usage: $0 check <tag> | is-latest <version>" >&2
    exit 2
}

check() {
    [[ "$1" =~ $TAG_RE ]] || { echo "tag '$1' is not v<x.y.z>" >&2; return 1; }
}

is_latest() {
    local own="v$1" tags
    check "$own" || return 2
    tags="$(grep -E "$TAG_RE" || true)"
    [ -n "$tags" ] || { echo "no valid tags on stdin" >&2; return 2; }
    grep -qxF "$own" <<< "$tags" || { echo "$own is not among the tags" >&2; return 2; }
    [ "$(sort -V <<< "$tags" | tail -n 1)" = "$own" ]
}

[ $# -eq 2 ] || usage
case "$1" in
    check) check "$2" ;;
    is-latest) is_latest "$2" ;;
    *) usage ;;
esac

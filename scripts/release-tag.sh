#!/bin/bash
# check prints x.y.z of a v<x.y.z>.<name> tag; is-latest exits 0 if <version> is the highest valid tag on stdin, 1 if not, 2 on broken input.

set -euo pipefail

TAG_RE='^v[0-9]+\.[0-9]+\.[0-9]+\.[A-Za-z]+$'

usage() {
    echo "usage: $0 check <tag> | is-latest <version>" >&2
    exit 2
}

check() {
    [[ "$1" =~ $TAG_RE ]] || { echo "tag '$1' is not v<x.y.z>.<name>" >&2; return 1; }
    local v="${1#v}"
    echo "${v%.*}"
}

is_latest() {
    local own="$1" versions
    [[ "$own" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version '$own' is not x.y.z" >&2; return 2; }
    versions="$(grep -E "$TAG_RE" | sed -E 's/^v//; s/\.[A-Za-z]+$//' || true)"
    [ -n "$versions" ] || { echo "no valid tags on stdin" >&2; return 2; }
    grep -qxF "$own" <<< "$versions" || { echo "$own is not among the tags" >&2; return 2; }
    [ "$(sort -V <<< "$versions" | tail -n 1)" = "$own" ]
}

[ $# -eq 2 ] || usage
case "$1" in
    check) check "$2" ;;
    is-latest) is_latest "$2" ;;
    *) usage ;;
esac

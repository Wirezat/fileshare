#!/bin/sh
# Builds the third-party scripts of the share page from scripts/vendor into <js-dir> (default assets/web/js):
# the CodeMirror bundle in codemirror/ and highlight.min.js, each with its license.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
out="$(mkdir -p "${1:-$root/assets/web/js}" && cd "${1:-$root/assets/web/js}" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cp "$root"/scripts/vendor/package.json "$root"/scripts/vendor/rollup.config.mjs "$root"/scripts/vendor/editor.js "$work"/
if [ -f "$root/scripts/vendor/package-lock.json" ]; then
    cp "$root/scripts/vendor/package-lock.json" "$work"/
    (cd "$work" && npm ci --ignore-scripts --no-audit --no-fund)
else
    (cd "$work" && npm install --ignore-scripts --no-audit --no-fund)
    cp "$work/package-lock.json" "$root/scripts/vendor/"
fi
(cd "$work" && npx rollup -c)

rm -rf "$out/codemirror"
cp -r "$work/dist" "$out/codemirror"
cp "$work/node_modules/codemirror/LICENSE" "$out/codemirror/LICENSE"
cp "$work/node_modules/@highlightjs/cdn-assets/highlight.min.js" "$out/highlight.min.js"
cp "$work/node_modules/@highlightjs/cdn-assets/LICENSE" "$out/highlight.min.js.LICENSE"

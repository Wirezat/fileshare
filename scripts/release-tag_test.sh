#!/bin/bash
set -uo pipefail

SCRIPT="$(dirname "$0")/release-tag.sh"
fail=0

expect() {
    local want="$1" input="$2"; shift 2
    printf '%b' "$input" | bash "$SCRIPT" "$@" > /dev/null 2>&1
    local got=$?
    if [ "$got" -ne "$want" ]; then
        echo "FAIL: $* with input '$input': want $want, got $got"
        fail=1
    fi
}

expect 0 "" check v1.2.3.census
expect 1 "" check v1.2.3
expect 1 "" check v1.2.3.rc1
expect 1 "" check v1.2.3.census-2
expect 1 "" check v.1.2.3.census
expect 1 "" check 1.2.3.census

expect 0 "v1.0.5.a\nv1.1.0.b\n" is-latest 1.1.0
expect 1 "v1.0.5.a\nv1.1.0.b\n" is-latest 1.0.5
expect 0 "v1.2.3.a\nv1.2.3-rc1\n" is-latest 1.2.3
expect 0 "v1.9.0.a\nv1.10.0.b\n" is-latest 1.10.0
expect 0 "v1.0.0.a\n" is-latest 1.0.0
expect 0 "v.4.0.0.literacy\nv2.1.0.dying\nv5.0.0.census\nv0.1.0\nv5.1.0.next\n" is-latest 5.1.0

expect 2 "" is-latest 1.0.0
expect 2 "v1.0.0\nRelease 1.0.0\n" is-latest 1.0.0
expect 2 "v1.0.0.a\nv1.1.0.b\n" is-latest 1.2.0
expect 2 "v1.0.0.a\n" is-latest v1.0.0

[ "$fail" -eq 0 ] && echo "ok"
exit "$fail"

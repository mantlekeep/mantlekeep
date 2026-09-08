#!/bin/sh
# Every module in this repo carries a `replace` to its siblings so a clone builds
# against the tree beside it. Go ignores a dependency's `replace`: consumers resolve
# the `require` line instead. So the graph CI tests and the graph a consumer gets are
# two different graphs, and nothing compares them -- mantlekeep-estate/v0.3.0 shipped
# declaring mantlekeep-control v0.2.0 while every test ran against v0.4.1, and a
# consumer following the documented approval pattern hit "undefined: DecisionFrom".
#
# Two rules, both about the DECLARED graph:
#   1. a sibling `require` must name that sibling's newest released tag
#   2. the module's own tests must pass with the `replace` lines stripped
#
# Usage: scripts/check-release-pins.sh [module ...]   (default: every module)

set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

module_prefix='github.com/mantlekeep/mantlekeep/'
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/mantlekeep-pins.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT INT TERM

if [ "$#" -gt 0 ]; then
    modules="$*"
else
    modules=$(find . -mindepth 2 -maxdepth 2 -name go.mod -exec dirname {} \; | sed 's|^\./||' | sort)
fi

latest_released_tag() {
    git tag -l "$1/v*" | sed "s|^$1/||" | sort -V | tail -1
}

declared_pin() {
    awk -v want="$module_prefix$2" '
        $1 == want && $2 ~ /^v/ { print $2; exit }
    ' "$1/go.mod"
}

failures=0

for module in $modules; do
    [ -f "$module/go.mod" ] || continue

    # Rule 1 -- a sibling require must name that sibling's newest released tag.
    siblings=$(awk -v prefix="$module_prefix" '
        $1 ~ "^" prefix && $2 ~ /^v/ { sub(prefix, "", $1); print $1 }
    ' "$module/go.mod" | sort -u)

    for sibling in $siblings; do
        released=$(latest_released_tag "$sibling")
        [ -n "$released" ] || continue
        declared=$(declared_pin "$module" "$sibling")
        if [ "$declared" != "$released" ]; then
            echo "FAIL $module declares $sibling $declared, but $sibling's newest release is $released"
            echo "     a consumer of $module resolves $declared -- not the tree your tests ran against"
            echo "     fix: (cd $module && go mod edit -require=$module_prefix$sibling@$released && go mod tidy)"
            failures=$((failures + 1))
        fi
    done

    # Rule 2 -- the tests must pass against that declared graph, replace stripped.
    if ! grep -q "^replace $module_prefix" "$module/go.mod"; then
        continue
    fi
    tar -cf - "$module" | tar -xf - -C "$work_dir"
    (
        cd "$work_dir/$module"
        grep -v "^replace $module_prefix" go.mod > go.mod.stripped
        mv go.mod.stripped go.mod
        GOWORK=off GOFLAGS=-mod=mod go mod tidy >/dev/null 2>&1 || true
        if ! GOWORK=off GOFLAGS=-mod=mod go test ./... -count=1 >test.log 2>&1; then
            echo "FAIL $module does not pass its own tests against the graph it ships"
            grep -E '^(FAIL|---|[a-z/]*\.go:[0-9]+)' test.log | head -5 | sed 's/^/     /'
            exit 1
        fi
    ) || failures=$((failures + 1))
done

if [ "$failures" -ne 0 ]; then
    echo
    echo "$failures module(s) would ship a graph nobody tested. Do not tag."
    exit 1
fi

echo "OK every module's declared graph matches the released siblings, and passes its tests"

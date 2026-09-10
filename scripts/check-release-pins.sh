#!/bin/sh
# Every module here carries a `replace` to its siblings so a clone builds against the tree beside
# it. Go IGNORES a `replace` in a dependency's go.mod: a consumer resolves the `require` line
# instead. So the graph CI tests and the graph a consumer gets are two different graphs, and
# nothing compares them -- mantlekeep-estate/v0.3.0 shipped declaring mantlekeep-control v0.2.0
# while every test ran against v0.4.1, and a consumer following the documented approval pattern
# hit "undefined: DecisionFrom" on a fully green release.
#
# Two rules, and this script is honest about what each one is worth:
#
#   Rule 1  a sibling `require` names that sibling's newest released tag.
#           This is the rule that catches the defect above. It is the load-bearing one.
#
#   Rule 2  the module still builds and tests with its `replace` lines stripped.
#           WEAKER THAN IT LOOKS: it runs the module's OWN tests, and a module's own tests may
#           never touch the API a consumer uses. Estate passed rule 2 at the broken pin -- 6/6
#           packages ok -- because nothing in estate calls DecisionFrom; the CONSUMER did. Rule 2
#           catches a declared graph that will not compile. It does not catch one that compiles
#           and is still wrong.
#
# A gate must be LOUD when it cannot check. Every "cannot verify" path below exits non-zero rather
# than skipping, because a skip prints success it did not earn.
#
# Usage: scripts/check-release-pins.sh [module ...]   (default: every module in the repo)

set -eu

repo_root=$(cd "$(dirname "$0")/.." && pwd)
cd "$repo_root"

module_prefix='github.com/mantlekeep/mantlekeep/'
work_dir=$(mktemp -d "${TMPDIR:-/tmp}/mantlekeep-pins.XXXXXX")
trap 'rm -rf "$work_dir"' EXIT INT TERM

if [ "$#" -gt 0 ]; then
    modules="$*"
else
    # Every module, at any depth. The old form stopped at depth 2 and silently missed nested
    # modules such as scripts/dupstrings -- invisible to the gate, with no warning.
    modules=$(find . -name go.mod -not -path './.git/*' -not -path '*/testdata/*' \
        -exec dirname {} \; | sed 's|^\./||' | grep -v '^\.$' | sort)
fi

# latest_released_tag prints a sibling's newest RELEASE tag, or nothing when it has none.
#
# Prereleases are excluded. `sort -V` ranks v0.5.0-rc.1 ABOVE v0.4.1, so a single rc tag would
# make the gate demand that every sibling pin a prerelease. A release gate compares against
# releases.
latest_released_tag() {
    git tag -l "$1/v*" \
        | sed "s|^$1/||" \
        | grep -vE '-' \
        | sort -V \
        | tail -1
}

# The three readers below parse `go mod edit -json` as JSON.
#
# An earlier version scanned that output with awk and reported nonsense -- a module requiring
# ITSELF at a version belonging to an unrelated dependency. Text-scanning structured data is the
# same mistake as regex over source: it matches something, and what it matches is not what you
# asked for. The toolchain emits JSON; read it as JSON.
mod_json() {
    (cd "$1" && GOWORK=off go mod edit -json)
}

# declared_pin prints the version a module requires for one sibling, or nothing.
declared_pin() {
    mod_json "$1" | python3 -c '
import json, sys
want = sys.argv[1] + sys.argv[2]
document = json.load(sys.stdin)
for entry in document.get("Require") or []:
    if entry["Path"] == want:
        print(entry["Version"])
        break
' "$module_prefix" "$2"
}

# sibling_requires prints every SIBLING module this module requires -- never itself.
sibling_requires() {
    mod_json "$1" | python3 -c '
import json, sys
prefix = sys.argv[1]
document = json.load(sys.stdin)
own = (document.get("Module") or {}).get("Path", "")
for entry in document.get("Require") or []:
    path = entry["Path"]
    if path.startswith(prefix) and path != own:
        print(path[len(prefix):])
' "$module_prefix" | sort -u
}

# sibling_replaces prints every sibling module this module replaces, in either syntax.
sibling_replaces() {
    mod_json "$1" | python3 -c '
import json, sys
prefix = sys.argv[1]
document = json.load(sys.stdin)
own = (document.get("Module") or {}).get("Path", "")
for entry in document.get("Replace") or []:
    path = entry["Old"]["Path"]
    if path.startswith(prefix) and path != own:
        print(path)
' "$module_prefix"
}

failures=0
checked=0

for module in $modules; do
    [ -f "$module/go.mod" ] || continue
    checked=$((checked + 1))

    # ── Rule 1 ──────────────────────────────────────────────────────────────
    for sibling in $(sibling_requires "$module"); do
        released=$(latest_released_tag "$sibling")
        declared=$(declared_pin "$module" "$sibling")

        if [ -z "$released" ]; then
            # The old script skipped here, which is how the gate passed vacuously for a sibling
            # that has never been tagged -- mantlekeep-policy-postgres, the exact module whose
            # FIRST release this gate exists to protect.
            echo "FAIL $module requires $sibling $declared, but $sibling has no released tag"
            echo "     the declared version names nothing that exists; it cannot be verified"
            echo "     fix: tag $sibling first, or drop the require if it is not a real dependency"
            failures=$((failures + 1))
            continue
        fi

        if [ "$declared" != "$released" ]; then
            echo "FAIL $module declares $sibling $declared, but $sibling's newest release is $released"
            echo "     a consumer of $module resolves $declared -- not the tree your tests ran against"
            echo "     fix: (cd $module && go mod edit -require=$module_prefix$sibling@$released && go mod tidy)"
            failures=$((failures + 1))
        fi
    done

    # ── Rule 2 ──────────────────────────────────────────────────────────────
    replaced=$(sibling_replaces "$module")
    [ -n "$replaced" ] || continue

    if ! tar -cf - "$module" | (cd "$work_dir" && tar -xf -); then
        echo "FAIL $module could not be copied for the stripped-replace build"
        failures=$((failures + 1))
        continue
    fi
    (
        cd "$work_dir/$module"
        # Drop each replace through the toolchain. `grep -v "^replace ..."` handled only the
        # single-line form: a `replace ( ... )` block did not match, nothing was stripped, and
        # rule 2 quietly tested the REPLACED graph and passed.
        for path in $replaced; do
            GOWORK=off go mod edit -dropreplace="$path"
        done
        # No `|| true` here. The old form swallowed a resolution failure and then reported a pass.
        if ! GOWORK=off GOFLAGS=-mod=mod go mod tidy >tidy.log 2>&1; then
            echo "FAIL $module cannot even resolve the graph it declares"
            tail -3 tidy.log | sed 's/^/     /'
            exit 1
        fi
        if ! GOWORK=off GOFLAGS=-mod=mod go test ./... -count=1 >test.log 2>&1; then
            echo "FAIL $module does not build or pass its own tests against the graph it ships"
            grep -E '^(FAIL|---|[a-z/]*\.go:[0-9]+)' test.log | head -5 | sed 's/^/     /'
            exit 1
        fi
    ) || failures=$((failures + 1))
done

if [ "$checked" -eq 0 ]; then
    echo "FAIL no modules were checked -- wrong directory, or the argument named nothing"
    exit 2
fi

if [ "$failures" -ne 0 ]; then
    echo
    echo "$failures check(s) failed across $checked module(s). Do not tag."
    exit 1
fi

echo "OK $checked module(s): every sibling pin names a released tag, and each builds with its replaces stripped"

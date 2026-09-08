#!/bin/sh
# The SonarQube rules that have actually blocked a release here, checked LOCALLY.
#
# We were learning these one at a time from a dashboard we cannot see, AFTER tagging — and each
# round trip costs a release. Every rule below has blocked one:
#
#   S1192  a string literal repeated 3+ times in one file
#   S1854  an `if` binding used once in its own condition
#   S3776  cognitive complexity over 15
#   S5443  a file created in the shared, world-writable temp directory
#
# It is an APPROXIMATION and says so. Sonar counts complexity slightly differently, so a function
# near the limit here may be over it there. Passing this does not prove Sonar passes — it proves
# the four things that have bitten are absent.
#
#   sh scripts/check-sonar-rules.sh                    # every module
#   sh scripts/check-sonar-rules.sh mantlekeep-estate  # one
set -eu

MODULES=${*:-$(find . -maxdepth 1 -type d -name 'mantlekeep-*' | sed 's|^\./||' | sort)}
GOCOGNIT=${GOCOGNIT:-$(command -v gocognit || echo "$HOME/go/bin/gocognit")}
fail=0

# S1192 and S1854 come from a Go PARSER, not a regular expression. A regex over the raw text
# matches the gap between two literals on one line — `Name: "a", Kind: "b"` yields `", Kind: "` —
# and a checker that reports things which are not literals is one people switch off.
# Run from inside its own module: the repository root is not a Go module, so `go run ./scripts/…`
# from here cannot resolve. Absolute paths so the tool sees the same files from a different cwd.
ROOT=$(pwd)
ABS=""
for mod in $MODULES; do ABS="$ABS $ROOT/$mod"; done
if ! (cd "$ROOT/scripts/dupstrings" && GOWORK=off go run . $ABS); then
  fail=1
fi

for mod in $MODULES; do
  [ -d "$mod" ] || continue

  if [ -x "$GOCOGNIT" ]; then
    over=$("$GOCOGNIT" -over 15 "$mod" 2>/dev/null || true)
    if [ -n "$over" ]; then
      fail=1
      echo "$over" | while read -r line; do
        printf '   S3776  %s\n          over 15 — split it; Sonar may count this higher still\n' "$line"
      done
    fi
  else
    echo "   (gocognit missing: go install github.com/uudashr/gocognit/cmd/gocognit@latest)"
  fi

  # S5443. Tests are exempt: a test's files are its own and die with it.
  tmp=$(grep -rn 'os\.MkdirTemp("",\|os\.CreateTemp("",\|os\.TempDir()' "$mod" --include='*.go' 2>/dev/null \
        | grep -v '_test\.go' | grep -v '^\s*//' || true)
  if [ -n "$tmp" ]; then
    fail=1
    echo "$tmp" | while read -r line; do
      printf '   S5443  %s\n          writes into the shared world-writable temp dir — use the data dir\n' "$line"
    done
  fi
done

echo
if [ "$fail" -ne 0 ]; then
  echo "Each finding above has blocked a release on a stricter SonarQube than ours."
  exit 1
fi
echo "✓ none of the four rules that have blocked a release are present"

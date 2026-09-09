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
# The first three come from an IN-TREE tool that parses Go. It used to shell out to an installed
# binary, which Sonar itself flagged: `go install …@version` resolves its own dependencies with no
# lock file, so the gate could change behaviour on a morning nobody touched this repository.
#
# It is an APPROXIMATION and says so. Sonar counts complexity slightly differently, so a function
# near the limit here may be over it there. Passing this does not prove Sonar passes — it proves
# the five things that have bitten are absent.
#
#   sh scripts/check-sonar-rules.sh                    # every module
#   sh scripts/check-sonar-rules.sh mantlekeep-estate  # one
set -eu

# The WHOLE repository by default, because that is what a downstream SonarQube scans — not the
# four Go modules. Java, Python and the scripts are analysed there too, and a finding in the Java
# SDK blocks the same release a finding in the core would.
MODULES=${*:-.}
fail=0

# S1192 and S1854 come from a Go PARSER, not a regular expression. A regex over the raw text
# matches the gap between two literals on one line — `Name: "a", Kind: "b"` yields `", Kind: "` —
# and a checker that reports things which are not literals is one people switch off.
# Run from inside its own module: the repository root is not a Go module, so `go run ./scripts/…`
# from here cannot resolve. Absolute paths so the tool sees the same files from a different cwd.
ROOT=$(pwd)
ABS=""
for mod in $MODULES; do
  [ -d "$mod" ] || continue
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
echo "✓ none of the five rules that have blocked a release are present"

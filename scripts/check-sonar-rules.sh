#!/bin/sh
# The SonarQube rules that have actually blocked a release here, checked LOCALLY.
#
# We were learning these one at a time from a dashboard we cannot see, AFTER tagging — and each
# round trip costs a release. Every rule below has blocked one:
#
#   S1192  a string literal repeated 3+ times in one file
#   S1854  an `if` binding used once in its own condition
#   S3776  cognitive complexity over 15
#   S1186  a method body that is empty and says nothing about why
#   S5443  a file created in the shared, world-writable temp directory
#
# The first four come from an IN-TREE tool that parses Go. It used to shell out to an installed
# binary, which Sonar itself flagged: `go install …@version` resolves its own dependencies with no
# lock file, so the gate could change behaviour on a morning nobody touched this repository.
#
# S1192 here is NARROWER than Sonar's: it counts only literals containing a space or a `%` —
# messages and format strings. Sonar counts any repeated literal. The filter keeps identifiers and
# struct tags out of the report, at the cost of missing repeats Sonar would flag. Stated here
# because a gate that implies more coverage than it has is how a clean run stops meaning anything.
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
  if [ ! -d "$mod" ]; then
    # Loud, not skipped. A named module that does not exist is a typo in the caller or a
    # module that moved, and silently checking nothing is how a gate reports success it did
    # not earn.
    echo "check-sonar-rules: no such directory: $mod" >&2
    exit 2
  fi
  case "$mod" in
    /*) ABS="$ABS $mod" ;;
     *) ABS="$ABS $ROOT/${mod#./}" ;;
  esac

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

# The parser-based rules. This invocation is the whole point of the script and it went missing
# once already: the loop above kept running, the S5443 grep kept passing, and the script kept
# printing that five rules were absent while checking one. Four of the five findings it claims
# to cover were never looked at. A gate that cannot run must exit non-zero, never green.
if [ ! -d "$ROOT/scripts/dupstrings" ]; then
  echo "check-sonar-rules: scripts/dupstrings is missing — cannot check S1192/S1854/S3776/S1186" >&2
  exit 2
fi
# `set -e` would abort on the tool's non-zero exit before the finding could be attributed, so the
# status is captured deliberately. Anything other than 0 (findings) or 1 (clean) is a broken
# tool, and that is a different failure from a dirty tree.
parser_status=0
( cd "$ROOT/scripts/dupstrings" && GOWORK=off go run . $ABS ) || parser_status=$?
case "$parser_status" in
  0) : ;;
  1) fail=1 ;;
  *) echo "check-sonar-rules: the in-tree checker failed to run (exit $parser_status)" >&2
     exit 2 ;;
esac

echo
if [ "$fail" -ne 0 ]; then
  echo "Each finding above has blocked a release on a stricter SonarQube than ours."
  exit 1
fi
echo "✓ none of the five rules that have blocked a release are present"

#!/usr/bin/env bash
# Locks the near-zero-dependency guarantee. MantleKeep's core is a sovereign engine that
# parses JSON without Jackson and stores state in embedded bbolt; its whole security story
# is that a scanner finds almost nothing to scan. A future change that quietly pulls in a
# heavy dependency would erode that silently — this guard fails the build when it does.
#
# It counts DIRECT, non-indirect module requires (transitive deps are pinned by go.sum and
# not the point). Bump BASELINE deliberately, in a commit that says why, when a new direct
# dependency is genuinely justified.
set -euo pipefail

MODULE_DIR="${1:-mantlekeep-control}"
BASELINE="${MANTLEKEEP_DEP_BASELINE:-1}"

cd "$MODULE_DIR"

count="$(go mod edit -json | python3 -c '
import json, sys
doc = json.load(sys.stdin)
requires = doc.get("Require") or []
direct = [r["Path"] for r in requires if not r.get("Indirect")]
print(len(direct))
for path in direct:
    sys.stderr.write("  direct dep: %s\n" % path)
')"

echo "direct non-stdlib dependencies in $MODULE_DIR: $count (baseline $BASELINE)"

if [ "$count" -gt "$BASELINE" ]; then
  echo "FAIL: direct dependency count $count exceeds the baseline $BASELINE." >&2
  echo "      A new direct dependency erodes the near-zero-dependency guarantee." >&2
  echo "      If it is justified, raise MANTLEKEEP_DEP_BASELINE in a commit that explains why." >&2
  exit 1
fi

echo "OK: near-zero-dependency guarantee intact."

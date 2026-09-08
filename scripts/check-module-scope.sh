#!/bin/sh
# A change belongs to ONE module, unless it says why not.
#
# Each mantlekeep-<module>/ is released and versioned on its own. A commit that edits two of them
# cannot be described by either module's release notes, cannot be reverted without touching a
# module nobody was reviewing, and asks a reviewer to approve a change to something they did not
# open the PR for.
#
# That last one is the point. A reviewer reading a change to the estate should not have to notice,
# three files down, that the core moved too. Whether that is fine is a decision — this makes it a
# DECLARED one rather than something discovered later.
#
#   sh scripts/check-module-scope.sh                 # main..HEAD
#   sh scripts/check-module-scope.sh origin/main..HEAD
#
# To cross modules deliberately, say why in the commit body:
#
#   Cross-module: the port moved, so every adapter implementing it moves with it
#
# Files outside a module directory — the root, .github, scripts, sdks, docs — are SHARED and count
# toward no module. A commit touching only those is repo-wide by definition and never flagged.
set -eu

RANGE=${1:-main..HEAD}
fail=0

for commit in $(git rev-list --no-merges "$RANGE"); do
  modules=$(git show --name-only --format='' "$commit" \
            | grep -oE '^mantlekeep-[a-z0-9-]+' | sort -u)
  count=$(printf '%s' "$modules" | grep -c . || true)
  [ "$count" -le 1 ] && continue

  if git show -s --format='%B' "$commit" | grep -qi '^Cross-module:'; then
    printf '  declared  %s  %s\n' "$(git rev-parse --short "$commit")" \
      "$(git show -s --format='%s' "$commit" | cut -c1-56)"
    printf '            %s\n' "$(git show -s --format='%B' "$commit" | grep -i '^Cross-module:' | head -1)"
    continue
  fi

  fail=1
  printf '  REJECT    %s  %s\n' "$(git rev-parse --short "$commit")" \
    "$(git show -s --format='%s' "$commit" | cut -c1-56)"
  printf '            touches %s modules: %s\n' "$count" "$(printf '%s' "$modules" | tr '\n' ' ')"
  printf '            split it, or add a Cross-module: line saying why they move together\n'
done

if [ "$fail" -ne 0 ]; then
  echo
  echo "A module is released on its own, so a change spanning two of them belongs to neither"
  echo "module's release notes — and a reviewer approving one has approved the other by accident."
  exit 1
fi
echo "  ✓ every commit in $RANGE stays inside one module, or says why it does not"

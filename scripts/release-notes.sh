#!/bin/sh
# Build the GitHub Release notes for ONE module version, from that module's own CHANGELOG.
#
# The notes are GENERATED, never written a second time. A release page, a CHANGELOG entry and a PR
# description saying the same thing in three places is three things to keep in step, and the one
# nobody updates is the one a consumer reads.
#
#   sh scripts/release-notes.sh mantlekeep-control v0.4.0            # print
#   sh scripts/release-notes.sh mantlekeep-control v0.4.0 --publish  # create the GitHub Release
set -eu

MODULE=${1:?usage: release-notes.sh <module> <vX.Y.Z> [--publish]}
VERSION=${2:?usage: release-notes.sh <module> <vX.Y.Z> [--publish]}
PUBLISH=${3:-}
TAG="$MODULE/$VERSION"
CHANGELOG="$MODULE/CHANGELOG.md"

[ -f "$CHANGELOG" ] || { echo "no $CHANGELOG — a module releases with its own notes" >&2; exit 1; }

# The section for this version, up to the next heading. Missing is a FAILURE, not an empty note:
# a release page that says nothing is worse than no release page, because it looks answered.
BODY=$(awk -v v="## [$VERSION]" '
  index($0, v) == 1 { found = 1; next }
  found && /^## \[/ { exit }
  found { print }
' "$CHANGELOG")

case "$(printf '%s' "$BODY" | tr -d '[:space:]')" in
  "") echo "no '## [$VERSION]' section in $CHANGELOG — write the note before tagging" >&2; exit 1 ;;
esac

# The module's purpose, lifted from the top of its CHANGELOG so a reader who lands on the release
# page cold knows what this thing IS before reading what changed in it.
PURPOSE=$(awk '/<!-- purpose -->/{flag=1;next} /<!-- \/purpose -->/{exit} flag' "$CHANGELOG")
case "$(printf '%s' "$PURPOSE" | tr -d '[:space:]')" in
  "") echo "no <!-- purpose --> block in $CHANGELOG — a release page a reader lands on cold must say what the module IS" >&2; exit 1 ;;
esac

cat <<EOF
$PURPOSE

---

## What changed in $VERSION
$BODY
---

### Using it

\`\`\`
go get github.com/mantlekeep/mantlekeep/$MODULE@$VERSION
\`\`\`

Full history: [\`$MODULE/CHANGELOG.md\`](https://github.com/mantlekeep/mantlekeep/blob/main/$CHANGELOG)
EOF

if [ "$PUBLISH" = "--publish" ]; then
  sh "$0" "$MODULE" "$VERSION" > /tmp/relnotes.$$ 
  gh release create "$TAG" --title "$MODULE $VERSION" --notes-file /tmp/relnotes.$$ --verify-tag
  rm -f /tmp/relnotes.$$
fi

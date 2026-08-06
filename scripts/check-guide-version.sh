#!/usr/bin/env bash
# The guide's masthead names the version it documents. A hand-maintained string there goes
# stale at the next release — it shipped v0.3.9 on the v0.4.0 guide, on the first line a
# reader sees at the public URL. This makes that a build failure instead of a reader's problem.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
[ -n "$tag" ] || { echo "check-guide-version: no tags yet, nothing to compare"; exit 0; }

# EVERY version string in the file, not just the masthead. The masthead was correct while a
# captured `hydra --version` transcript inside a panel was a release behind — visible on the
# published page, invisible to a check that only looked at one element.
mapfile -t found < <(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' docs/guide.html | sort -u)
if [ ${#found[@]} -eq 0 ]; then
  echo "check-guide-version: docs/guide.html names no version at all" >&2
  exit 1
fi
bad=0
for v in "${found[@]}"; do
  [ "$v" = "$tag" ] || { echo "check-guide-version: docs/guide.html names $v, latest tag is $tag" >&2; bad=1; }
done
if [ "$bad" -ne 0 ]; then
  echo "  every version string in the guide must match the released tag" >&2
  echo "  grep -n 'v[0-9]' docs/guide.html" >&2
  exit 1
fi
echo "check-guide-version: docs/guide.html matches $tag"

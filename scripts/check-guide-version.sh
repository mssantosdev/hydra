#!/usr/bin/env bash
# The guide's masthead names the version it documents. A hand-maintained string there goes
# stale at the next release — it shipped v0.3.9 on the v0.4.0 guide, on the first line a
# reader sees at the public URL. This makes that a build failure instead of a reader's problem.
set -euo pipefail
cd "$(dirname "$0")/.."

tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
[ -n "$tag" ] || { echo "check-guide-version: no tags yet, nothing to compare"; exit 0; }

shown=$(grep -oE '<div class="ver">v[0-9]+\.[0-9]+\.[0-9]+' docs/guide.html | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' || true)
if [ -z "$shown" ]; then
  echo "check-guide-version: docs/guide.html has no version in its .ver masthead" >&2
  exit 1
fi
if [ "$shown" != "$tag" ]; then
  echo "check-guide-version: docs/guide.html says $shown, latest tag is $tag" >&2
  echo "  fix: update the .ver line in docs/guide.html" >&2
  exit 1
fi
echo "check-guide-version: docs/guide.html matches $tag"

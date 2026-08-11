#!/usr/bin/env bash
# Facts the docs assert about the build, checked against the build.
#
# This exists because the same failure happened five times in one session: a fact was fixed at the
# site that prompted the fix, and left stale at its siblings. The guide's masthead said v0.4.0 while
# a captured `hydra --version` two hundred lines down said v0.3.9. The README's Themes section was
# updated to `terminal` while a Features bullet kept advertising `hydra`. Each was invisible to a
# check that looked at one place. So every check here asserts over EVERY occurrence, and compares
# against the binary or the git tag rather than against a hand-maintained copy.
set -euo pipefail
cd "$(dirname "$0")/.."

fail=0
note() { echo "check-docs-claims: $*" >&2; fail=1; }

# Every check below needs the binary or a tool. Missing ones used to make a check SKIP silently
# while the success line at the bottom still claimed all six agreed with the build — so hiding
# PyYAML, or deleting ./hydra, produced a green gate that verified nothing. A prerequisite is now
# a failure: this script may report success only for work it actually did.
require_cmd() { command -v "$1" >/dev/null 2>&1 || note "$1 is required and not installed"; }
require_cmd jq
[ -x ./hydra ] || note "./hydra is not built; run make build (gate depends on it)"
[ "$fail" -eq 0 ] || { echo "check-docs-claims: prerequisites missing, nothing was verified" >&2; exit 1; }

# The verdict is a trap rather than a line at the bottom. As a bare statement it could be — and was
# — appended above by mistake, leaving a check that sets fail=1 after the last thing that reads it:
# the drift printed and the script still exited 0. On EXIT a check's position on the page cannot
# change whether its result counts, and a death under errexit is reported instead of passing.
trap 'status=$?
  if [ "$fail" -ne 0 ]; then
    echo "check-docs-claims: docs contradict the build" >&2
    exit 1
  fi
  if [ "$status" -ne 0 ]; then
    echo "check-docs-claims: aborted before finishing, so nothing is verified" >&2
    exit "$status"
  fi
  echo "check-docs-claims: version, theme, card, command count, hook env and the guide URL agree with the build"' EXIT

# --- version: every version string in the guide, not just the masthead ---
tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
if [ -n "$tag" ]; then
  mapfile -t found < <(grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' docs/guide.html | sort -u)
  if [ ${#found[@]} -eq 0 ]; then
    note "docs/guide.html names no version at all"
  else
    for v in "${found[@]}"; do
      [ "$v" = "$tag" ] || note "docs/guide.html names $v, latest tag is $tag (grep -n 'v[0-9]' docs/guide.html)"
    done
  fi
fi

# --- default theme: no document may name a borrowed theme as the default ---
# The default itself is pinned in Go (TestDefaultThemeIsTheTerminalPalette), where it is a value
# rather than a sentence. What can only be checked here is the PROSE, and only negatively: a
# document must not advertise one of the named themes as what a fresh install resolves to. Matching
# on one English phrasing guarded nothing — the phrase it looked for had been deleted by the same
# commit that added the check.
for f in README.md docs/README.md; do
  [ -f "$f" ] || continue
  themes='tokyo ?night|catppuccin|dracula|nord|one ?dark|hydra'
  bad=$(grep -niE "defaults? to (the )?\`?($themes)\`?|\`?($themes)\`? theme by default|\`?($themes)\`? is the default" "$f" || true)
  [ -z "$bad" ] || note "$f names a borrowed theme as the default; a fresh install inherits the terminal palette: $bad"
done


# --- the card must name the shelf on its own ---
# A link-preview card is read with no page around it. Ours once described the tool as a
# "machine-readable output contract", which told a cold reader nothing about worktrees or repos.
for f in 'name="description"' 'property="og:description"' 'property="og:title"'; do
  grep -q "$f" docs/guide.html || note "docs/guide.html is missing $f"
done
for f in '<title>' 'og:title" content="'; do
  # `|| true` on every capture: an unmatched grep exits 1, and under `set -e` that kills the
  # script before the empty-value check below can report anything — the diagnostic became
  # unreachable exactly in the case it exists for.
  line=$(grep -oE "${f}[^<\"]*" docs/guide.html | head -1 || true)
  printf '%s' "$line" | grep -qi 'worktree' \
    || note "the card title does not name worktrees, so a cold reader cannot tell what this is: $line"
done

# --- the command count the guide advertises ---
# §11 said "40 commands" for several releases after the surface reached 42. Ask the binary.
if true; then
  n=$(./hydra commands --output json 2>/dev/null | jq '.data.commands|length' 2>/dev/null)
  claimed=$(grep -oE '>[0-9]+ commands<' docs/guide.html | grep -oE '[0-9]+' | head -1 || true)
  if [ -n "$claimed" ] && [ -n "$n" ] && [ "$n" -gt 0 ] && [ "$claimed" != "$n" ]; then
    note "the guide advertises $claimed commands; the surface publishes $n"
  fi
fi

# --- the hook environment the guide and the field reference advertise ---
# The published page listed eight variables for a commit after two more were added, so a reader
# writing a hook was told less than the tool gives. `hooks ls --output json` publishes the list
# precisely so this can be checked instead of remembered.
if true; then
  d=$(mktemp -d)
  # `hooks ls` needs a workspace, and this runs from the repo root — so it is invoked inside a
  # throwaway one. The `|| true` matters under `set -euo pipefail`: without it a non-zero exit
  # from hydra propagates through the pipe into the assignment and kills the whole check silently.
  ( cd "$d" && HYDRA_CONFIG_DIR="$d/cfg" "$OLDPWD/hydra" init --project-name docs-gate >/dev/null 2>&1 ) || true
  keys=$( ( cd "$d" && HYDRA_CONFIG_DIR="$d/cfg" "$OLDPWD/hydra" hooks ls --output json 2>/dev/null ) | jq -r '.data.env[]?' 2>/dev/null || true )
  rm -rf "$d"
  if [ -n "$keys" ]; then
    for doc in docs/guide.html docs/configuration.md; do
      for key in $keys; do
        grep -qE "\\b${key}\\b" "$doc" || note "$doc never mentions $key, which every hook receives"
      done
    done
  fi
fi

# --- the guide URL the binary prints must be the guide that exists ---
# `hydra --help` names the guide, and the agent skill carries it, so a caller with only the binary
# can find it. A URL advertised and wrong is worse than none, so what --help prints is checked
# against README's link, the guide's own canonical URL, and the skill.
if true; then
  printed=$(./hydra --help 2>&1 | grep -oE 'https://[a-z0-9./-]*github\.io[a-z0-9./-]*' | head -1 || true)
  if [ -z "$printed" ]; then
    note "hydra --help names no guide URL, so a caller with only the binary cannot find it"
  else
    grep -qF -- "$printed" README.md || note "README does not link the guide URL --help prints ($printed)"
    grep -qF -- "$printed" docs/guide.html || note "the guide does not name the URL --help prints ($printed)"
    grep -qF -- "$printed" skills/hydra/SKILL.md || note "the agent skill does not carry the guide URL ($printed)"
  fi
fi


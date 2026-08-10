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

# --- default theme: whatever a fresh install actually resolves to ---
# v0.4.0 changed this from `hydra` to `terminal`. Ask the binary instead of trusting the prose.
if [ -x ./hydra ]; then
  d=$(mktemp -d)
  actual=$(HYDRA_CONFIG_DIR="$d" ./hydra config show --output json 2>/dev/null \
    | sed -n 's/.*"theme"[[:space:]]*:[[:space:]]*"\([a-z-]*\)".*/\1/p' | head -1)
  rm -rf "$d"
  if [ -n "$actual" ]; then
    while IFS= read -r line; do
      claimed=$(printf '%s' "$line" | sed -n 's/.*`\([a-z-]*\)` theme by default.*/\1/p')
      [ -z "$claimed" ] && continue
      [ "$claimed" = "$actual" ] || note "README advertises \`$claimed\` as the default theme; a fresh install resolves to \`$actual\`"
    done < <(grep -n 'theme by default' README.md docs/README.md || true)
    for f in README.md docs/README.md; do
      bad=$(grep -niE '(tokyo ?night|catppuccin|dracula|nord|one ?dark)[^.]{0,24}(theme )?(by default|with styled)' "$f" || true)
      [ -z "$bad" ] || note "$f advertises a borrowed theme as the default; a fresh install resolves to \`$actual\`: $bad"
    done
  fi
fi

# --- the card must name the shelf on its own ---
# A link-preview card is read with no page around it. Ours once described the tool as a
# "machine-readable output contract", which told a cold reader nothing about worktrees or repos.
for f in 'name="description"' 'property="og:description"' 'property="og:title"'; do
  grep -q "$f" docs/guide.html || note "docs/guide.html is missing $f"
done
for f in '<title>' 'og:title" content="'; do
  line=$(grep -oE "${f}[^<\"]*" docs/guide.html | head -1)
  printf '%s' "$line" | grep -qi 'worktree' \
    || note "the card title does not name worktrees, so a cold reader cannot tell what this is: $line"
done

# --- the command count the guide advertises ---
# §11 said "40 commands" for several releases after the surface reached 42. Ask the binary.
if [ -x ./hydra ] && command -v jq >/dev/null 2>&1; then
  n=$(./hydra commands --output json 2>/dev/null | jq '.data.commands|length' 2>/dev/null)
  claimed=$(grep -oE '>[0-9]+ commands<' docs/guide.html | grep -oE '[0-9]+' | head -1)
  if [ -n "$claimed" ] && [ -n "$n" ] && [ "$n" -gt 0 ] && [ "$claimed" != "$n" ]; then
    note "the guide advertises $claimed commands; the surface publishes $n"
  fi
fi

# --- the hook environment the guide and the field reference advertise ---
# The published page listed eight variables for a commit after two more were added, so a reader
# writing a hook was told less than the tool gives. `hooks ls --output json` publishes the list
# precisely so this can be checked instead of remembered.
if [ -x ./hydra ] && command -v jq >/dev/null 2>&1; then
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
        grep -q "$key" "$doc" || note "$doc never mentions $key, which every hook receives"
      done
    done
  fi
fi

# --- the guide URL the binary publishes must be the guide that exists ---
# An agent has the binary and the skill, not a repository checkout, so `commands --output json`
# is the only place it can learn where the docs are. A URL published there and wrong is worse
# than none, so it is checked against README's link and the guide's own canonical URL.
if [ -x ./hydra ] && command -v jq >/dev/null 2>&1; then
  published=$(./hydra commands --output json 2>/dev/null | jq -r '.data.docs // empty')
  if [ -z "$published" ]; then
    note "hydra commands publishes no docs URL, so a caller with only the binary cannot find the guide"
  else
    grep -q "$published" README.md || note "README does not link the guide URL the binary publishes ($published)"
    grep -q "$published" docs/guide.html || note "the guide does not name the URL the binary publishes ($published)"
  fi
fi

# --- documented manifests must actually load ---
# Every schema-2 example was renested for schema 3 by hand, and two were missed: one group in
# README kept the old nesting, which parses as a group with ZERO repos and silently drops the
# repository. Eyeballing caught four and missed two, so the examples are loaded instead of read.
#
# This block MUST stay above the exit gate below. Appended after it, `note` set fail=1 after the
# last thing that read it: the drift printed, the success line printed too, and the script exited 0.
if command -v python3 >/dev/null 2>&1 && [ -x ./hydra ] && python3 -c 'import yaml' 2>/dev/null; then
  manifest_report=$(python3 scripts/check_doc_manifests.py 2>&1) || {
    echo "$manifest_report" >&2
    note "a documented manifest does not load, or loses a repository"
  }
fi

[ "$fail" -eq 0 ] || { echo "check-docs-claims: docs contradict the build" >&2; exit 1; }
echo "check-docs-claims: version, theme, card, command count, hook env and documented manifests agree with the build"

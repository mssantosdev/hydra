#!/usr/bin/env bash
# End-to-end proof for the hydra worktree/upstream contract.
#
# Every assertion below targets a specific defect that shipped in v0.0.16:
# upstream tracking that was never configured, worktrees created inside the git
# dir, an inert `sync`, `--delete-branch` that printed success without deleting,
# a `switch` that refused to answer any script, and a fictional exit-code table.
#
# Usage: scripts/e2e.sh [path-to-hydra-binary]
set -euo pipefail

HYDRA=$(cd "$(dirname "$0")/.." && pwd)/${1:-hydra}
[ -x "$HYDRA" ] || { echo "FAIL: no hydra binary at $HYDRA (run make build)"; exit 1; }
command -v jq >/dev/null || { echo "FAIL: jq is required"; exit 1; }

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
export HYDRA_CONFIG_DIR="$T/config"

pass=0
ok()   { pass=$((pass+1)); printf '  ok   %s\n' "$1"; }
fail() { printf '  FAIL %s\n' "$1"; exit 1; }
check(){ if eval "$2"; then ok "$1"; else fail "$1"; fi; }

# ---------------------------------------------------------------- upstream repo
echo "== fixture =="
git init -q -b main "$T/upstream"
git -C "$T/upstream" -c user.email=t@t -c user.name=T commit -q --allow-empty -m init
git -C "$T/upstream" branch stage main
git -C "$T/upstream" config receive.denyCurrentBranch ignore
ok "upstream with main + stage"

mkdir -p "$T/ws" && cd "$T/ws"
"$HYDRA" init --project-name demo >/dev/null
"$HYDRA" clone "$T/upstream" --alias api --group backend --branches main --yes --output json >/dev/null
"$HYDRA" add api stage --output json >/dev/null
ok "init + clone + add"

# ------------------------------------------------ 1. layout (step 4)
echo "== 1. sibling layout, nothing inside the git dir =="
check "backend/api exists"            'test -d backend/api'
check "backend/api-stage exists"      'test -d backend/api-stage'
check "no worktree inside .bare"      'test ! -e .bare/api.git/main && test ! -e .bare/api.git/stage'
check "backend/api is a real dir"     'test ! -L backend/api'
check "backend/api-stage is real"     'test ! -L backend/api-stage'
check "no symlink in group dir"       '! find backend -maxdepth 1 -type l | grep -q .'

# ------------------------------------------------ 2. THE BUG (step 3)
echo "== 2. upstream tracking is configured =="
check "api-stage tracks origin/stage" \
  '[ "$(git -C backend/api-stage rev-parse --abbrev-ref "@{upstream}")" = origin/stage ]'
check "api tracks origin/main" \
  '[ "$(git -C backend/api rev-parse --abbrev-ref "@{upstream}")" = origin/main ]'
check "fetch refspec present" \
  '[ "$(git -C .bare/api.git config --get remote.origin.fetch)" = "+refs/heads/*:refs/remotes/origin/*" ]'
check "origin/HEAD resolves to main" \
  '[ "$(git -C .bare/api.git symbolic-ref refs/remotes/origin/HEAD)" = refs/remotes/origin/main ]'

# ------------------------------------------------ 3. sync (steps 3 + 6)
echo "== 3. sync is no longer inert =="
# `status` is deliberately offline (like `git status`): it never fetches, so the
# behind count comes from `sync`, which fetches before reporting.
git -C "$T/upstream" -c user.email=t@t -c user.name=T commit -q --allow-empty -m ahead
check "sync detects 1 behind and pulls it" \
  '{ "$HYDRA" sync api --yes --output json 2>/dev/null || true; } | jq -e ".data.worktrees[]|select(.branch==\"main\")|(.behind==1 and .pulled==true and .status==\"pulled\")" >/dev/null'
check "main is up to date after sync" \
  '"$HYDRA" status --output json | jq -e ".data.worktrees[]|select(.branch==\"main\")|.behind==0" >/dev/null'
check "a second sync reports nothing to do" \
  '{ "$HYDRA" sync api --yes --output json 2>/dev/null || true; } | jq -e ".data.summary.pulled==0" >/dev/null'

# ------------------------------------------------ 4. machine contract (step 5)
echo "== 4. machine contract and exit codes =="
check "list envelope has schema 1 and 2 worktrees" \
  '"$HYDRA" list --output json | jq -e ".schema==1 and (.data.worktrees|length)==2" >/dev/null'
check "non-TTY stdout auto-selects JSON" \
  '"$HYDRA" list | jq -e ".schema==1" >/dev/null'
check "HYDRA_OUTPUT=text forces text" \
  '! (HYDRA_OUTPUT=text "$HYDRA" list | jq -e . >/dev/null 2>&1)'
check "text output carries no ANSI" \
  '! HYDRA_OUTPUT=text "$HYDRA" list | grep -q "$(printf "\033")"'
check "not_in_project exits 2" \
  '(cd "$T" && "$HYDRA" list >/dev/null 2>&1; test $? -eq 2)'
# `add <new-branch>` deliberately CREATES the branch, so an unknown branch name is
# not an error. branch_unknown is for an unknown BASE, which cannot be invented.
check "unknown --from base -> branch_unknown" \
  '{ "$HYDRA" add api feat/needs-base --from does-not-exist --output json 2>&1 || true; } | jq -e ".error.code==\"branch_unknown\"" >/dev/null'
check "add on an unknown branch creates it as local-only" \
  '"$HYDRA" add api feat/invented --output json | jq -e ".data.upstream==null and .data.branch==\"feat/invented\"" >/dev/null'
check "unknown repo -> repo_unknown" \
  '{ "$HYDRA" add nosuchrepo main --output json 2>&1 || true; } | jq -e ".error.code==\"repo_unknown\"" >/dev/null'
check "unknown project -> exit 2" \
  '"$HYDRA" --project nope list >/dev/null 2>&1; test $? -eq 2'
check "local-only branch reports upstream null" \
  '"$HYDRA" add api feat/brand-new --output json >/dev/null &&
   "$HYDRA" list --output json | jq -e ".data.worktrees[]|select(.branch==\"feat/brand-new\")|.upstream==null" >/dev/null'

# ------------------------------------------------ 5. path / switch (step 6)
echo "== 5. path and switch work without the shell helper =="
check "path prints the absolute worktree path" \
  '[ "$(env -u HYDRA_SHELL_HELPER "$HYDRA" path api-stage)" = "$PWD/backend/api-stage" ]'
check "switch exits 0 without the helper" \
  'env -u HYDRA_SHELL_HELPER "$HYDRA" switch api-stage >/dev/null 2>&1'
check "switch --cd without helper exits 3" \
  'env -u HYDRA_SHELL_HELPER "$HYDRA" switch api-stage --cd >/dev/null 2>&1; test $? -eq 3'
check "unknown worktree -> worktree_unknown" \
  '{ "$HYDRA" path nope --output json 2>&1 || true; } | jq -e ".error.code==\"worktree_unknown\"" >/dev/null'

# ------------------------------------------------ 6. name conflicts (step 4)
echo "== 6. --as and name conflicts =="
"$HYDRA" add api feat/long-branch-name --as short --output json >/dev/null
check "--as chose the directory name" 'test -d backend/short'
check "collision is refused, never auto-suffixed" \
  '{ "$HYDRA" add api other/branch --as short --output json 2>&1 || true; } | jq -e ".error.code|test(\"worktree_name_conflict|branch_unknown\")" >/dev/null'

# ------------------------------------------------ 7. delete-branch (step 6, D1)
echo "== 7. --delete-branch actually deletes =="
"$HYDRA" remove api stage --yes --delete-branch --output json >/dev/null
check "worktree directory gone"  'test ! -d backend/api-stage'
check "branch really deleted" \
  '! git -C .bare/api.git show-ref --verify --quiet refs/heads/stage'

# ------------------------------------------------ 8. hooks (step 7)
echo "== 8. hooks run with the documented environment =="
cat >> .hydra/config.yaml <<'YAML'
hooks:
  post_add:
    - run: 'printf "%s|%s|%s" "$HYDRA_BRANCH" "$HYDRA_REPO" "$HYDRA_GROUP" > .hydra-hook-ran'
YAML
"$HYDRA" add api stage --output json >/dev/null
check "post_add ran in the worktree with injected env" \
  '[ "$(cat backend/api-stage/.hydra-hook-ran)" = "stage|api|backend" ]'
check "--no-hooks skips hooks" \
  '"$HYDRA" add api feat/nohook --no-hooks --output json >/dev/null &&
   test ! -e backend/api-feat-nohook/.hydra-hook-ran'

# ------------------------------------------------ 9. doctor (step 8)
echo "== 9. doctor detects and repairs a hand-broken workspace =="
git -C .bare/api.git config --unset remote.origin.fetch
check "doctor reports missing_fetch_refspec as fail" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e ".data.checks[]|select(.id==\"missing_fetch_refspec\")|.status==\"fail\"" >/dev/null'
"$HYDRA" doctor --fix --output json >/dev/null 2>&1 || true
check "doctor --fix restored the refspec" \
  '[ "$(git -C .bare/api.git config --get remote.origin.fetch)" = "+refs/heads/*:refs/remotes/origin/*" ]'
check "doctor is clean afterwards" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e "[.data.checks[]|select(.status==\"fail\")]|length==0" >/dev/null'

# ------------------------------------------------ 9b. topic membership (step 3)
# There is no `topic` command yet, so membership is written the way the store
# writes it. What is under test is the WIRING: decoration, filtering, detach on
# remove, and doctor's drift check.
#
# This section creates its OWN worktree rather than reusing one an earlier section
# touched: section 5 leaves api-stage dirty. It also passes --no-hooks, because
# section 8 installs a post_add hook that writes into every new worktree — which
# would make the remove assertions test hook side effects, not membership.
echo "== 9b. topic membership is recorded, reported, filtered and repaired =="
"$HYDRA" add api feat/topic-demo --no-hooks --output json >/dev/null
check "a fresh worktree reports topic null" \
  '"$HYDRA" list --output json | jq -e ".data.worktrees[]|select(.branch==\"feat/topic-demo\")|has(\"topic\") and .topic==null" >/dev/null'

write_state(){ mkdir -p .hydra && cat > .hydra/state.yaml; }
write_state <<'YAML'
version: "1"
topics:
  "2072958":
    members:
      - repo: api
        branch: feat/topic-demo
YAML

check "membership reaches the envelope" \
  '"$HYDRA" list --output json | jq -e ".data.worktrees[]|select(.branch==\"feat/topic-demo\")|.topic==\"2072958\"" >/dev/null'
check "--topic narrows to recorded members" \
  '[ "$("$HYDRA" list --topic 2072958 --output json | jq ".data.worktrees|length")" = 1 ]'
check "--topic on status narrows too" \
  '[ "$("$HYDRA" status --topic 2072958 --output json | jq ".data.worktrees|length")" = 1 ]'
# Error envelopes are written to stderr, so they must be redirected to be parsed.
check "an unknown topic is an error, not an empty list" \
  '{ "$HYDRA" list --topic 9999999 --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"topic_unknown\"" >/dev/null'
check "the unknown-topic error lists the known ids" \
  '{ "$HYDRA" list --topic 9999999 --output json 2>&1 >/dev/null || true; } | jq -e ".error.details.known|index(\"2072958\")!=null" >/dev/null'
check "topic_unknown exits 1" \
  '{ "$HYDRA" list --topic 9999999 --output json >/dev/null 2>&1; [ "$?" = 1 ]; }'

# Drift: a member whose worktree never existed. This is the state an interrupted
# remove leaves behind.
write_state <<'YAML'
version: "1"
topics:
  "2072958":
    members:
      - repo: api
        branch: feat/topic-demo
      - repo: api
        branch: feat/vanished
YAML
check "doctor reports the dangling member" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e ".data.checks[]|select(.id==\"topic_dangling_member\")|.branch==\"feat/vanished\"" >/dev/null'
check "the healthy member is not reported as dangling" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e "[.data.checks[]|select(.id==\"topic_dangling_member\")]|length==1" >/dev/null'
"$HYDRA" doctor --fix --output json >/dev/null 2>&1 || true
check "doctor --fix detached only the dangling member" \
  '"$HYDRA" list --topic 2072958 --output json | jq -e ".data.worktrees|length==1" >/dev/null'
check "doctor is clean after the topic fix" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e "[.data.checks[]|select(.status==\"fail\")]|length==0" >/dev/null'

# remove detaches AFTER the worktree is gone, so state cannot outlive it.
check "remove reports the topic it detached" \
  '"$HYDRA" remove api feat/topic-demo --yes --output json | jq -e ".data.topic==\"2072958\"" >/dev/null'
check "the removed worktree is gone" '! test -d backend/api-feat-topic-demo'
check "doctor stays clean after remove detached" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e "[.data.checks[]|select(.id==\"topic_dangling_member\")]|length==0" >/dev/null'
check "an emptied topic no longer matches anything" \
  '{ "$HYDRA" list --topic 2072958 --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"topic_unknown\"" >/dev/null'

# ------------------------------------------------ 9c. selector surface (step 4)
echo "== 9c. the selector narrows, and ambiguity is refused =="
check "--repos narrows to one repository" \
  '[ "$("$HYDRA" list --repos api --output json | jq "[.data.worktrees[]|select(.repo!=\"api\")]|length")" = 0 ]'
check "--group narrows to one group" \
  '[ "$("$HYDRA" list --group backend --output json | jq "[.data.worktrees[]|select(.group!=\"backend\")]|length")" = 0 ]'
check "an unknown --repos value is refused" \
  '{ "$HYDRA" list --repos nope --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"repo_unknown\"" >/dev/null'
check "the unknown-repo error lists the known aliases" \
  '{ "$HYDRA" list --repos nope --output json 2>&1 >/dev/null || true; } | jq -e ".error.details.known|index(\"api\")!=null" >/dev/null'
check "an unknown --group value is refused" \
  '{ "$HYDRA" list --group nope --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"repo_unknown\"" >/dev/null'

"$HYDRA" add api feat/selector --no-hooks --output json >/dev/null
check "--filter branch:<glob> narrows by branch" \
  '[ "$("$HYDRA" list --filter "branch:feat/*" --output json | jq "[.data.worktrees[]|select(.branch|startswith(\"feat/\")|not)]|length")" = 0 ]'
check "--filter branch matched the new worktree" \
  '"$HYDRA" list --filter "branch:feat/*" --output json | jq -e "[.data.worktrees[]|select(.branch==\"feat/selector\")]|length==1" >/dev/null'
# An earlier section leaves api-stage dirty, so assert about THIS worktree rather
# than about the workspace being globally clean.
check "--filter dirty excludes the clean worktree" \
  '[ "$("$HYDRA" list --filter dirty --output json | jq "[.data.worktrees[]|select(.branch==\"feat/selector\")]|length")" = 0 ]'
echo scratch > backend/api-feat-selector/scratch.txt
check "--filter dirty finds the dirtied worktree" \
  '"$HYDRA" list --filter dirty --output json | jq -e "[.data.worktrees[]|select(.branch==\"feat/selector\")]|length==1" >/dev/null'
# Filters intersect: dirty AND a non-matching branch glob yields nothing, even
# though each alone matches feat/selector.
check "filters combine as an intersection, not a union" \
  '[ "$("$HYDRA" list --filter dirty --filter "branch:hotfix/*" --output json | jq "[.data.worktrees[]|select(.branch==\"feat/selector\")]|length")" = 0 ]'
check "an invalid --filter value is refused" \
  '{ "$HYDRA" list --filter nope --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"internal\"" >/dev/null'
check "the invalid-filter error names the valid set" \
  '{ "$HYDRA" list --filter nope --output json 2>&1 >/dev/null || true; } | jq -e ".error.details.valid|index(\"dirty\")!=null" >/dev/null'
# Ambiguity needs a branch name that exists in two repos. The fixture registers one,
# so clone the same upstream a second time under a different group and alias: both
# then have a "main" worktree, which is the ordinary shape that made first-match
# resolution dangerous.
"$HYDRA" clone "$T/upstream" --alias web --group frontend --branches main --yes --output json >/dev/null
check "the second repo has its own main worktree" 'test -d frontend/web'
# Ambiguity: main exists in every repo, so a bare branch name names several.
check "an ambiguous handle is refused by path" \
  '{ "$HYDRA" path main --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
check "the ambiguity error lists every candidate" \
  '{ "$HYDRA" path main --output json 2>&1 >/dev/null || true; } | jq -e ".error.details.candidates|length>=2" >/dev/null'
check "a group-qualified handle still resolves" \
  '[ "$("$HYDRA" path backend/api)" = "$PWD/backend/api" ]'
check "an ambiguous handle is refused by remove" \
  '{ "$HYDRA" remove main --yes --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
check "the refused remove deleted nothing" 'test -d backend/api'

# path --topic must print exactly one path.
"$HYDRA" list --output json >/dev/null
write_state <<'YAML'
version: "1"
topics:
  spanning:
    members:
      - repo: api
        branch: main
      - repo: api
        branch: feat/selector
YAML
check "path --topic refuses a topic spanning worktrees" \
  '{ "$HYDRA" path --topic spanning --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
write_state <<'YAML'
version: "1"
topics:
  solo:
    members:
      - repo: api
        branch: feat/selector
YAML
check "path --topic prints the single worktree path" \
  '[ "$("$HYDRA" path --topic solo)" = "$PWD/backend/api-feat-selector" ]'
check "a handle and --topic together are refused" \
  '{ "$HYDRA" path backend/api --topic solo --output json 2>&1 >/dev/null || true; } | jq -e ".error.code==\"internal\"" >/dev/null'

rm -f backend/api-feat-selector/scratch.txt
"$HYDRA" remove api feat/selector --yes --output json >/dev/null
rm -f .hydra/state.yaml

# ------------------------------------------------ 10. registry (step 2)
echo "== 10. project registry =="
check "the workspace registered itself" \
  '"$HYDRA" project ls --output json | jq -e ".data.projects[]|select(.name==\"demo\")|.exists" >/dev/null'
check "--project resolves from the registry" \
  '(cd "$T" && "$HYDRA" --project demo list --output json | jq -e ".schema==1" >/dev/null)'

# ------------------------------------------------ 11. agent skill (step 9)
echo "== 11. agent skill: emitted, installable, thin =="
check "skill opens with frontmatter" '[ "$("$HYDRA" skill | head -1)" = "---" ]'
check "skill is at most 120 lines"   '[ "$("$HYDRA" skill | wc -l)" -le 120 ]'
check "skill installs outside a workspace" \
  '(cd "$T" && "$HYDRA" skill --install --dir "$T/ws/.agents/skills" >/dev/null) &&
   test -f "$T/ws/.agents/skills/hydra/SKILL.md"'

echo
echo "ALL $pass ASSERTIONS PASSED"

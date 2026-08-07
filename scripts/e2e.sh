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
"$HYDRA" repo add "$T/upstream" --as api --group backend --branches main --output json >/dev/null
"$HYDRA" add api stage --output json >/dev/null
ok "init + clone + add"

# add is documented convergent (invariant 3). It used to exit 1 with worktree_exists on a
# retry, which killed any provisioning script re-running its own steps — a cloud-init retry,
# a resumed setup. start, apply and clone always treated this as a skip; add did not.
check "add is convergent: retry exits 0"  '"$HYDRA" add api stage --output json >/dev/null'
check "retry reports skipped"             '"$HYDRA" add api stage --output json | jq -e ".data.disposition==\"skipped\""'

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
# The fan-out engine's two user-visible guarantees (step 5).
check "sync results are sorted by group/repo/branch, not finish order" \
  '[ "$("$HYDRA" sync --yes --output json 2>/dev/null | jq -r "[.data.worktrees[]|\"\(.group)/\(.repo)/\(.branch)\"]|(. == (.|sort))")" = "true" ]'

# Snapshot and restore the manifest rather than editing the hook back out: section 8
# appends its own `hooks:` block, and a partial removal leaves a duplicate mapping
# key that makes every later command fail to parse the config.
cp .hydra/config.yaml "$T/config.before-hook"
printf '\nhooks:\n  post_sync:\n    - run: "exit 1"\n' >> .hydra/config.yaml
git -C "$T/upstream" -c user.email=t@t -c user.name=T commit -q --allow-empty -m hooked
check "a failing post_sync hook warns instead of aborting" \
  '"$HYDRA" sync api --yes --output json 2>/dev/null | jq -e ".data.summary.pulled==1 and .data.summary.failed==0 and (.warnings|length)>0" >/dev/null'
cp "$T/config.before-hook" .hydra/config.yaml
check "the manifest is restored and still parses" \
  '"$HYDRA" list --output json | jq -e ".schema==3" >/dev/null && ! grep -q post_sync .hydra/config.yaml'

# ------------------------------------------------ 3b. clone converges (step 5)
echo "== 3b. re-cloning a complete repository is a no-op =="
# Before the fan-out engine every already-present branch counted as a failure, so
# this exact command returned git_failed "no worktree could be created".
check "a second identical clone exits 0" \
  '"$HYDRA" repo add "$T/upstream" --as api --group backend --branches main --output json >/dev/null 2>&1'
check "the converged clone still reports the worktree" \
  '"$HYDRA" repo add "$T/upstream" --as api --group backend --branches main --output json 2>/dev/null | jq -e ".data.worktrees|length>=1" >/dev/null'
check "the re-clone destroyed nothing" 'test -d backend/api && test -d .bare/api.git'

# ------------------------------------------------ 4. machine contract (step 5)
echo "== 4. machine contract and exit codes =="
check "list envelope has schema 3 and 2 worktrees" \
  '"$HYDRA" list --output json | jq -e ".schema==3 and (.data.worktrees|length)==2" >/dev/null'
check "non-TTY stdout auto-selects JSON" \
  '"$HYDRA" list | jq -e ".schema==3" >/dev/null'
# The envelope fields (step 6): every envelope carries them, so a consumer never has
# to reconstruct "what happened" from data or interpret an exit status.
check "every success envelope carries outcome and summary" \
  '"$HYDRA" list --output json | jq -e ".outcome==\"success\" and (.summary|type==\"string\" and length>0)" >/dev/null'
check "next is omitted rather than null when empty" \
  '"$HYDRA" list --output json | jq -e "has(\"next\")|not" >/dev/null'
check "error envelopes carry outcome failure" \
  '{ "$HYDRA" list --topic nope --output json 2>/dev/null || true; } | jq -e ".outcome==\"failure\"" >/dev/null'
check "a terminal error is retryable:false, never absent" \
  '{ "$HYDRA" list --topic nope --output json 2>/dev/null || true; } | jq -e ".error|has(\"retryable\") and .retryable==false" >/dev/null'
check "exit is still absent from the error envelope" \
  '{ "$HYDRA" list --topic nope --output json 2>/dev/null || true; } | jq -e ".error|has(\"exit\")|not" >/dev/null'
check "summary states the actual count" \
  '[ "$("$HYDRA" list --output json | jq -r .summary)" = "2 worktree(s)" ]'
check "HYDRA_OUTPUT=text forces text" \
  '! (HYDRA_OUTPUT=text "$HYDRA" list | jq -e . >/dev/null 2>&1)'
check "text output carries no ANSI" \
  '! HYDRA_OUTPUT=text "$HYDRA" list | grep -q "$(printf "\033")"'
# The shared table (step 9). list and status render through ONE renderer, so a column
# cannot appear in one and silently be missing from the other.
#
# Each assertion captures the render ONCE into a variable. Re-running hydra inside a
# multi-command && chain made the earlier version fail for reasons unrelated to the
# table, and one invocation is both faster and easier to reason about.
list_text=$(HYDRA_OUTPUT=text "$HYDRA" list)
status_text=$(HYDRA_OUTPUT=text "$HYDRA" status)
status_against=$(HYDRA_OUTPUT=text "$HYDRA" status --against main)

check "list renders a table header" \
  'case "$list_text" in *WORKTREE*BRANCH*STATUS*) true;; *) false;; esac'
check "the table draws a header rule" \
  'case "$list_text" in *"────"*) true;; *) false;; esac'
check "status shows the UPSTREAM column that list omits" \
  'case "$status_text" in *UPSTREAM*) true;; *) false;; esac &&
   case "$list_text" in *UPSTREAM*) false;; *) true;; esac'
check "--against adds a VS REF column, absent otherwise" \
  'case "$status_against" in *"VS REF"*) true;; *) false;; esac &&
   case "$status_text" in *"VS REF"*) false;; *) true;; esac'
check "every worktree occupies exactly one row" \
  '[ "$(printf "%s\n" "$list_text" | grep -c "clean\|dirty")" = "$("$HYDRA" list --output json | jq ".data.worktrees|length")" ]'
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
  '{ "$HYDRA" list --topic 9999999 --output json 2>/dev/null || true; } | jq -e ".error.code==\"topic_unknown\"" >/dev/null'
check "the unknown-topic error lists the known ids" \
  '{ "$HYDRA" list --topic 9999999 --output json 2>/dev/null || true; } | jq -e ".error.details.known|index(\"2072958\")!=null" >/dev/null'
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
  '{ "$HYDRA" list --topic 2072958 --output json 2>/dev/null || true; } | jq -e ".error.code==\"topic_unknown\"" >/dev/null'

# ------------------------------------------------ 9c. selector surface (step 4)
echo "== 9c. the selector narrows, and ambiguity is refused =="
check "--repos narrows to one repository" \
  '[ "$("$HYDRA" list --repos api --output json | jq "[.data.worktrees[]|select(.repo!=\"api\")]|length")" = 0 ]'
check "--group narrows to one group" \
  '[ "$("$HYDRA" list --group backend --output json | jq "[.data.worktrees[]|select(.group!=\"backend\")]|length")" = 0 ]'
check "an unknown --repos value is refused" \
  '{ "$HYDRA" list --repos nope --output json 2>/dev/null || true; } | jq -e ".error.code==\"repo_unknown\"" >/dev/null'
check "the unknown-repo error lists the known aliases" \
  '{ "$HYDRA" list --repos nope --output json 2>/dev/null || true; } | jq -e ".error.details.known|index(\"api\")!=null" >/dev/null'
check "an unknown --group value is refused" \
  '{ "$HYDRA" list --group nope --output json 2>/dev/null || true; } | jq -e ".error.code==\"repo_unknown\"" >/dev/null'

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
  '{ "$HYDRA" list --filter nope --output json 2>/dev/null || true; } | jq -e ".error.code==\"internal\"" >/dev/null'
check "the invalid-filter error names the valid set" \
  '{ "$HYDRA" list --filter nope --output json 2>/dev/null || true; } | jq -e ".error.details.valid|index(\"dirty\")!=null" >/dev/null'
# Ambiguity needs a branch name that exists in two repos. The fixture registers one,
# so clone the same upstream a second time under a different group and alias: both
# then have a "main" worktree, which is the ordinary shape that made first-match
# resolution dangerous.
"$HYDRA" repo add "$T/upstream" --as web --group frontend --branches main --output json >/dev/null
check "the second repo has its own main worktree" 'test -d frontend/web'
# Ambiguity: main exists in every repo, so a bare branch name names several.
check "an ambiguous handle is refused by path" \
  '{ "$HYDRA" path main --output json 2>/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
check "the ambiguity error lists every candidate" \
  '{ "$HYDRA" path main --output json 2>/dev/null || true; } | jq -e ".error.details.candidates|length>=2" >/dev/null'
check "a group-qualified handle still resolves" \
  '[ "$("$HYDRA" path backend/api)" = "$PWD/backend/api" ]'
check "an ambiguous handle is refused by remove" \
  '{ "$HYDRA" remove main --yes --output json 2>/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
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
  '{ "$HYDRA" path --topic spanning --output json 2>/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'
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
  '{ "$HYDRA" path backend/api --topic solo --output json 2>/dev/null || true; } | jq -e ".error.code==\"internal\"" >/dev/null'

rm -f backend/api-feat-selector/scratch.txt
"$HYDRA" remove api feat/selector --yes --output json >/dev/null
rm -f .hydra/state.yaml

# ------------------------------------------------ 9d. topic commands (step 7b)
echo "== 9d. the topic command tree =="
"$HYDRA" add api feat/t1 --no-hooks --output json >/dev/null
"$HYDRA" add api feat/t2 --no-hooks --output json >/dev/null

check "an empty workspace lists no topics" \
  '"$HYDRA" topic list --output json | jq -e ".data.total==0" >/dev/null'
check "attach creates the topic" \
  '"$HYDRA" topic attach 3001 backend/api-feat-t1 --output json | jq -e ".data.topic==\"3001\"" >/dev/null'
check "topic list now reports it" \
  '"$HYDRA" topic list --output json | jq -e "[.data.topics[].id]|index(\"3001\")!=null" >/dev/null'
check "attaching a second worktree extends it" \
  '"$HYDRA" topic attach 3001 backend/api-feat-t2 --output json >/dev/null &&
   "$HYDRA" topic show 3001 --output json | jq -e ".data.members|length==2" >/dev/null'
check "a worktree cannot belong to two topics" \
  '{ "$HYDRA" topic attach 3002 backend/api-feat-t1 --output json 2>/dev/null || true; } | jq -e ".error.code==\"topic_conflict\" and .error.details.existing_topic==\"3001\"" >/dev/null'
check "show on an unknown topic lists the known ids" \
  '{ "$HYDRA" topic show 9999 --output json 2>/dev/null || true; } | jq -e ".error.code==\"topic_unknown\" and (.error.details.known|index(\"3001\")!=null)" >/dev/null'
check "detaching a non-member is refused" \
  '{ "$HYDRA" topic detach 3001 backend/api --output json 2>/dev/null || true; } | jq -e ".error.code==\"worktree_unknown\"" >/dev/null'

# The gates, which are the reason this is not a loop over single-worktree remove.
#
# Order matters: the dirty gate (step 3) runs BEFORE the confirmation gate (step 5),
# so the --yes check must be made while the workspace is still clean or the dirty
# gate answers first.
check "a non-TTY removal without --yes refuses" \
  '{ "$HYDRA" topic remove 3001 --with-worktrees --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"--yes\")!=null)" >/dev/null'

echo scratch > backend/api-feat-t2/scratch.txt
check "one dirty member refuses the WHOLE removal" \
  '{ "$HYDRA" topic remove 3001 --with-worktrees --yes --output json 2>/dev/null || true; } | jq -e ".error.code==\"worktree_dirty\"" >/dev/null'
check "the dirty gate names the offending member" \
  '{ "$HYDRA" topic remove 3001 --with-worktrees --yes --output json 2>/dev/null || true; } | jq -e "[.error.details.dirty[].branch]|index(\"feat/t2\")!=null" >/dev/null'
check "the refused removal mutated nothing" \
  'test -d backend/api-feat-t1 && test -d backend/api-feat-t2 &&
   "$HYDRA" topic show 3001 --output json | jq -e ".data.members|length==2" >/dev/null'
rm -f backend/api-feat-t2/scratch.txt

check "--dry-run changes nothing" \
  '"$HYDRA" topic remove 3001 --with-worktrees --dry-run --output json | jq -e ".data.dry_run==true" >/dev/null &&
   test -d backend/api-feat-t1'
check "membership-only removal keeps the worktrees" \
  '"$HYDRA" topic remove 3001 --yes --output json | jq -e ".data.topic_removed==true" >/dev/null &&
   test -d backend/api-feat-t1 && test -d backend/api-feat-t2'
check "the topic is garbage-collected with its last member" \
  '"$HYDRA" topic list --output json | jq -e ".data.total==0" >/dev/null'

# --with-worktrees really removes them, and detach happens after each success.
"$HYDRA" topic attach 3003 backend/api-feat-t1 --output json >/dev/null
"$HYDRA" topic attach 3003 backend/api-feat-t2 --output json >/dev/null
check "--with-worktrees removes every member's worktree" \
  '"$HYDRA" topic remove 3003 --with-worktrees --yes --output json | jq -e ".data.targets|length==2 and all(.worktree_removed and .detached)" >/dev/null'
check "both worktrees are gone from disk" \
  '! test -d backend/api-feat-t1 && ! test -d backend/api-feat-t2'
check "doctor is clean after a topic removal" \
  '{ "$HYDRA" doctor --output json 2>/dev/null || true; } | jq -e "[.data.checks[]|select(.status==\"fail\")]|length==0" >/dev/null'

# ------------------------------------------------ 9e. hydra start (step 7c)
echo "== 9e. start: two axes, convergent, no guessing =="
# A second repo, so "which repos" is a real question.
"$HYDRA" repo add "$T/upstream" --as web --group frontend --branches main --output json >/dev/null 2>&1 || true

check "start with no selector asks which repos" \
  '{ "$HYDRA" start feat/x --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.one_of|index(\"--repos\")!=null)" >/dev/null'
check "start with a selector but nothing to name a branch asks for --branch" \
  '{ "$HYDRA" start --repos api --topic 5001 --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"--branch\")!=null)" >/dev/null'
check "the refused start recorded no topic" \
  '"$HYDRA" topic list --output json | jq -e "[.data.topics[].id]|index(\"5001\")==null" >/dev/null'

check "start creates one branch across two repos and records the topic" \
  '"$HYDRA" start marcus/feat-login --repos api,web --topic 5002 --output json 2>/dev/null | jq -e ".outcome==\"success\" and (.data.created|length)==2 and .data.branch_source==\"positional\"" >/dev/null'
check "the topic has both members" \
  '"$HYDRA" topic show 5002 --output json | jq -e ".data.members|length==2" >/dev/null'
# next[] is argv plus a reason, not a command string: a caller execs it rather than
# parsing a shell line, so a branch containing a space needs no quoting decision.
check "start suggests status for the topic as argv" \
  '"$HYDRA" start marcus/feat-login --repos api --topic 5002 --output json 2>/dev/null |
   jq -e "[.next[]|select(.argv|index(\"status\"))]|length==1 and
          (.[0].argv|index(\"5002\"))!=null and (.[0].why|length)>0" >/dev/null'
check "re-running an identical start is convergent" \
  '"$HYDRA" start marcus/feat-login --repos api,web --topic 5002 --output json 2>/dev/null | jq -e "(.data.created|length)==0 and (.data.skipped|length)==2" >/dev/null'
check "extending to a repo needs no branch flag" \
  '"$HYDRA" start --topic 5002 --output json 2>/dev/null | jq -e ".data.branch==\"marcus/feat-login\" and .data.branch_source==\"topic_members\"" >/dev/null'

# The pattern, and the documented surprise.
cp .hydra/config.yaml "$T/config.before-pattern"
printf '\ndefaults:\n  branch_pattern: "{user}/{kind}-{slug}"\n' >> .hydra/config.yaml
check "a pattern generates the branch" \
  '"$HYDRA" start --topic 5003 --repos api --slug "Login Page" --kind feat --user marcus --output json 2>/dev/null | jq -e ".data.branch==\"marcus/feat-login-page\" and .data.branch_source==\"branch_pattern\"" >/dev/null'
check "a pattern missing a value names its flag" \
  '{ "$HYDRA" start --topic 5004 --repos api --kind feat --user marcus --output json 2>/dev/null || true; } | jq -e "(.error.details.missing|index(\"--slug\")!=null)" >/dev/null'
check "a positional branch is literal and the pattern never runs" \
  '"$HYDRA" start 5005 --repos api --slug login --kind feat --user marcus --output json 2>/dev/null | jq -e ".data.branch==\"5005\" and .data.branch_source==\"positional\"" >/dev/null'
check "an unknown placeholder is a config error, not a literal branch" \
  'printf "\ndefaults:\n  branch_pattern: \"{ticket}/x\"\n" > "$T/bad.yaml" &&
   cp "$T/config.before-pattern" .hydra/config.yaml &&
   cat "$T/bad.yaml" >> .hydra/config.yaml &&
   { "$HYDRA" start --topic 5006 --repos api --output json 2>/dev/null || true; } | jq -e ".error.code==\"branch_unknown\"" >/dev/null'
cp "$T/config.before-pattern" .hydra/config.yaml

check "--dry-run creates nothing" \
  '"$HYDRA" start feat/dry --repos api --dry-run --output json 2>/dev/null | jq -e ".data.dry_run==true" >/dev/null &&
   ! test -d backend/api-feat-dry'
check "start without --topic records nothing" \
  '"$HYDRA" start feat/loose --repos api --output json >/dev/null 2>&1 &&
   "$HYDRA" list --output json | jq -e ".data.worktrees[]|select(.branch==\"feat/loose\")|.topic==null" >/dev/null'

"$HYDRA" topic remove 5002 --with-worktrees --yes --output json >/dev/null 2>&1 || true
"$HYDRA" topic remove 5003 --with-worktrees --yes --output json >/dev/null 2>&1 || true

# ------------------------------------------------ 9f. --against REF (step 8)
echo "== 9f. --against answers merged-ness without storing an edge =="
# A release branch at main, plus a worktree carrying a commit release does not have.
git -C .bare/api.git branch release main
"$HYDRA" start feat/unmerged --repos api --output json >/dev/null 2>&1

git -C backend/api-feat-unmerged -c user.email=t@t -c user.name=T commit -q --allow-empty -m "not in release"

check "a branch with unique commits is NOT merged into the ref" \
  '"$HYDRA" status --repos api --against release --output json 2>/dev/null | jq -e ".data.worktrees[]|select(.branch==\"feat/unmerged\")|.against.merged==false and .against.ahead==1" >/dev/null'
check "main IS merged into a release branch pointing at it" \
  '"$HYDRA" status --repos api --against release --output json 2>/dev/null | jq -e ".data.worktrees[]|select(.branch==\"main\")|.against.merged==true and .against.ahead==0" >/dev/null'
check "merged is exactly ahead==0" \
  '"$HYDRA" status --repos api --against release --output json 2>/dev/null | jq -e "[.data.worktrees[]|select(.against!=null)|.against|.merged==(.ahead==0)]|all" >/dev/null'
check "the against block names the ref it compared with" \
  '"$HYDRA" status --repos api --against release --output json 2>/dev/null | jq -e "[.data.worktrees[]|select(.against!=null)|.against.ref]|all(.==\"release\")" >/dev/null'
check "an unresolvable ref warns and still lists" \
  '"$HYDRA" status --repos api --against no-such-ref --output json 2>/dev/null | jq -e "(.data.worktrees|length)>0 and (.warnings|length)>0 and .outcome==\"success\"" >/dev/null'
check "an unresolvable ref exits 0" \
  '"$HYDRA" status --repos api --against no-such-ref --output json >/dev/null 2>&1'
check "the against key is absent without --against" \
  '"$HYDRA" status --repos api --output json | jq -e "[.data.worktrees[]|has(\"against\")]|all(.==false)" >/dev/null'
check "list takes --against as well" \
  '"$HYDRA" list --repos api --against release --output json 2>/dev/null | jq -e "[.data.worktrees[]|.against.ref]|all(.==\"release\")" >/dev/null'
check "--against combines with a filter" \
  '"$HYDRA" status --repos api --against release --filter "branch:feat/*" --output json 2>/dev/null | jq -e "[.data.worktrees[]|select(.branch|startswith(\"feat/\")|not)]|length==0" >/dev/null'

"$HYDRA" remove api feat/unmerged --yes --output json >/dev/null 2>&1 || true

# ------------------------------------------------ 9i. hydra repo (step 11)
echo "== 9i. repo add is the one front door =="
check "repo list reports the registered repositories" \
  '"$HYDRA" repo list --output json | jq -e "(.data.total)>=1 and ([.data.repos[].alias]|index(\"api\")!=null)" >/dev/null'
check "repo list reports whether the bare exists on disk" \
  '"$HYDRA" repo list --output json | jq -e ".data.repos[]|select(.alias==\"api\")|.bare_exists==true" >/dev/null'

# --adopt is required, never inferred: a local path is an ordinary clone SOURCE.
git init -q -b main "$T/loose-checkout"
git -C "$T/loose-checkout" -c user.email=t@t -c user.name=T commit -q --allow-empty -m loose
check "a local path clones by default, it is not adopted" \
  '"$HYDRA" repo add "$T/loose-checkout" --as cloned --group imported --branches main --output json >/dev/null 2>&1 &&
   test -d .bare/cloned.git'
check "--adopt with --branches is refused rather than ignored" \
  '{ "$HYDRA" repo add "$T/loose-checkout" --adopt --group imported --branches main --output json 2>/dev/null || true; } | jq -e ".error.code==\"internal\"" >/dev/null'
check "--adopt without a group asks for one" \
  '{ "$HYDRA" repo add "$T/loose-checkout" --adopt --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"--group\")!=null)" >/dev/null'
check "repo add with no argument asks for one" \
  '{ "$HYDRA" repo add --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\"" >/dev/null'

# remove unregisters and deletes NOTHING.
check "repo remove refuses a repo with worktrees without --yes" \
  '{ "$HYDRA" repo remove cloned --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"--yes\")!=null)" >/dev/null'
check "repo remove unregisters and reports what it kept" \
  '"$HYDRA" repo remove cloned --yes --output json | jq -e "(.data.kept|length)>=1" >/dev/null'
check "the git data really survived being unregistered" 'test -d .bare/cloned.git'
check "the unregistered repo is gone from repo list" \
  '"$HYDRA" repo list --output json | jq -e "[.data.repos[].alias]|index(\"cloned\")==null" >/dev/null'
check "removing an unknown repo lists the known ones" \
  '{ "$HYDRA" repo remove nosuchrepo --output json 2>/dev/null || true; } | jq -e ".error.code==\"repo_unknown\" and (.error.details.known|index(\"api\")!=null)" >/dev/null'

# ------------------------------------------------ 9h. hydra run (step 11)
echo "== 9h. run: argv after --, never a shell =="
check "run in one worktree by handle" \
  '"$HYDRA" run backend/api --output json -- true 2>/dev/null | jq -e ".data.total==1 and .data.failed==0" >/dev/null'
check "run across a selector" \
  '[ "$("$HYDRA" run --group backend --output json -- true 2>/dev/null | jq ".data.total")" -ge 1 ]'
check "an ambiguous handle is refused" \
  '{ "$HYDRA" run main --output json -- true 2>/dev/null || true; } | jq -e ".error.code==\"worktree_name_conflict\"" >/dev/null'

# The safety property: no implicit shell, so a metacharacter is a literal argument.
check "metacharacters are NOT interpreted by a shell" \
  '"$HYDRA" run backend/api --output json -- echo "x; touch $T/e2e-shell-escape" >/dev/null 2>&1 &&
   ! test -f "$T/e2e-shell-escape"'
check "an explicit sh -c still works" \
  '"$HYDRA" run backend/api --output json -- sh -c "touch $T/e2e-explicit-shell" >/dev/null 2>&1 &&
   test -f "$T/e2e-explicit-shell"'

check "the documented environment reaches the command" \
  '"$HYDRA" run backend/api --output json -- sh -c "printf %s \"\$HYDRA_REPO/\$HYDRA_GROUP\" > $T/e2e-run-env" >/dev/null 2>&1 &&
   [ "$(cat "$T/e2e-run-env")" = "api/backend" ]'
check "the command runs inside the worktree" \
  '"$HYDRA" run backend/api --output json -- sh -c "printf %s \"\$PWD\" > $T/e2e-run-pwd" >/dev/null 2>&1 &&
   [ "$(cat "$T/e2e-run-pwd")" = "$PWD/backend/api" ]'

check "a failing command exits non-zero" \
  '"$HYDRA" run backend/api --output json -- false >/dev/null 2>&1; [ "$?" != 0 ]'
check "no command asks for one" \
  '{ "$HYDRA" run --group backend --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\"" >/dev/null'
# hydra exits non-zero here (the command failed), so the invocation must be wrapped or
# pipefail fails the whole assertion.
check "--timeout kills a hung command and says so" \
  '{ "$HYDRA" run backend/api --timeout 100ms --output json 2>/dev/null -- sleep 5 || true; } | jq -e ".data.timed_out==1" >/dev/null'

# ------------------------------------------------ 9g. parity holes closed (step 10)
echo "== 9g. every prompt has a flag, and none of them hang =="
# A prompt that cannot be shown must name the missing argument. worktree_unknown was
# wrong for these: nothing is unknown, nothing was named.
check "switch with no argument asks for one" \
  '{ "$HYDRA" switch --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"<worktree>\")!=null)" >/dev/null'
check "remove with no argument asks for one" \
  '{ "$HYDRA" remove --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\"" >/dev/null'
check "needs_input exits 7" \
  '"$HYDRA" switch --output json >/dev/null 2>&1; [ "$?" = 7 ]'

# sync --dirty. The stderr of sync carries git's fetch output before the envelope, so
# these assert on the EXIT CODE rather than parsing that stream.
echo dirty-content > backend/api/dirty-file.txt
git -C "$T/upstream" -c user.email=t@t -c user.name=T commit -q --allow-empty -m "for dirty policy"
check "a dirty worktree with no policy exits 7, not silently skipped" \
  '"$HYDRA" sync api --yes --output json >/dev/null 2>&1; [ "$?" = 7 ]'
check "--dirty skip leaves it alone and exits 0" \
  '"$HYDRA" sync api --yes --dirty skip --output json 2>/dev/null | jq -e ".data.summary.pulled==0" >/dev/null'
check "--dirty stash pulls and restores the change" \
  '"$HYDRA" sync api --yes --dirty stash --output json 2>/dev/null | jq -e ".data.summary.pulled==1" >/dev/null &&
   test -f backend/api/dirty-file.txt'
check "an invalid --dirty value is refused" \
  '"$HYDRA" sync api --yes --dirty nope --output json >/dev/null 2>&1; [ "$?" = 1 ]'
rm -f backend/api/dirty-file.txt

# config was read-only without a TTY: an agent could see settings but never write them.
check "config show reports without changing" \
  '"$HYDRA" config show --output json | jq -e "(.data.theme|length)>0 and (has(\"changed\")|not or .data.changed!=true)" >/dev/null'
check "config set editor persists" \
  '"$HYDRA" config set editor "e2e-editor" --output json | jq -e ".data.changed==true" >/dev/null &&
   "$HYDRA" config show --output json | jq -e ".data.editor==\"e2e-editor\"" >/dev/null'
check "config set theme rejects an unknown name with the valid set" \
  '{ "$HYDRA" config set theme no-such-theme --output json 2>/dev/null || true; } | jq -e "(.error.details.valid|length)>0" >/dev/null'
check "config set with no value asks for it" \
  '{ "$HYDRA" config set theme --output json 2>/dev/null || true; } | jq -e ".error.code==\"needs_input\" and (.error.details.missing|index(\"<value>\")!=null)" >/dev/null'

# ------------------------------------------------ 10. registry (step 2)
echo "== 10. project registry =="
check "the workspace registered itself" \
  '"$HYDRA" project ls --output json | jq -e ".data.projects[]|select(.name==\"demo\")|.exists" >/dev/null'
check "--project resolves from the registry" \
  '(cd "$T" && "$HYDRA" --project demo list --output json | jq -e ".schema==3" >/dev/null)'

# ------------------------------------------------ 11. agent skill (step 9)
echo "== 11. agent skill: emitted, installable, thin =="
check "skill opens with frontmatter" '[ "$("$HYDRA" skill | head -1)" = "---" ]'
check "skill is at most 120 lines"   '[ "$("$HYDRA" skill | wc -l)" -le 120 ]'
check "skill installs outside a workspace" \
  '(cd "$T" && "$HYDRA" skill --install --dir "$T/ws/.agents/skills" >/dev/null) &&
   test -f "$T/ws/.agents/skills/hydra/SKILL.md"'

# ------------------------------------- 12. surface and apply (step 12)
echo "== 12. commands surface, and apply round-trips list =="
check "commands publishes the surface outside a workspace" \
  '(cd "$T" && "$HYDRA" commands --output json |
    jq -e ".data.surface_schema==1 and (.data.commands|length)>20" >/dev/null)'
check "every documented error code carries an exit status" \
  '"$HYDRA" commands --output json |
   jq -e "(.data.error_codes|length)>15 and
          (.data.error_codes|all(.exit>=0 and .code!=\"\"))" >/dev/null'
check "the committed SURFACE.txt is not stale" \
  '(cd "$T" && "$HYDRA" commands --output text) |
   diff -q - "$(dirname "$HYDRA")/SURFACE.txt" >/dev/null'
# The round-trip the design rests on: what list emits, apply consumes, in a SECOND
# workspace - so this proves portability, not just idempotence in place. The replica has
# to register the same repos first: apply creates worktrees, never repositories.
check "apply reproduces a captured workspace elsewhere" \
  '(cd "$T/ws" && "$HYDRA" list --output json > "$T/captured.json") &&
   (mkdir -p "$T/ws2" && cd "$T/ws2" && "$HYDRA" init --project-name replica >/dev/null &&
    "$HYDRA" repo add "$T/upstream" --as api --group backend --branches main >/dev/null &&
    "$HYDRA" repo add "$T/upstream" --as web --group frontend --branches main >/dev/null &&
    "$HYDRA" repo add "$T/loose-checkout" --as cloned --group imported --branches main >/dev/null &&
    "$HYDRA" apply - < "$T/captured.json" --output json |
      jq -e ".outcome==\"success\" and .data.created>0 and .data.failed==0" >/dev/null)'
# Every branch the source had must exist in the replica. Containment, not equality: the
# replica's own `repo add` creates a default-branch worktree per repo, and the source had
# since removed one of those. Branch-carrying worktrees only - a detached worktree has no
# branch, so no document can describe it, which apply warns about below.
check "the replica holds every branch the source had" \
  '(cd "$T/ws2" && "$HYDRA" list --output json |
    jq -e --slurpfile src <(cd "$T/ws" && "$HYDRA" list --output json) "
      [\$src[0].data.worktrees[]|select(.branch!=\"\").branch] -
      [.data.worktrees[]|select(.branch!=\"\").branch] == []" >/dev/null)'
printf '[{"repo":"api","branch":""},{"repo":"api","branch":"main"}]' > "$T/detached.json"
check "a skipped detached worktree is warned about, not dropped in silence" \
  '(cd "$T/ws2" && "$HYDRA" apply - < "$T/detached.json" --output json |
    jq -e "(.warnings|length)==1 and (.warnings[0]|test(\"detached\"))" >/dev/null)'
check "applying the same document twice creates nothing" \
  '(cd "$T/ws2" && "$HYDRA" apply - < "$T/captured.json" --output json |
    jq -e ".data.created==0 and .data.failed==0" >/dev/null)'
check "apply carries topic membership across workspaces" \
  '(cd "$T/ws2" && "$HYDRA" topic ls --output json | jq -e "(.data.topics|length)>0" >/dev/null)'
check "a jq-filtered bare array is accepted too" \
  '(cd "$T/ws2" && jq -c "[.data.worktrees[0]]" "$T/captured.json" |
    "$HYDRA" apply - --output json | jq -e ".data.total==1" >/dev/null)'
check "apply with no stdin asks for input" \
  '{ (cd "$T/ws2" && printf "" | "$HYDRA" apply - --output json) || true; } 2>/dev/null |
   jq -e ".error.code==\"needs_input\"" >/dev/null'

# --------------------------------- 13. AX affordances (patch bundle)
echo "== 13. one envelope on stdout, capture, where, discovery =="
# The envelope now lands on STDOUT for failures too, so a caller needs one idiom.
check "a failure envelope is on stdout, not stderr" \
  '{ (cd "$T" && "$HYDRA" list --output json) || true; } 2>/dev/null |
   jq -e ".outcome==\"failure\" and .error.code==\"not_in_project\"" >/dev/null'
check "a failing command writes nothing to stderr under json" \
  '[ -z "$( { (cd "$T" && "$HYDRA" list --output json >/dev/null) || true; } 2>&1 )" ]'
# outcome must not claim partial when nothing at all succeeded.
check "run reports failure, not partial, when every worktree fails" \
  '(cd "$T/ws" && { "$HYDRA" run --repos api -- sh -c "exit 3" --output json || true; } 2>/dev/null |
    jq -e ".outcome==\"failure\"" >/dev/null)'
check "run attributes captured output to each worktree" \
  '(cd "$T/ws" && "$HYDRA" run --repos api --output json -- sh -c "echo \$HYDRA_REPO" 2>/dev/null |
    jq -e "all(.data.results[]; (.stdout|rtrimstr(\"\n\"))==.repo)" >/dev/null)'
check "run reports a child exit code without adopting it" \
  '(cd "$T/ws" && { "$HYDRA" run --repos api --output json -- sh -c "exit 42" || true; } 2>/dev/null |
    jq -e "all(.data.results[]; .exit_code==42)" >/dev/null)'
# Every command that creates a worktree says where it is.
check "apply reports the path of what it created" \
  '(cd "$T/ws" && "$HYDRA" list --output json 2>/dev/null | "$HYDRA" apply - --output json 2>/dev/null |
    jq -e "all(.data.results[]; (.path|length)>0 and (.name|length)>0)" >/dev/null)'
check "where answers inside a worktree" \
  '(cd "$T/ws/backend/api" && "$HYDRA" where --output json 2>/dev/null |
    jq -e ".data.in_project and .data.is_worktree and .data.repo==\"api\"" >/dev/null)'
check "where outside a workspace succeeds and says so" \
  '(cd "$T" && "$HYDRA" where --output json 2>/dev/null |
    jq -e ".outcome==\"success\" and .data.in_project==false" >/dev/null)'
check "an unknown subcommand is actionable, not internal" \
  '{ (cd "$T" && "$HYDRA" lst --output json) || true; } 2>/dev/null |
   jq -e ".error.code==\"unknown_command\" and (.error.details.did_you_mean|index(\"list\"))!=null
          and ([.next[].argv[1]]|index(\"commands\"))!=null" >/dev/null'
check "a selector matching nothing explains itself" \
  '(cd "$T/ws" && "$HYDRA" list --filter "branch:no-such-*" --output json 2>/dev/null |
    jq -e "(.data.worktrees|length)==0 and (.warnings|length)==1" >/dev/null)'
check "repo branches lists a remote without cloning" \
  '(cd "$T/ws" && "$HYDRA" repo branches "$T/upstream" --output json 2>/dev/null |
    jq -e "(.data.branches|index(\"main\"))!=null and .data.branches_source==\"remote\"" >/dev/null)'
check "dry-run distinguishes not-queried from none" \
  '(cd "$T/ws" && "$HYDRA" repo add "$T/upstream" --as dr --group dg --dry-run --output json 2>/dev/null |
    jq -e ".data.branches_source!=null and (.data.branches|type)==\"array\"" >/dev/null)'
check "repo restore rebuilds repositories from the manifest alone" \
  '(mkdir -p "$T/ws3/.hydra" && cp "$T/ws/.hydra/config.yaml" "$T/ws3/.hydra/config.yaml" &&
    cd "$T/ws3" && "$HYDRA" repo restore --jobs 2 --output json 2>/dev/null |
    jq -e ".outcome==\"success\" and .data.cloned>0" >/dev/null)'
check "repo restore is additive on a second run" \
  '(cd "$T/ws3" && "$HYDRA" repo restore --output json 2>/dev/null |
    jq -e ".data.cloned==0 and .data.present>0" >/dev/null)'
# Width is measured in CHARACTERS: the box-drawing rules are 3 bytes each in UTF-8, so
# byte length reports a 46-wide table as 138 and the assertion would never hold.
check "COLUMNS narrows piped output" \
  '[ "$(cd "$T/ws" && COLUMNS=48 "$HYDRA" list --output text 2>/dev/null |
        while IFS= read -r l; do printf "%s\n" "${#l}"; done | sort -rn | head -1)" -le 48 ]'
check "init reports the registry it wrote to" \
  '(mkdir -p "$T/ws4" && cd "$T/ws4" && "$HYDRA" init --project-name w4 --output json 2>/dev/null |
    jq -e "[.warnings[]|select(test(\"projects.yaml\"))]|length==1" >/dev/null)'

# ------------------------- 14. an unregistered bare repo is a failure (0.3.3)
echo "== 14. hydra cannot silently omit a repository it has on disk =="
# Reproduce the aftermath of a lost manifest write: bare and worktree on disk, no entry.
(cd "$T/ws" && "$HYDRA" repo add "$T/upstream" --as orphan --group og --branches main \
  --output json >/dev/null 2>&1) || true
ORPHAN_CFG="$T/ws/.hydra/config.yaml"
python3 - "$ORPHAN_CFG" <<'PYEOF'
import re, sys, pathlib
p = pathlib.Path(sys.argv[1])
p.write_text(re.sub(r"    og:\n(?:        .*\n|            .*\n)+", "", p.read_text()))
PYEOF
check "an unregistered bare repo fails doctor, it is not a note" \
  '(cd "$T/ws" && { "$HYDRA" doctor --output json || true; } 2>/dev/null |
    jq -e "[.data.checks[]|select(.id==\"bare_unregistered\" and .status==\"fail\")]|length>=1" >/dev/null)'
check "doctor outcome agrees with its exit status" \
  '(cd "$T/ws" && { "$HYDRA" doctor --output json || true; } 2>/dev/null |
    jq -e ".outcome==\"partial\" and .error.code==\"partial_failure\" and (.data.checks|length)>0" >/dev/null)'
check "the failure names the remote so the recovery is copyable" \
  '(cd "$T/ws" && { "$HYDRA" doctor --output json || true; } 2>/dev/null |
    jq -e "[.data.checks[]|select(.id==\"bare_unregistered\")|.message|test(\"repo add .*upstream\")]|any" >/dev/null)'
# Only THIS repository's check must clear. The shared workspace legitimately carries other
# findings by now, and asserting a globally clean doctor would couple this to every
# section above it.
check "re-running repo add converges and clears that repository's failure" \
  '(cd "$T/ws" && { "$HYDRA" repo add "$T/upstream" --as orphan --group og --branches main \
     --output json >/dev/null 2>&1 || true; } &&
    { "$HYDRA" doctor --output json || true; } 2>/dev/null |
    jq -e "[.data.checks[]|select(.id==\"bare_unregistered\" and .repo==\"orphan\")]|length==0" >/dev/null)'

# --------------------- 15. usage mistakes and zero-match scope (0.3.4)
echo "== 15. a usage mistake is recoverable, and scope survives an empty match =="
check "too many arguments quotes the usage line and points at help" \
  '(cd "$T/ws" && { "$HYDRA" path api stage --output json || true; } 2>/dev/null |
    jq -e "(.error.details.usage|length)>0 and ([.next[].argv[-1]]|index(\"--help\"))!=null" >/dev/null)'
check "an unknown flag is guided the same way" \
  '(cd "$T/ws" && { "$HYDRA" list --no-such-flag --output json || true; } 2>/dev/null |
    jq -e "(.next|length)>=1" >/dev/null)'
check "a zero-match query still reports which project it queried" \
  '(cd "$T/ws" && "$HYDRA" list --filter "branch:no-such-thing-*" --output json 2>/dev/null |
    jq -e "(.data.worktrees|length)==0 and (.data.project|length)>0 and (.data.root|length)>0" >/dev/null)'

# ------------ 16. success may never co-exist with a fault (0.3.5)
echo "== 16. the aggregate verdict is derived, not asserted =="
# A failing post_add hook: the worktree IS created, so the summary stays true — but the
# envelope must carry the failure rather than reporting a clean success and exiting 1.
mkdir -p "$T/hookws" && (cd "$T/hookws" && "$HYDRA" init --project-name hookws >/dev/null 2>&1)
(cd "$T/hookws" && "$HYDRA" repo add "$T/upstream" --as api --group g --branches main >/dev/null 2>&1)
printf 'hooks:\n    post_add:\n        - run: /bin/false\n' >> "$T/hookws/.hydra/config.yaml"
check "a failing hook cannot be reported as a clean success" \
  '(cd "$T/hookws" && { "$HYDRA" add api feat/hooked --output json || true; } 2>/dev/null |
    jq -e ".outcome!=\"success\" and .error.code==\"hook_failed\"" >/dev/null)'
check "the worktree it created is still reported" \
  '(cd "$T/hookws" && "$HYDRA" list --output json 2>/dev/null |
    jq -e "[.data.worktrees[]|select(.branch==\"feat/hooked\")]|length==1" >/dev/null)'
# A registered worktree missing from disk: status used to say "all clean" and exit 0.
rm -rf "$T/hookws/g/api-feat-hooked"
check "status cannot claim all clean while a worktree is missing" \
  '(cd "$T/hookws" && { "$HYDRA" status --output json || true; } 2>/dev/null |
    jq -e ".outcome==\"partial\" and (.summary|test(\"doctor\"))" >/dev/null)'
check "and its exit status agrees with that outcome" \
  '(cd "$T/hookws" && "$HYDRA" status --output json >/dev/null 2>&1; test $? -eq 4)'
check "the warning carries a hydra code, not raw git text alone" \
  '(cd "$T/hookws" && { "$HYDRA" status --output json || true; } 2>/dev/null |
    jq -e "[.warnings[]|test(\"^(worktree_unknown|git_failed|bare_missing):\")]|any" >/dev/null)'
check "status points at doctor without being asked" \
  '(cd "$T/hookws" && { "$HYDRA" status --output json || true; } 2>/dev/null |
    jq -e "[.next[].argv[1]]|index(\"doctor\")!=null" >/dev/null)'

# ------- 17. cached answers are dated, and rejections leave nothing behind (0.3.6)
echo "== 17. behind is qualified, the naming rule is observable, init rolls back =="
check "behind is dated, because hydra never fetches to answer a query" \
  '(cd "$T/ws" && "$HYDRA" list --output json 2>/dev/null |
    jq -e "[.data.worktrees[]|select(.upstream!=null)]|all(.upstream_as_of!=null)" >/dev/null)'
# Assert the INVARIANT, not a particular mix: by this point the shared workspace's set of
# branches is whatever earlier sections left behind.
check "every worktree states whether it is on the default branch" \
  '(cd "$T/ws" && "$HYDRA" list --output json 2>/dev/null |
    jq -e "[.data.worktrees[]|select(.on_default_branch==null)]|length==0" >/dev/null)'
check "the unsuffixed directory is exactly the default-branch worktree" \
  '(cd "$T/ws" && "$HYDRA" list --output json 2>/dev/null |
    jq -e "[.data.worktrees[]|select(.default_branch!=null and .default_branch!=\"\")]|
           all(if .on_default_branch then .name==.repo else .name!=.repo end)" >/dev/null)'
# A rejected init must leave nothing half-made for the retry to trip over.
mkdir -p "$T/collide"
{ (cd "$T/collide" && "$HYDRA" init --project-name demo --output json) || true; } \
  2>/dev/null > "$T/collide.json"
check "a name collision is rejected and names the taken registry" \
  'jq -e ".error.code==\"project_exists\" and (.next|length)>=1" "$T/collide.json" >/dev/null'
check "the rejected init left no manifest behind" \
  '! test -f "$T/collide/.hydra/config.yaml"'
check "and a retry with a free name then succeeds" \
  '(cd "$T/collide" && "$HYDRA" init --project-name collide-ok --output json 2>/dev/null |
    jq -e ".outcome==\"success\"" >/dev/null)'

# ---- 18. one broken remote must not stop the others (0.3.7)
echo "== 18. sync attempts every repo, and present means present =="
SB="$T/syncbreak"
mkdir -p "$SB/ws"
for r in alpha beta; do
  git init -q -b trunk "$SB/up-$r"
  git -C "$SB/up-$r" -c user.email=t@t -c user.name=T commit -q --allow-empty -m init
done
(cd "$SB/ws" && "$HYDRA" init --project-name syncbreak >/dev/null 2>&1)
for r in alpha beta; do
  (cd "$SB/ws" && "$HYDRA" repo add "$SB/up-$r" --as "$r" --group g --branches trunk >/dev/null 2>&1)
done
# advance both upstreams, then make one unreachable
for r in alpha beta; do
  git -C "$SB/up-$r" -c user.email=t@t -c user.name=T commit -q --allow-empty -m more
done
mv "$SB/up-beta" "$SB/up-beta-gone"
check "a broken remote does not stop the healthy ones from pulling" \
  '(cd "$SB/ws" && { "$HYDRA" sync --yes --output json || true; } 2>/dev/null |
    jq -e ".outcome==\"partial\" and (.data.summary.pulled>=1)" >/dev/null)'
check "the healthy repo really did fast-forward" \
  '[ "$(git -C "$SB/ws/g/alpha" rev-parse HEAD)" = "$(git -C "$SB/up-alpha" rev-parse HEAD)" ]'
check "the unreachable remote is reported with a hydra code" \
  '(cd "$SB/ws" && { "$HYDRA" sync --yes --output json || true; } 2>/dev/null |
    jq -e "[.warnings[]|test(\"^git_failed:\")]|any" >/dev/null)'
check "and sync exits consistently with a partial outcome" \
  '(cd "$SB/ws" && "$HYDRA" sync --yes --output json >/dev/null 2>&1; test $? -eq 4)'
# `present` names a fact about disk, so deleting the directory must change it.
check "a topic member whose directory is gone is not reported present" \
  '(cd "$T/ws" && "$HYDRA" start feat/gone --repos api --topic PRESENT-1 >/dev/null 2>&1 &&
    rm -rf "$T/ws/backend/api-feat-gone" &&
    "$HYDRA" topic show PRESENT-1 --output json 2>/dev/null |
    jq -e ".data.dangling==1 and ([.data.members[]|select(.present)]|length==0)" >/dev/null)'
check "a pruned registration only suggests a command it can complete" \
  '(cd "$T/ws" && { "$HYDRA" doctor --fix --output json || true; } 2>/dev/null |
    jq -e "[.data.checks[]|select(.fixed)|.message|test(\"hydra add [a-z].* [a-z]\")or(test(\"hydra add\")|not)]|all" >/dev/null)'

# ------------------------------------------------------------------ 19. the board is a TTY affordance, never a change to the piped contract (0.4.x)
echo
echo "== 19. bare status pipes JSON; the board is TTY-only =="

# `--output auto` has always meant "JSON when stdout is not a terminal". Bare `status` now
# opens an interactive board on a TTY, and that must NOT change what a pipe gets: every
# script calling `hydra status` depends on the envelope. An earlier cut of this refused with
# needs_input here, which turned a working invocation into exit 7.
{ "$HYDRA" status --output json >"$T/st.json" 2>/dev/null; } && st_exit=0 || st_exit=$?
check "bare status pipes an envelope, not a refusal" \
  'jq -e ".outcome==\"success\" and (.data.worktrees|type==\"array\")" "$T/st.json" >/dev/null'
check "bare status piped exits 0" '[ "'"$st_exit"'" = "0" ]'
{ "$HYDRA" status >"$T/st2.json" 2>/dev/null; } && st2_exit=0 || st2_exit=$?
check "auto output with no tty is JSON, not needs_input" \
  'jq -e ".schema==3" "$T/st2.json" >/dev/null'
check "auto output with no tty exits 0" '[ "'"$st2_exit"'" = "0" ]'

# `ui` is a hidden alias: an alias must behave exactly like its target, so it pipes too.
{ "$HYDRA" ui --output json >"$T/ui.json" 2>/dev/null; } && ui_exit=0 || ui_exit=$?
check "ui pipes the same envelope as status" \
  'jq -e ".outcome==\"success\"" "$T/ui.json" >/dev/null'
check "ui piped exits 0 like its target" '[ "'"$ui_exit"'" = "0" ]'
check "ui is published in the surface" \
  '"$HYDRA" commands --output json | jq -e ".data.commands[]|select(.name==\"ui\")" >/dev/null'

echo
# ------------------------------------------------ topic hierarchy
echo "== topic hierarchy: containment, and a gate that will not lie =="
"$HYDRA" start epic/login --repos api --topic epic-login --output json >/dev/null
"$HYDRA" start feat/social --repos api --topic feat-social --parent epic-login --from epic/login --output json >/dev/null
check "parent is recorded, not inferred"  '"$HYDRA" topic list --output json | jq -e ".data.topics[]|select(.id==\"feat-social\")|.parent==\"epic-login\""'
check "a leaf closes immediately"         '"$HYDRA" topic close feat-social --output json | jq -e ".data.closed==true"'
# Real work on the child, so the merge check has something to refuse. Until now the branches share
# a commit, and an identical branch IS trivially an ancestor.
( cd backend/api-feat-social && git -c user.email=t@t -c user.name=T commit -q --allow-empty -m work )
check "epic refuses while a child is unmerged" '{ "$HYDRA" topic close epic-login --output json || true; } | jq -e ".error.code==\"topic_not_closeable\""'
check "and names the member that is behind"    '{ "$HYDRA" topic close epic-login --output json || true; } | jq -e ".error.details.blocked_by[0].reason==\"not_merged\""'
( cd backend/api-epic-login && git -c user.email=t@t -c user.name=T merge -q --no-edit feat/social )
check "closes once the work has landed"        '"$HYDRA" topic close epic-login --output json | jq -e ".data.closed==true"'
check "reopen is available"                    '"$HYDRA" topic close epic-login --reopen --output json | jq -e ".data.closed==false"'

echo "ALL $pass ASSERTIONS PASSED"

---
title: "Topics and Execution Commands"
description: "hydra topic, start, run, and apply — topics, multi-repo worktrees, and batch execution"
ai_context: "Reference for topic management, cross-repo start, run, and apply with flags and error codes"
---

# Topics and Execution

Commands for grouping worktrees into **topics** (units of work spanning repositories), creating worktrees across repos, running commands in many worktrees, and replaying a captured worktree set from JSON.

A topic is a name for one piece of work, plus the record of which worktrees belong to it. Worktrees without a topic are normal and stay that way — not every checkout belongs to one. Membership is **recorded** in `.hydra/state.yaml` when you `start` with `--topic`, `topic attach`, or `apply` a document that includes `topic`; it is **never inferred** from a branch name. Two repositories can use different branch names for the same topic, and two topics can share a branch name without colliding.

There is no `topic create`: a topic exists because work was put in it (`hydra start --topic` or `hydra topic attach`). It disappears when its last member is detached.

### Topics relate as a graph

Work does not form a tree, so relationships are **typed, directed edges** rather than one parent. Two kinds carry meaning to hydra:

| kind | meaning | what `topic close` derives |
|------|---------|----------------------------|
| `part_of` | containment — this work integrates into that | every topic inside must be closed, **and** each of their member branches must have reached this topic's branch in the same repository, asked of git on every call |
| `depends_on` | a peer that must land first | the target must be **closed**. Peers share no integration branch, so merged-ness is not checkable and is not pretended |

Any other kind must be **dot-namespaced** (`acme.tested-by`). hydra stores and reports those and gates on nothing in them, so a plugin, a UI or a script can build its own semantics on the same primitive; bare words stay reserved for kinds hydra may define later. A topic may declare more than one `part_of` — each parent gates its own close independently.

`meta` is free-form key/value data hydra never interprets, so an extension keeps its state on the topic that owns it instead of in a sidecar that drifts.

Relationships are stored once, on the topic that declares them. `linked_from` in `topic show` is **derived** on read — a second copy of an edge would be a second thing that can be wrong. Deleting a topic sweeps every edge naming it in the same write, so the CLI cannot leave a dangling one; `hydra doctor` reports the hand-edited case as `topic_dangling_link` and `--fix` drops it.

```bash
hydra topic link feat-social part_of epic-login        # containment
hydra topic link feat-social depends_on feat-tokens    # a peer that lands first
hydra topic link feat-social acme.tested-by qa-suite   # yours; hydra never gates on it
hydra topic unlink feat-social depends_on feat-tokens

hydra topic update feat-social --meta acme.pbi=2072958 --unset-meta stale.key
printf 'meta:\n  acme.pbi: "2072958"\n' | hydra topic update feat-social -   # JSON or YAML
```

A relationship that would close a loop is refused (`topic_cycle`, with the loop in `details.path`); `--force` records it anyway, and every walk carries a visited set so a recorded cycle costs mutual close-blocking rather than a hang. A self-edge is refused in **every** kind: mutuality can be meaningful, being one's own relatum cannot.

A document REPLACES whole sections, which is what makes a checked-in file the source of truth rather than a patch with invented merge rules: `links:` or `meta:` present replaces that section, absent leaves it alone, explicitly empty clears it, and a document declaring neither converges at exit 0 saying so. Flags and a document cannot be combined.

---

## hydra topic

Inspect and manage topics — recorded membership across repositories.

### Description

`hydra topic` groups the worktrees that belong to one piece of work. A worktree belongs to at most one topic, and belonging to none is a normal, permanent state.

`hydra topic attach` promotes existing ad-hoc work: attach an unlabeled worktree to a topic with no migration step. `hydra start --topic` creates worktrees and records membership in one pass.

### Usage

```bash
hydra topic <subcommand> [args] [flags]
```

### Subcommands

| Subcommand | Alias | Purpose |
|------------|-------|---------|
| `list` | `ls` | List active topics with member counts |
| `show` | `view` | Show one topic's members, joined to worktrees on disk |
| `attach` | | Record that a worktree belongs to a topic |
| `detach` | | Drop membership without touching the worktree |
| `close` | | Declare the work finished, if its children and dependencies are in |
| `link` | | Record a relationship to another topic |
| `unlink` | | Remove a recorded relationship |
| `update` | | Set metadata and relationships, from flags or a document |
| `remove` | `rm` | Detach every member, optionally removing worktrees |

### Examples

```bash
hydra topic list
hydra topic show 2072958
hydra topic attach 2072958 backend/api-login
hydra topic detach 2072958 backend/api-login
hydra topic remove 2072958 --with-worktrees --yes
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_unknown` | 1 | `show`, `detach`, or `remove` with an id that is not recorded; `details.known` lists valid ids |
| `topic_conflict` | 1 | `attach` when the worktree already belongs to a different topic |
| `topic_not_closeable` | 1 | `close` when a child or a `depends_on` target is unfinished; `details.blocked_by` names every reason at once, and `--force` closes anyway |
| `topic_cycle` | 1 | `link` when the relationship would close a loop in `part_of`/`depends_on`, or points a topic at itself in any kind; `details.path` names the loop, and `--force` records it anyway |
| `link_unknown` | 1 | `unlink` naming a relationship that is not recorded; `details.recorded` lists the ones that are |
| `worktree_unknown` | 1 | `attach` or `detach` when the worktree handle does not resolve |
| `worktree_name_conflict` | 1 | worktree handle matches more than one worktree |
| `worktree_dirty` | 5 | `remove --with-worktrees` when a target has uncommitted changes and `--force` was not passed |
| `partial_failure` | 4 | `remove --with-worktrees` when some worktrees failed to remove |
| `git_failed` | 1 | underlying git error during `remove --with-worktrees` |
| `needs_input` | 7 | required argument missing (for example `<id>` on `show`) |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held by another hydra process; retry |

---

## hydra topic list

List active topics with their member counts.

### Description

Lists every topic that has at least one recorded member. Empty topics are garbage-collected when their last member is detached, so this list reflects only active topics.

### Usage

```bash
hydra topic list [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |

### Arguments

None.

### Examples

```bash
hydra topic list
hydra topic list --output json
```

### Error codes

| code | exit | when |
|------|------|------|
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held; retry |

---

## hydra topic show

Show one topic and its members, joined to worktrees on disk.

### Description

`hydra topic show` reads **recorded** membership and joins each `(repo, branch)` pair to the worktree on disk, if any. Members with no worktree are reported as missing (dangling). It does not infer topics from branch names.

### Usage

```bash
hydra topic show <id> [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Recorded topic id |

### Examples

```bash
hydra topic show 2072958
hydra topic show 2072958 --output json
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_unknown` | 1 | id not recorded; `details.known` lists valid ids |
| `needs_input` | 7 | `<id>` missing |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held; retry |

---

## hydra topic attach

Record that a worktree belongs to a topic.

### Description

`hydra topic attach` records explicit membership for an existing worktree. It is one of two commands that may **create** a topic (the other is `hydra start --topic`). Attaching promotes ad-hoc, unlabeled work with no migration.

The command is **convergent**: attaching a worktree that already belongs to the same topic is a no-op that exits 0. A worktree already held by a **different** topic returns `topic_conflict`.

Membership is keyed by `(repo, branch)`. A detached HEAD has no branch to record; attach refuses with `internal`.

### Usage

```bash
hydra topic attach <id> <worktree> [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Topic id to record (created if it does not exist) |
| `worktree` | Yes | Worktree handle (`api-stage`, `backend/api-stage`, etc.) |

### Examples

```bash
# Promote an existing unlabeled worktree into a topic
hydra topic attach 2072958 backend/api-login

# Safe to re-run — already attached is a no-op
hydra topic attach 2072958 backend/api-login
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_conflict` | 1 | worktree already belongs to a different topic |
| `worktree_unknown` | 1 | handle does not resolve to exactly one worktree |
| `worktree_name_conflict` | 1 | handle matches more than one worktree |
| `needs_input` | 7 | `<id>` missing |
| `internal` | 1 | worktree is detached (no branch to record) |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held; retry |

---

## hydra topic detach

Drop a worktree's topic membership without touching the worktree.

### Description

`hydra topic detach` removes the recorded `(repo, branch)` membership. The worktree directory and branch are unchanged. When the last member is detached, the topic is garbage-collected.

The worktree handle is resolved against **this topic's members only**, so a handle that is not a member fails with a clear error rather than a confusing cross-topic mismatch.

### Usage

```bash
hydra topic detach <id> <worktree> [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--help` | `-h` | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Recorded topic id |
| `worktree` | Yes | Member worktree handle |

### Examples

```bash
hydra topic detach 2072958 backend/api-login
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_unknown` | 1 | id not recorded; `details.known` lists valid ids |
| `worktree_unknown` | 1 | handle does not match a member of this topic |
| `worktree_name_conflict` | 1 | handle matches more than one worktree |
| `needs_input` | 7 | `<id>` missing |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held; retry |

---

## hydra topic remove

Detach every member from a topic, optionally removing their worktrees.

### Description

`hydra topic remove` drops all membership for a topic. With `--with-worktrees`, it also removes each member's worktree from disk (same semantics as `hydra remove`). Without `--with-worktrees`, only membership is cleared.

Destructive removal requires confirmation in a TTY unless `--yes` is passed. A non-interactive invocation without `--yes` refuses rather than implying consent.

### Usage

```bash
hydra topic remove <id> [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--with-worktrees` | | `false` | Also remove each member's worktree from disk |
| `--delete-branch` | | `false` | With `--with-worktrees`, delete each branch when merged |
| `--force` | `-f` | `false` | Proceed when a target has uncommitted changes |
| `--yes` | | `false` | Skip confirmation prompts |
| `--dry-run` | | `false` | Report what would happen and change nothing |
| `--help` | `-h` | — | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `id` | Yes | Recorded topic id |

### Examples

```bash
# Drop membership only; worktrees stay on disk
hydra topic remove 2072958 --yes

# Remove every member worktree as well
hydra topic remove 2072958 --with-worktrees --yes

# Preview without changing anything
hydra topic remove 2072958 --with-worktrees --dry-run
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_unknown` | 1 | id not recorded; `details.known` lists valid ids |
| `worktree_dirty` | 5 | uncommitted changes on a target and no `--force` |
| `partial_failure` | 4 | some worktrees failed to remove |
| `git_failed` | 1 | underlying git error |
| `needs_input` | 7 | `<id>` missing |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state lock held; retry |

---

## hydra start

Create a worktree per repository for one unit of work.

### Description

`hydra start` is `hydra add` across several repositories at once, with optional topic recording. Without `--topic`, worktrees are created unassigned — the same state a single `hydra add` leaves them in.

The command is **convergent**: a worktree that already exists for the requested branch is reported as skipped, not as a failure. Re-running the same command to confirm it landed is safe and exits 0.

**Branch name** (highest precedence first):

1. Positional `<branch>` — a **literal** name; branch patterns never run on it
2. `--branch <name>`
3. Unanimous branch of `--topic`'s existing members (extend a topic without extra flags)
4. `repos.<alias>.branch_provider` / `defaults.branch_provider`
5. `repos.<alias>.branch_pattern` / `defaults.branch_pattern`

With none of these available, `start` returns `needs_input` naming `--branch` — it never guesses a branch name.

**Which repositories:** `--repos`, `--group`, or `--all`. With `--topic` and no selector, the topic's existing members define the repo set. With neither selector nor topic members, `start` returns `needs_input` — it never silently targets every repository.

Pass `--no-assign` to create worktrees without recording topic membership.

### Usage

```bash
hydra start [<branch>] [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--repos` | | Only these repositories (repeatable, comma-separated) |
| `--group` | | Only worktrees in this group |
| `--all` | | Target every registered repository |
| `--topic` | | Record membership in this topic (created if needed) |
| `--branch` | | Branch name (a positional branch wins) |
| `--slug` | | Value for `{slug}` in `branch_pattern` |
| `--kind` | | Value for `{kind}` in `branch_pattern` |
| `--user` | | Override `{user}`; defaults to git `user.name` |
| `--from` | | Base ref for a brand-new branch |
| `--filter` | | Narrow by state: `dirty`, `behind`, or `branch:<glob>` (repeatable) |
| `--no-assign` | | Create worktrees without recording topic membership |
| `--dry-run` | | Report what would happen and change nothing |
| `--help` | `-h` | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `branch` | No | Literal branch name (see precedence above) |

### Examples

#### One branch across repos, recorded as a topic

```bash
hydra start marcus/feat-login --repos api,web --topic 2072958
```

#### Extend a topic to another repo (reuses members' branch)

```bash
hydra start --topic 2072958 --repos billing
```

#### Generate branch name from `branch_pattern`

```bash
hydra start --topic 2072958 --slug login --kind feat --repos api
```

#### Single repo, no topic (like `hydra add`)

```bash
hydra start feat/spike --repos api
```

#### Convergent re-run

```bash
hydra start marcus/feat-login --repos api,web --topic 2072958
# exits 0 with skipped worktrees if already present
```

### Error codes

| code | exit | when |
|------|------|------|
| `repo_unknown` | 1 | `--repos` or `--group` names something not registered; `details.known` lists valid values |
| `branch_unknown` | 1 | base ref or branch name does not resolve |
| `branch_provider_failed` | 1 | configured `branch_provider` failed or timed out |
| `worktree_exists` | 1 | directory held by a **different** branch (not convergence) |
| `worktree_name_conflict` | 1 | derived directory name taken by another branch |
| `topic_conflict` | 1 | topic membership could not be recorded (worktree belongs to another topic) |
| `git_failed` | 1 | underlying git error |
| `partial_failure` | 4 | some repositories failed |
| `needs_input` | 7 | missing `--repos`/`--group`/`--all`, or branch name; `details.missing` / `details.one_of` name flags |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `busy` | 6 | state or git lock held; retry |

---

## hydra run

Run one command across selected worktrees.

### Description

Everything after `--` is the command, passed as an **argv array to exec**. There is **no implicit shell**: hydra never wraps the command in `sh -c`. Redirection (`>`, `|`), globs (`*`), and variable expansion (`$VAR`) are **literal arguments**, not shell syntax. To use shell features, invoke a shell explicitly:

```bash
hydra run --topic 2072958 -- sh -c 'go build ./... && go test ./...'
```

Each invocation runs with its working directory set to the worktree and these environment variables (same context hooks receive):

| Variable | Value |
|----------|-------|
| `HYDRA_TOPIC` | Topic id, empty when the worktree is unassigned |
| `HYDRA_REPO` | Repository alias |
| `HYDRA_GROUP` | Group name |
| `HYDRA_BRANCH` | Branch name, empty when detached |
| `HYDRA_PATH` | Absolute path to the worktree |

A bare worktree handle runs in exactly that worktree. A handle matching several worktrees is an error (`worktree_name_conflict`), never a silent first match. Selectors (`--topic`, `--repos`, `--group`, `--filter`) intersect the same way as `list` and `status`.

### Usage

```bash
hydra run [<worktree>] [selector] -- <command> [args…] [flags]
```

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--topic` | | | Only worktrees recorded in this topic |
| `--repos` | | | Only these repositories (repeatable, comma-separated) |
| `--group` | | | Only worktrees in this group |
| `--filter` | | | Narrow by state: `dirty`, `behind`, or `branch:<glob>` (repeatable) |
| `--jobs` | `-j` | `0` | Max worktrees to run in parallel (`0` = one per repository, capped) |
| `--timeout` | | `0` | Per-invocation timeout, e.g. `30s` or `5m` (`0` = no limit) |
| `--keep-going` | | `false` | Report every failure instead of stopping at the first |
| `--help` | `-h` | — | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `worktree` | No | Single worktree handle (mutually exclusive with using only selectors) |
| `command` | Yes* | Everything after `--` |

\*Required after `--`. Omitting a command returns `needs_input`.

### Examples

#### One worktree

```bash
hydra run api-stage -- go build ./...
```

#### Every worktree in a topic

```bash
hydra run --topic 2072958 -- go test ./...
```

#### Parallel run with keep-going

```bash
hydra run --group backend --jobs 4 --keep-going -- make lint
```

#### No shell — metacharacters are literal

```bash
hydra run --repos api -- echo '>$VAR|*'
# prints the string literally; no redirection, glob, or expansion
```

#### Shell features, requested explicitly

```bash
hydra run --repos api -- sh -c 'git log --oneline -1 | cat'
```

#### Environment variables in a script

```bash
hydra run --topic 2072958 -- sh -c 'echo "$HYDRA_REPO@$HYDRA_BRANCH (topic=$HYDRA_TOPIC)"'
```

### Error codes

| code | exit | when |
|------|------|------|
| `worktree_unknown` | 1 | handle not found, or selector matched no worktrees |
| `worktree_name_conflict` | 1 | handle matches more than one worktree |
| `topic_unknown` | 1 | `--topic` names an id that is not recorded |
| `repo_unknown` | 1 | `--repos` or `--group` names something not registered |
| `git_failed` | 1 | command failed in every worktree |
| `partial_failure` | 4 | command failed in some worktrees; `details.failed` lists them |
| `needs_input` | 7 | no command after `--` |
| `not_in_project` | 2 | no `.hydra/config.yaml` |

---

## hydra apply

Create the worktrees described by JSON on stdin.

### Description

`hydra apply` is the batch counterpart to `hydra start`. It reads a desired worktree set on stdin and **converges** the workspace to match — creating worktrees and recording topic membership where the document asks for it.

It accepts either shape:

```json
{"data": {"worktrees": [{"repo": "api", "branch": "feat/login", "topic": "2072958"}]}}
```

```json
[{"repo": "api", "branch": "feat/login", "topic": "2072958"}]
```

Only `repo`, `branch`, and `topic` are read. Fields `list` also emits (`path`, `ahead`, `behind`, `dirty`, …) are observed state, not desired state — replaying them cannot change git tracking.

The command is **convergent**: a worktree that already exists is skipped. Applying the same document twice is a no-op that exits 0.

**Repositories:** apply creates worktrees but **never creates repositories**. The target workspace must already have each `repo` alias registered in `.hydra/config.yaml` (via `hydra repo add` or project bootstrap). Unregistered repos appear as failed items in a `partial_failure`.

**Detached worktrees:** a worktree on a detached HEAD has no branch, so it cannot be described in the document. Such entries are **skipped** with a **warning** (not dropped silently), because a round-trip from `list` must not fail when the source workspace happened to contain a detached checkout.

### Usage

```bash
hydra apply - [flags]
```

The `-` argument is required: it signals that JSON is read from stdin (like `kubectl apply -f -`).

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--dry-run` | | `false` | Report what would happen and change nothing |
| `--help` | `-h` | — | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `-` | Yes | Read JSON from stdin |

### Examples

#### Capture and replay in the same workspace

```bash
hydra list --output json > work.json
hydra apply - < work.json
```

#### Move a topic's worktrees into another workspace

Workspace A (source):

```bash
hydra list --topic 2072958 --output json > topic-work.json
```

Workspace B (target — repos must already be registered):

```bash
hydra apply - < topic-work.json
```

#### Filter with jq, then apply

```bash
hydra list --output json \
  | jq '[.data.worktrees[] | select(.topic=="2072958")]' \
  | hydra apply -
```

#### Dry run

```bash
hydra apply - --dry-run < work.json
```

#### Convergent re-apply

```bash
hydra apply - < work.json
hydra apply - < work.json   # exits 0; all items skipped
```

### Detached worktrees in the document

If `list` output includes worktrees with an empty `branch` (detached HEAD), `apply` warns and skips them:

```text
N detached worktree(s) skipped: a branchless worktree cannot be described by a branch
```

### Error codes

| code | exit | when |
|------|------|------|
| `topic_conflict` | 1 | document assigns a worktree to a topic, but it belongs to another |
| `git_failed` | 1 | underlying git error creating a worktree |
| `partial_failure` | 4 | some items failed (including unregistered `repo` aliases) |
| `needs_input` | 7 | stdin empty or described no worktrees |
| `not_in_project` | 2 | no `.hydra/config.yaml` |
| `internal` | 1 | stdin is not valid JSON in the expected shape |

---

## Related commands

| Task | Command |
|------|---------|
| List worktrees (optionally by topic) | `hydra list --topic <id>` |
| Per-worktree git state | `hydra status --topic <id>` |
| Single-repo worktree | `hydra add <repo> <branch>` |
| Remove one worktree | `hydra remove <repo> <branch>` |
| Locate path (scripts) | `hydra path <worktree>` |
| Run lifecycle hooks | `hydra hooks run <event>` |

---

## See Also

- [Worktree Management](./worktree-management.md) — `add` and `remove`
- [Commands index](./README.md) — navigation, sync, doctor, hooks
- [Configuration](../configuration.md) — groups, hooks, branch patterns
- [skills/hydra/SKILL.md](../../skills/hydra/SKILL.md) — agent contract

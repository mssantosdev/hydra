---
name: hydra
description: Manage hydra workspaces and git worktrees with the `hydra` CLI. Use when creating or inspecting a hydra project, adding/removing/locating worktrees, grouping work into topics across repositories, running a command in many worktrees, syncing branches, diagnosing a broken workspace, or scripting any `hydra` invocation.
---

# hydra

## Invariants

These hold for every command. Rely on them instead of probing.

1. `--output json` (default when stdout is not a terminal) emits one envelope. Never scrape text.
2. Branch on `error.code`, never on message wording. Codes are stable; messages are not.
3. **Creation is convergent**: `add`, `start`, `apply`, `repo add`, `repo restore` twice is a no-op at exit 0, reported as `skipped`. Removing what is already gone is `worktree_unknown`/`topic_unknown`, never a silent success.
4. Nothing is inferred from a branch name. Topic membership is recorded, never guessed.
5. Never blocks on a prompt: a missing value is `needs_input`, a wrong one `usage`, an aborted one `cancelled`.
6. A handle matching several worktrees is an error, never a silent first match.
7. `hydra commands --output json` publishes the whole surface and the code→exit table. Human guide: https://mssantosdev.github.io/hydra/

## Envelope

```json
{"schema":3,"command":"list","outcome":"success","summary":"2 worktree(s)","data":{},"warnings":[]}
{"schema":3,"command":"add","outcome":"failure","error":{"severity":"error","code":"worktree_dirty","retryable":false,"message":"…","subject":"worktree:backend/api-stage","details":{}}}
```

ONE envelope, on stdout, success or failure. `outcome` is `success`, `partial` or `failure`; a **partial**
carries both `data` and `error` — the work that landed is real. `next[]` is argv plus a reason, never acted
on. No `exit` field: the process status carries it.

`error`, each `warnings[]` entry, and a failed item's `error` in `data` are ONE shape: `severity`
(`error`|`warning`|`note`), `code`, `message`, `details`, `retryable`, plus optional `subject`
(`worktree:…`, `repo:…`, `<manifest>:<line>`) and `cause` (the tool's own words verbatim).
`error`/`warning` force at least `partial`; a `note` never does, and only a note may omit `code`.

## Decisions

**Which command creates worktrees?** One repo one branch → `add`; several repos one unit of work →
`start --topic <id> --repos a,b` (records the topic); a captured set → `list -o json | … | apply -`.

**Which worktrees does it act on?** A bare handle (`api-stage`, `backend/api-stage`) → exactly one;
`--topic <id>` → members; `--repos`/`--group`/`--all` → those repos; `--filter dirty|behind|branch:<glob>` narrows further. Combine freely; they intersect. Aliases: `ls`, `rm`, `view`, `cd`.

**Rebuilding a workspace elsewhere?** `repo restore` creates the `branches:` a manifest declares
(additive, `--jobs N`); `repo set <alias> --branches a,b` declares them later; without a declaration only
default branches are restored. `apply -` replays a captured set. `sync` needs `--dirty stash|reset|skip`.

**Workspace looks wrong?** `doctor --output json` first; `checks[].fixable` says whether `doctor --fix` repairs it. **`manifest_untrusted`?** Read `hydra hooks ls`, then `hydra trust` — in CI, `--accept sha256:…` pinned in CI config, never in the repo.

## Commands

| command | purpose | key flags |
|---|---|---|
| `init` | create `.hydra/config.yaml` here | `--project-name` |
| `new` | bootstrap a project and its first repo | `--local`, `--remote-url` |
| `repo` | `add` (`--adopt`), `set`, `list`, `remove`, `branches <url\|alias>`, `restore` | `--branches`, `--as`, `--group` (add), `--jobs` (restore) |
| `add` | one worktree for one branch | `--as`, `--from` |
| `start` | one branch across repositories; records a topic | `--repos`, `--topic`, `--slug`, `--kind` |
| `apply` | create the worktrees described by JSON on stdin | `-`, `--dry-run` |
| `remove` | delete a worktree | `--delete-branch`, `--force`, `--yes` |
| `topic` | `list`, `show`, `attach`, `detach`, `close`, `remove` | `--reopen`, `--with-worktrees`, `--yes` |
| `list` | list worktrees | `--topic`, `--repos`, `--group`, `--filter`, `--against` |
| `status` | bare on TTY: full-screen board; agents: `--output json` | `--topic`, `--filter`, `--against`, `--all` |
| `path` | print one worktree's absolute path | `--topic` |
| `where` | where hydra thinks it is; works outside a workspace | — |
| `switch` | change directory to a worktree (TTY) | `--cd` |
| `run` | run a command per worktree; argv after `--` | `--topic`, `--jobs`, `--timeout` |
| `sync` | fast-forward worktrees from upstream | `--dirty`, `--yes` |
| `doctor` | diagnose, and repair what is fixable | `--fix`, `--all` |
| `prune` | drop stale registrations and empty groups | `--dry-run` |
| `project` | global registry: `list`, `add`, `rm` | `--prune` |
| `hooks` | `ls`, `run <event>` | `--worktree` |
| `config` | `show`, `set theme\|editor <value>` | — |
| `trust` | approve this manifest to execute hooks/providers | `--show`, `--revoke`, `--accept` |
| `commands` | the whole surface, and the error table | — |
| `skill` | emit this skill | `--install` |
| `init-shell` | install the helper `switch` needs | `--install` |
| `completion` | shell completion script | — |

## Error codes

| code | exit | raised when |
|---|---|---|
| `not_in_project` | 2 | no `.hydra/config.yaml` walking up, no `--project` |
| `config_invalid` | 2 | a manifest value hydra refuses; see `details.line` |
| `config_version_unsupported` | 2 | `version` is not `"3"` or `"2"` (v2 upgrades on write) |
| `state_version_unsupported` | 2 | `state.yaml` written by a newer hydra |
| `project_unknown` | 2 | `--project` names nothing in the registry |
| `usage` | 2 | bad flag value, bad input document, or exclusive flags; see `details.valid` |
| `manifest_untrusted` | 2 | the manifest can execute, unapproved; `hooks ls` then `trust` |
| `repo_unknown` | 1 | alias or group not registered; see `details.known` |
| `bare_missing` | 1 | `.bare/<alias>.git` is gone; run `doctor` |
| `branch_unknown` | 1 | a base ref or branch name does not resolve |
| `worktree_exists` | 1 | that branch already has a worktree |
| `worktree_unknown` | 1 | no worktree by that name |
| `worktree_name_conflict` | 1 | a name matches no one worktree |
| `topic_unknown` | 1 | id not recorded; `details.known` lists valid ids |
| `topic_conflict` | 1 | that worktree already belongs to another topic |
| `topic_not_closeable` | 1 | a child is open or unmerged; see `details.blocked_by` |
| `branch_provider_failed` | 1 | the `branch_provider` script failed or timed out |
| `hook_failed` | 1 | a non-`optional` hook failed; the worktree IS created — fix it, then `hooks run <event>` |
| `git_failed` | 1 | a git invocation failed; `cause` has git's words |
| `project_exists` | 1 | that project name is already registered |
| `unknown_command` | 1 | no such subcommand; see `details.did_you_mean` |
| `io_failed` | 1 | the machine: a path hydra cannot create, write or read |
| `cancelled` | 130 | you stopped a prompt; nothing changed |
| `internal` | 1 | a broken hydra invariant — report it |
| `shell_helper_missing` | 3 | `switch --cd` with no shell helper |
| `partial_failure` | 4 | some succeeded, some failed — read `data` AND `error` |
| `worktree_dirty` | 5 | uncommitted changes block a destructive op; commit or `--force` |
| `busy` | 6 | a lock was held — retry with backoff, the ONLY retryable code |
| `needs_input` | 7 | a value is missing; add the flag in `details.missing`, never prompt |

## Anti-patterns

- Rebuilding `<group>/<repo>-<branch>` instead of reading `data[].path` — `--as` may have overridden it. Treating `upstream: null` as a failure; it is a branch with no upstream yet.
- Deleting after a failure. Re-running `add` is safe, reports `skipped`, and does NOT re-run the hook.
- Retrying `remove --delete-branch --force` after `git_failed`: the branch is NOT merged. Ask first.
- Assuming `hydra run` gets a shell. It does not — pass `-- sh -c '…'` when you need one.

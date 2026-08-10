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
5. A missing value is `needs_input` (exit 7) naming it in `details.missing`/`details.one_of` — hydra
   never blocks on a prompt when output is machine-readable.
6. A handle matching several worktrees is an error, never a silent first match.
7. `hydra commands --output json` publishes the whole surface and the code→exit table.

## Envelope

```json
{"schema":3,"command":"list","outcome":"success","summary":"2 worktree(s)","data":{},"warnings":[]}
{"schema":3,"command":"add","outcome":"failure","error":{"code":"worktree_dirty","retryable":false,"message":"…","details":{}}}
```

ONE envelope, on stdout, success or failure. `outcome` is `success`, `partial` or `failure`; a
**partial** carries both `data` and `error` — the work that landed is real. `next[]` is argv plus a
reason, never acted on. No `exit` field: the process status carries it.

## Decisions

**Which command creates worktrees?** One repo, one branch → `add`. Several repos, one unit of work →
`start --topic <id> --repos a,b`, which also records the topic. A captured set →
`list -o json | … | apply -`. `start` is `add` across repositories; `apply -` is its batch form,
consuming exactly what `list` emits. Aliases: `ls`, `rm`, `view`, `cd`.

**Which worktrees does this act on?** A bare handle (`api-stage`, `backend/api-stage`) → exactly one.
`--topic <id>` → recorded members. `--repos`/`--group`/`--all` → those repositories.
`--filter dirty|behind|branch:<glob>` → narrow further. Combine freely; they intersect.

**`topic_unknown`?** Never recorded. `details.known` lists real ids; `topic attach` records one. Never guess from a branch name. **`topic_not_closeable`?** `details.blocked_by` names every open or unmerged child at once. **`busy`?** A lock was held — retry with backoff, the ONLY retryable code.

**`worktree_dirty`?** Uncommitted work is in the way. For `sync`, choose `--dirty stash|reset|skip`. For `remove`, commit or pass `--force`. Never `--force` blindly.

**Rebuilding a workspace elsewhere?** A manifest declaring `branches:` per repo is enough on its own — `repo restore` creates that set (additive, `--jobs N`), and `repo set <alias> --branches a,b` declares it later.
Without a declaration only default branches are restored, and `apply -` replays a captured set. Repos and worktrees stay separate.

**Workspace looks wrong?** `doctor --output json` first. `checks[].fixable` says whether
`doctor --fix` can repair it.

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
| `commands` | the whole surface, and the error table | — |
| `skill` | emit this skill | `--install` |
| `init-shell` | install the helper `switch` needs | `--install` |
| `completion` | shell completion script | — |
| `ui` | hidden alias of `status` | — |

## Error codes

| code | exit | raised when |
|---|---|---|
| `not_in_project` | 2 | no `.hydra/config.yaml` walking up, and no `--project` |
| `config_version_unsupported` | 2 | manifest `version` is neither `"3"` nor `"2"` (v2 is upgraded on write) |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` written by a newer hydra |
| `project_unknown` | 2 | `--project` names nothing in the registry |
| `repo_unknown` | 1 | repo alias or group not registered; `details.known` lists them |
| `bare_missing` | 1 | `.bare/<alias>.git` is gone; run `doctor` |
| `branch_unknown` | 1 | a base ref or branch name does not resolve |
| `worktree_exists` | 1 | that branch already has a worktree |
| `worktree_unknown` | 1 | no worktree by that name |
| `worktree_name_conflict` | 1 | a name does not identify exactly one worktree |
| `worktree_dirty` | 5 | destructive op blocked by uncommitted changes |
| `topic_unknown` | 1 | id not recorded; `details.known` lists valid ids |
| `topic_not_closeable` | 1 | a child is open or unmerged; `details.blocked_by` names every reason |
| `topic_conflict` | 1 | that worktree already belongs to another topic |
| `branch_provider_failed` | 1 | configured `branch_provider` failed or timed out |
| `hook_failed` | 1 | a non-`optional` hook exited non-zero |
| `shell_helper_missing` | 3 | `switch --cd` with no shell helper |
| `partial_failure` | 4 | some items succeeded, some failed |
| `busy` | 6 | a lock was held — **the only retryable code** |
| `needs_input` | 7 | a value is missing; `details.missing`/`one_of` name it |
| `git_failed` | 1 | an underlying git invocation failed |
| `project_exists` | 1 | that project name is already registered (the opposite of `project_unknown`) |
| `unknown_command` | 1 | no such subcommand; `details.did_you_mean`/`available` list real ones |
| `internal` | 1 | anything unclassified, including a bad flag value |

## Anti-patterns

- Rebuilding `<group>/<repo>-<branch>` instead of reading `data[].path` — `--as` may have overridden it.
- Treating `upstream: null` as a failure; it is a branch with no upstream yet.
- Retrying anything but `busy`, or retrying `needs_input` without adding the flag. Never delete after a failure: a `hook_failed` worktree was created correctly. Re-running `add` is safe but reports `skipped` and does NOT re-run the hook — fix the hook, then `hooks run post_add --worktree <name>`.
- Retrying `remove --delete-branch --force` after `git_failed`: the branch is NOT merged. Ask first.
- Passing `--force` to escape `worktree_dirty` without checking what is uncommitted.
- Assuming `hydra run` gets a shell. It does not — pass `-- sh -c '…'` when you need one.

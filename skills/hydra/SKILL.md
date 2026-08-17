---
name: hydra
description: Manage hydra workspaces and git worktrees with the `hydra` CLI. Use for creating or inspecting a hydra project, adding/removing/locating worktrees, relating work as topics across repositories, running commands in many worktrees, syncing, diagnosing a broken workspace, or scripting `hydra`.
---

# hydra

## Invariants

These hold for every command. Rely on them instead of probing.

1. `--output json|yaml` (json when stdout is not a terminal) emits one envelope. Never scrape text.
2. Branch on `error.code`, never message wording. Codes are stable, messages are not.
3. **Creation is convergent**: `add`, `start`, `apply`, `repo add`, `repo restore`, `topic link` twice is a no-op at exit 0 (`skipped`/`recorded:false`). Removing what is gone errors, never silent success.
4. Nothing is inferred from a branch name. Membership and links are recorded, never guessed.
5. Never blocks: missing → `needs_input`, wrong → `usage`, aborted → `cancelled`. An ambiguous handle errors, never a silent first match.
6. `hydra commands --output json` publishes the surface and the code→exit table. Guide: https://mssantosdev.github.io/hydra/

## Envelope

```json
{"schema":3,"command":"list","outcome":"success","summary":"2 worktree(s)","data":{},"warnings":[]}
{"schema":3,"command":"add","outcome":"failure","error":{"code":"worktree_dirty","message":"…","subject":"worktree:backend/api-stage"}}
```

ONE envelope, on stdout, success or failure. `outcome` is `success`, `partial` or `failure`; a **partial** carries
both `data` and `error` — the work that landed is real. `next[]` is argv plus a reason. No `exit`: the process status carries it.

`error`, each `warnings[]` entry, and a failed item's `error` in `data` are ONE shape: `severity`
(`error`|`warning`|`note`), `code`, `message`, `details`, `retryable`, optional `subject` and `cause` (the tool's
own words). `error`/`warning` force `partial`; a `note` never does — an accepted override reports notes.

## Decisions

**Creating worktrees?** One repo one branch → `add`; several repos one unit of work →
`start --topic <id> --repos a,b`; a captured set → `list -o json | … | apply -`.

**Topics relate as a graph.** `topic link <id> part_of|depends_on <target>` — both gate `close` — or a
dot-namespaced kind of your own (`acme.tested-by`), stored, never gated on. `topic update` takes `--meta k=v`, or a JSON/YAML document on `-`/a path replacing `links:`/`meta:`.

**Which worktrees?** A bare handle (`api-stage`, `backend/api-stage`) → one; `--topic <id>` → members;
`--repos`/`--group`/`--all` → those repos; `--filter dirty|behind|branch:<glob>` narrows; they intersect. Aliases: `ls`, `rm`, `view`, `cd`.

**Rebuilding elsewhere?** `repo restore` creates the `branches:` a manifest declares (additive, `--jobs N`);
`repo set <alias> --branches a,b` declares them later; undeclared → default branches only. `sync` needs `--dirty stash|reset|skip`.

**Workspace wrong?** `doctor --output json`; `checks[].fixable` says whether `--fix` repairs it. **`manifest_untrusted`?** `hydra hooks ls`, then `hydra trust` — in CI `--accept sha256:…`, pinned in CI config, never the repo.

## Commands

| command | purpose | key flags |
|---|---|---|
| `init` | create `.hydra/config.yaml` here | `--project-name` |
| `new` | bootstrap a project and its first repo | `--local`, `--remote-url` |
| `repo` | `add` (`--adopt`), `set`, `list`, `remove`, `branches`, `restore` | `--branches`, `--as`, `--group`, `--jobs` |
| `add` | one worktree for one branch | `--as`, `--from` |
| `start` | one branch across repos; records a topic | `--repos`, `--topic`, `--slug`, `--kind` |
| `apply` | create the worktrees a document describes | `-`, `--dry-run` |
| `remove` | delete a worktree | `--delete-branch`, `--force`, `--yes` |
| `topic` | `list`, `show`, `attach`, `detach`, `close`, `remove`, `link`, `unlink`, `update` | `--force`, `--reopen`, `--meta`, `--with-worktrees` |
| `list` | list worktrees | `--topic`, `--repos`, `--group`, `--filter`, `--against` |
| `status` | bare on TTY: the board; agents: `--output json` | `--topic`, `--filter`, `--against` |
| `path` | print one worktree's absolute path | `--topic` |
| `where` | where hydra thinks it is; safe outside a workspace | — |
| `switch` | change directory to a worktree (TTY) | `--cd` |
| `run` | run a command per worktree; argv after `--` | `--topic`, `--jobs`, `--timeout` |
| `sync` | fast-forward worktrees from upstream | `--dirty`, `--yes` |
| `doctor` | diagnose, repair what is fixable | `--fix`, `--all` |
| `prune` | drop stale registrations, empty groups | `--dry-run` |
| `project` | global registry: `list`, `add`, `rm` | `--prune` |
| `hooks` | `ls`, `run <event>` | `--worktree` |
| `config` | `show`, `set theme\|editor <value>` | — |
| `trust` | approve this manifest to execute hooks | `--show`, `--revoke`, `--accept` |
| `commands` | the whole surface, and the error table | — |
| `skill` | emit this skill | `--install` |
| `init-shell` | install the helper `switch` needs | `--install` |
| `completion` | shell completion script | — |

## Error codes

| code | exit | raised when |
|---|---|---|
| `not_in_project` | 2 | no `.hydra/config.yaml` walking up, no `--project` |
| `config_invalid` | 2 | a manifest value hydra refuses; see `details.line` |
| `config_version_unsupported` | 2 | manifest `version` is not `"3"`/`"2"` (v2 upgrades on write) |
| `state_version_unsupported` | 2 | `state.yaml` written by a newer hydra |
| `project_unknown` | 2 | `--project` names nothing in the registry |
| `usage` | 2 | bad flag value, bad document, or exclusive flags; see `details.valid` |
| `manifest_untrusted` | 2 | manifest can execute, unapproved; `hooks ls` then `trust` |
| `repo_unknown` | 1 | alias or group not registered; see `details.known` |
| `bare_missing` | 1 | `.bare/<alias>.git` gone; run `doctor` |
| `branch_unknown` | 1 | a base ref or branch name does not resolve |
| `worktree_exists` | 1 | that branch already has a worktree |
| `worktree_unknown` | 1 | no worktree by that name |
| `worktree_name_conflict` | 1 | a name matches no single worktree |
| `topic_unknown` | 1 | id not recorded; `details.known` lists real ids |
| `topic_conflict` | 1 | that worktree belongs to another topic |
| `topic_not_closeable` | 1 | child/dependency unfinished; `details.blocked_by`, override `--force` |
| `topic_cycle` | 1 | link closes a loop or self-points; `details.path`, override `--force` |
| `link_unknown` | 1 | no such link; `details.recorded` lists the real ones |
| `branch_provider_failed` | 1 | the `branch_provider` script failed or timed out |
| `hook_failed` | 1 | a non-`optional` hook failed; the worktree IS created — then `hooks run <event>` |
| `git_failed` | 1 | a git invocation failed; `cause` has git's words |
| `project_exists` | 1 | that project name is already registered |
| `unknown_command` | 1 | no such subcommand; see `details.did_you_mean` |
| `io_failed` | 1 | the machine: a path hydra cannot create, write or read |
| `carry_refused` | 2 | a `carry` source resolves outside the workspace, or is absent |
| `cancelled` | 130 | you stopped a prompt; nothing changed |
| `internal` | 1 | a broken hydra invariant — report it |
| `shell_helper_missing` | 3 | `switch --cd` with no shell helper |
| `partial_failure` | 4 | some succeeded, some failed — read `data` AND `error` |
| `worktree_dirty` | 5 | uncommitted changes block a destructive op; commit or `--force` |
| `busy` | 6 | a lock was held — retry with backoff, the ONLY retryable code |
| `needs_input` | 7 | a value is missing; add the flag in `details.missing`, never prompt |

## Anti-patterns

- Rebuilding `<group>/<repo>-<branch>` instead of `data[].path`; treating `upstream: null` as failure. Deleting after a failure: re-running `add` is safe, reports `skipped`, does NOT re-run the hook. Retrying `remove --delete-branch --force` after `git_failed`: the branch is NOT merged — ask.
- Assuming `hydra run` gets a shell. It does not — pass `-- sh -c '…'`.
- Treating a refusal as final: `topic close`/`link` refusals name an override in `next[]`.

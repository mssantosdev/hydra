---
name: hydra
description: Manage hydra workspaces and git worktrees with the `hydra` CLI. Use when creating or inspecting a hydra project, adding/removing/locating worktrees, syncing branches, diagnosing a broken workspace, or scripting any `hydra` invocation.
---

# hydra

## Model

Four levels: **project** → **group** → **repo** → **worktree**. A project is a workspace root holding
`.hydra/config.yaml`, registered by name in a global registry. Bare repos hold git data only;
every worktree is a real sibling directory under its group.

```
<project-root>/
  .hydra/config.yaml     # shared manifest; .hydra/ also holds local state
  .bare/api.git/          # git data ONLY — never cd into or write under .bare/
  backend/
    api/                  # worktree for the repo's default branch
    api-feat-login/       # worktree for branch feat/login  (alias + "-" + slug)
```

The map key under a group **is** the repo alias, and it is the single source of both the bare path
(`.bare/<alias>.git`) and the worktree directory base name.

## Rules for agents

- Always pass `--output json` and parse the envelope. Never scrape text output.
- Branch on `error.code`, never on message wording. Codes are stable; messages are not.
- Use `hydra path <worktree>` to locate a worktree; `hydra switch` is for interactive
  shells. `path` prints a bare path even when captured, so `cd "$(hydra path api)"`
  works; add `--output json` when you want its group/repo/branch too.
- Read a worktree's location from `data[].path`. Never reconstruct it from a branch name.
- Never `cd` into or write under `.bare/`.
- Never call `git worktree add` directly in a hydra workspace: it bypasses upstream setup and hooks.
- Pass `--yes` to skip confirmations and expect a prompt-free run. `--no-hooks` skips every hook.
- Run `hydra doctor --output json` first whenever anything looks structurally wrong.
- `upstream: null` is a valid **local-only** state (a branch not yet pushed), not an error.

## Commands

| command | purpose | key flag |
|---|---|---|
| `init` | create `.hydra/config.yaml` in the current directory | `--project-name` |
| `new` | bootstrap a new project and its first repo | `--group` |
| `clone` | clone a remote into a project, one worktree per branch | `--branches` |
| `adopt` | import an existing checkout into the current project | `--group` |
| `add` | create a worktree for a branch | `--as`, `--from` |
| `remove` | delete a worktree | `--delete-branch`, `--force` |
| `path` | print a worktree's absolute path | — |
| `switch` | change directory to a worktree (interactive shells) | `--cd` |
| `list` | list worktrees (alias `ls`) | `--all` |
| `status` | per-worktree tracking and dirtiness summary | `--all` |
| `sync` | fast-forward worktrees from their upstreams | `--force`, `--yes` |
| `doctor` | diagnose workspace/upstream problems | `--fix`, `--all` |
| `prune` | drop stale worktree registrations and empty groups | `--dry-run` |
| `project` | manage the global project registry (`ls`/`add`/`rm`) | `--prune` |
| `hooks` | inspect or run configured hooks (`ls`/`run <event>`) | `--worktree` |
| `config` | edit global settings (theme, editor) | — |
| `init-shell` | install the shell helper that powers `switch` | `--install` |
| `completion` | emit a shell completion script | — |
| `glossary` | explain hydra's vocabulary | — |
| `skill` | emit this skill | `--install` |

Global flags: `--output auto|text|json`, `--project <name>`, `--config <path>`, `--verbose`,
`--no-hooks`. `HYDRA_OUTPUT` sets the default mode; `NO_COLOR` disables color.

## Contract

Success, on stdout:

```json
{"schema":1,"command":"list","data":{},"warnings":[]}
```

Failure, on stderr:

```json
{"schema":1,"command":"add","error":{"code":"worktree_name_conflict","message":"…","details":{}}}
```

`--output auto` (the default) emits JSON whenever stdout is not a terminal.

| code | exit | raised when |
|---|---|---|
| `not_in_project` | 2 | no `.hydra/config.yaml` found walking up, and no `--project` |
| `config_version_unsupported` | 2 | manifest `version` is not `"2"` |
| `project_unknown` | 2 | `--project <name>` not in the registry |
| `repo_unknown` | 1 | alias not present in any group |
| `bare_missing` | 1 | `<bare_dir>/<alias>.git` absent |
| `branch_unknown` | 1 | branch does not exist where an existing branch was required |
| `worktree_exists` | 1 | target worktree already exists for that branch |
| `worktree_unknown` | 1 | named worktree not found |
| `worktree_name_conflict` | 1 | derived directory name taken by a different branch |
| `worktree_dirty` | 5 | destructive op blocked by uncommitted changes |
| `hook_failed` | 1 | a non-`optional` hook exited non-zero |
| `shell_helper_missing` | 3 | `switch --cd` with no shell helper installed |
| `partial_failure` | 4 | some items succeeded, some failed |
| `git_failed` | 1 | an underlying git invocation failed |
| `topic_unknown` | 1 | `--topic <id>` is not recorded; `details.known` lists valid ids |
| `topic_conflict` | 1 | that worktree already belongs to another topic |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` was written by a newer hydra |
| `branch_provider_failed` | 1 | a configured `branch_provider` failed or timed out |
| `busy` | 6 | a git or state lock was held — **the only retryable code** |
| `needs_input` | 7 | a value is missing and output is machine-readable; `details.missing` names the flag |
| `internal` | 1 | anything unclassified |

## Anti-patterns

- Building `<group>/<repo>-<branch>` yourself instead of reading `data[].path` — `--as` may have
  overridden the directory name, and `/` in a branch becomes `-`.
- Treating `upstream: null` as a failure; it is a branch with no upstream yet.
- Assuming exit 1 for every failure: `not_in_project` 2, `shell_helper_missing` 3, `partial_failure` 4, `worktree_dirty` 5, `busy` 6 (retry this one), `needs_input` 7.
- Calling `hydra switch` from a script to find a path; use `hydra path`.
- Reacting to a `hook_failed` from `add` by deleting the worktree — the worktree was created
  correctly. Fix the hook and run `hydra hooks run post_add`.
- Retrying `remove --delete-branch` with `--force` after a `git_failed` refusal: the refusal means
  the branch is NOT merged into the default branch, and nothing was removed. Confirm with the user.
- Deleting `.bare/<alias>.git` after an interrupted clone; re-run the same `hydra clone` and it
  completes. `hydra doctor` reports the half-built state as `bare_unregistered`.

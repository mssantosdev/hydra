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
- Use `hydra path <worktree>` to locate a worktree (`switch` is for interactive shells). It prints a
  bare path even when captured, so `cd "$(hydra path api)"` works; read `data[].path` from any
  envelope rather than reconstructing a path from a branch name.
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
| `add` | create one worktree for a branch | `--as`, `--from` |
| `start` | one branch across many repos, convergent; records a topic | `--repos`, `--topic`, `--slug` |
| `remove` | delete a worktree | `--delete-branch`, `--force` |
| `path` | print a worktree's absolute path | — |
| `switch` | change directory to a worktree (interactive shells) | `--cd` |
| `list` | list worktrees (alias `ls`) | `--topic`, `--repos`, `--group`, `--filter`, `--against` |
| `status` | tracking, dirtiness, and merged-ness vs any ref | `--topic`, `--filter`, `--against REF` |
| `topic` | `list`/`show`/`attach`/`detach`/`remove` units of work spanning repos | `--with-worktrees`, `--yes` |
| `run` | run one command per worktree; argv after `--`, never a shell | `--topic`, `--jobs`, `--timeout` |
| `sync` | fast-forward worktrees from their upstreams | `--dirty stash\|reset\|skip`, `--yes` |
| `doctor` | diagnose workspace/upstream problems | `--fix`, `--all` |
| `prune` | drop stale worktree registrations and empty groups | `--dry-run` |
| `project` | manage the global project registry (`ls`/`add`/`rm`) | `--prune` |
| `hooks` | inspect or run configured hooks (`ls`/`run <event>`) | `--worktree` |
| `config` | `show`, or `set theme\|editor <value>` without a prompt | — |
| `init-shell` | install the shell helper that powers `switch` | `--install` |
| `completion` | emit a shell completion script | — |
| `skill` | emit this skill | `--install` |

Global flags: `--output auto|text|json`, `--project <name>`, `--config <path>`, `--verbose`,
`--no-hooks`. `HYDRA_OUTPUT` sets the default mode; `NO_COLOR` disables color.

## Contract

Success, on stdout. `outcome` is `success` or `partial`; `summary` is the one-line answer;
`next` is present only when there is a suggestion, and hydra never acts on it:

```json
{"schema":2,"command":"list","outcome":"success","summary":"2 worktree(s)","data":{},"warnings":[]}
```

Failure, on stderr. `retryable` is the one fact you cannot derive — only `busy` is `true`:

```json
{"schema":2,"command":"add","outcome":"failure","error":{"code":"worktree_name_conflict","retryable":false,"message":"…","details":{}}}
```

`--output auto` (the default) emits JSON whenever stdout is not a terminal. There is no `exit` field:
the process exit status carries it.

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
- Branching on the exit status instead of `error.code` and `error.retryable`; only `busy` is retryable.
- Calling `hydra switch` from a script to find a path; use `hydra path`.
- Deleting the worktree after `hook_failed` from `add` — it was created correctly; fix the hook and run `hydra hooks run post_add`.
- Retrying `remove --delete-branch --force` after a `git_failed` refusal: the branch is NOT merged and nothing was removed. Ask the user.
- Deleting `.bare/<alias>.git` after an interrupted clone; re-run the same `hydra clone` — it is convergent and completes.

---
title: "Hydra Commands Reference"
description: "Complete reference for all Hydra commands"
ai_context: "Command index with decision trees and quick reference tables"
---

# Commands Reference

Complete reference for all Hydra commands. Global flags on every command:

| Flag | Description |
|------|-------------|
| `--output auto\|text\|json` | Output mode (`HYDRA_OUTPUT` env override; `auto` → JSON when stdout is not a TTY) |
| `--project <name>` | Target a registered project by name |
| `--config <path>` | Path to `.hydra/config.yaml` |
| `--verbose` | Verbose logging |
| `--no-hooks` | Skip all configured hooks |

`--yes` is **not** global: `remove`, `sync`, `topic`, `repo remove` and `init-shell` each define it.

## Quick Command Table

| Task | Command | Details |
|------|---------|---------|
| Initialize project | `hydra init` | [Project Bootstrap](./project-bootstrap.md#hydra-init) |
| Bootstrap new project | `hydra new` | [Project Bootstrap](./project-bootstrap.md#hydra-new) |
| Clone repository | `hydra repo add <url>` | [Project Bootstrap](./project-bootstrap.md#hydra-clone) |
| Adopt existing checkout | `hydra repo add --adopt` | [Project Bootstrap](./project-bootstrap.md#hydra-adopt) |
| Add worktree | `hydra add [<repo> <branch>]` | [Worktree Management](./worktree-management.md#hydra-add) |
| Start a unit of work | `hydra start <branch> --repos a,b --topic <id>` | [Topics and execution](./topics-and-execution.md#hydra-start) |
| Inspect a unit of work | `hydra topic ls\|show\|attach\|detach\|remove` | [Topics and execution](./topics-and-execution.md#hydra-topic) |
| Run a command per worktree | `hydra run --topic <id> -- <argv>` | [Topics and execution](./topics-and-execution.md#hydra-run) |
| Recreate from JSON | `hydra list -o json \| hydra apply -` | [Topics and execution](./topics-and-execution.md#hydra-apply) |
| Remove worktree | `hydra remove [<repo> <branch>]` | [Worktree Management](./worktree-management.md#hydra-remove) |
| Print worktree path | `hydra path <worktree>` | [Navigation](#hydra-path) |
| Switch worktree | `hydra switch [<worktree>]` | [Navigation](#hydra-switch) |
| List worktrees | `hydra list` / `hydra ls` | [Navigation](#hydra-list) |
| Worktree status / interactive board | `hydra status` (`ui`/`tui` alias) | [Navigation](#hydra-status) |
| Sync updates | `hydra sync [<alias>]` | [Sync](#hydra-sync) |
| Diagnose workspace | `hydra doctor` | [Maintenance](#hydra-doctor) |
| Prune stale entries | `hydra prune` | [Maintenance](#hydra-prune) |
| Project registry | `hydra project ls\|add\|rm` | [Project registry](#hydra-project) |
| Lifecycle hooks | `hydra hooks ls\|run <event>` | [Hooks](#hydra-hooks) |
| Global settings | `hydra config` | [Configuration & shell](#hydra-config) |
| Shell integration | `hydra init-shell` | [Configuration & shell](#hydra-init-shell) |
| Shell completion | `hydra completion <shell>` | Built-in help |
| Agent skill | `hydra skill [--install]` | [Agents](#hydra-skill) |
| Command surface | `hydra commands` | [Command surface](#hydra-commands) |

Version details appear in `hydra`, `hydra --help`, and `hydra --version`.

## Project Bootstrap

See [Project Bootstrap](./project-bootstrap.md) for `init`, `new`, `repo add`, and `repo add --adopt`.

## Worktree Management

See [Worktree Management](./worktree-management.md) for `add`, `remove`, and `status`.

## Topics and execution

See [Topics and execution](./topics-and-execution.md) for `topic`, `start`, `run`, and `apply` — the
commands that treat a unit of work spanning several repositories as one thing.

## Navigation

### hydra path

Print the absolute path of a worktree. **Preferred for scripts and agents.**

```bash
hydra path api-feature-x
hydra path api-feature-x --output json
```

| code | exit |
|------|------|
| `worktree_unknown` | 1 |

### hydra switch

Print a worktree path (exit 0). With the shell helper installed, also emits a directive so your shell `cd`s automatically.

```bash
hydra switch api-feature-x          # print path; auto-cd when helper active
hydra switch api-feature-x --cd     # require auto-cd (exit 3 without helper)
```

Without the shell helper, `switch` still succeeds and prints the path. Only `--cd` fails with `shell_helper_missing` (exit 3).

Install the helper with `hydra init-shell` (see below). Do not use `switch` in scripts — use `path`.

### hydra list

List worktrees in the current project.

```bash
hydra list
hydra ls
hydra list --all                  # every registered project
hydra list --output json
hydra list --topic 2072958        # only one unit of work
hydra list --against release      # add a merged-ness column vs any ref
```

Alias: `ls`.

### hydra status

Per-worktree summary: branch, upstream tracking, dirty/clean state. On a TTY with default output,
opens a full-screen interactive board (same route as the deprecated `ui`/`tui` commands).

```bash
hydra status                        # interactive board on a TTY
hydra status --output text
hydra status --output json          # agents and scripts
hydra status --all
hydra status --topic 2072958
hydra status --against main         # ahead/behind/merged vs ref
```

Without a TTY and no selector-narrowing flags, returns `needs_input` (exit 7). The board prints
navigation hints on stderr; use `cd "$(hydra status)"` only with `--output text` and a selector.
See [Worktree Management](./worktree-management.md#hydra-status) for keys and layout.

### Selectors

`list`, `status`, `path`, `sync` and `run` share one selector surface. The flags **intersect**, so
each one you add narrows the set further:

| Flag | Selects |
|------|---------|
| `--topic <id>` | the worktrees recorded as members of that unit of work |
| `--repos a,b` | those repository aliases |
| `--group <name>` | every repository in that group |
| `--all` | every registered project, not just the current one |
| `--filter dirty` | worktrees with uncommitted changes |
| `--filter behind` | worktrees behind their upstream |
| `--filter branch:<glob>` | worktrees whose branch matches the glob |

`--against REF` is not a selector: it adds a column answering "is this branch merged into REF
yet?" without changing which worktrees are shown. The relationship is computed from git at query
time, so it can never go stale.

## hydra sync

Fast-forward worktrees from their upstream remotes.

```bash
hydra sync              # interactive selection
hydra sync api          # one repo alias
hydra sync --yes        # skip prompts
hydra sync --force      # pull dirty worktrees
```

Runs `post_sync` hooks after successful updates. Partial failures return `partial_failure` (exit 4).

## Maintenance

### hydra doctor

Diagnose structural problems: missing bare repos, broken worktree registrations, upstream issues.

```bash
hydra doctor
hydra doctor --fix      # attempt repairs
hydra doctor --all      # all registered projects
hydra doctor --output json
```

Run this first when anything looks wrong. Each check reports a stable `id` plus
`ok` / `warn` / `fail`; `--fix` repairs only the checks marked fixable.

| `id` | Detects | Fixable |
|---|---|---|
| `missing_fetch_refspec` | `remote.origin.fetch` absent, so no remote-tracking refs exist | yes |
| `missing_origin_head` | `refs/remotes/origin/HEAD` unset | yes |
| `branch_no_upstream` | branch exists on origin but the worktree has no upstream | yes |
| `bare_unregistered` | a bare repo on disk that `.hydra/config.yaml` does not claim, typically left by an interrupted clone | no — re-run the clone to finish it, or delete the directory |
| `worktree_inside_gitdir` | worktree path under `<bare_dir>/` (legacy layout) | no — re-create the worktree |
| `legacy_symlink` | a symlink in a group dir pointing into `<bare_dir>` | yes (deleted) |
| `worktree_missing_on_disk` | registered in git but the directory is gone | yes |
| `worktree_unregistered` | a directory in a group dir that is not a registered worktree | no |
| `stale_git_state` | an in-progress rebase/merge/cherry-pick | no — reported only, never touched |
| `worktree_detached` | worktree is on a detached HEAD | no |
| `worktree_dirty` | uncommitted changes | no |
| `registry_dangling` | a registry entry whose root has no `.hydra/config.yaml` | yes |

A repository with no `origin` at all (created by `hydra new` without a remote) is a
valid local-only state, not damage: the two remote checks report `ok` for it.

`doctor` exits 0 when nothing failed and 4 (`partial_failure`) when something did,
after emitting the full report — so the report is always machine-readable.

### hydra prune

Remove stale worktree registrations and empty group directories.

```bash
hydra prune
hydra prune --dry-run
```

## hydra project

Manage the global project registry at `<config-dir>/projects.yaml` (see [Configuration](../configuration.md)).

```bash
hydra project ls
hydra project add <name> <path>
hydra project rm <name>
hydra project ls --prune    # drop entries whose roots no longer exist
```

## hydra hooks

Inspect or manually run hook chains from `.hydra/config.yaml`.

```bash
hydra hooks ls
hydra hooks run post_add --worktree api-feature-x
```

Events (in order): `post_clone`, `post_add`, `pre_remove`, `post_remove`, `post_sync`.

Each `run` entry executes via `sh -c` with `HYDRA_*` environment variables injected. A failing required hook returns `hook_failed` (exit 1) but does **not** roll back completed work.

## Configuration & shell

### hydra config

Interactive editor for global user settings (theme, editor) stored under `~/.config/hydra/` (platform-specific).

### hydra init-shell

Install shell integration for automatic `cd` on `hydra switch`.

```bash
# Default: writes loader block to shell rc AND prints success message
hydra init-shell

# Print raw loader snippet only (safe to redirect)
hydra init-shell --install=false >> ~/.bashrc
```

**Do not** run `hydra init-shell >> ~/.bashrc` — the default mode already writes your rc file and stdout contains human-readable prose.

Supported shells: `bash`, `zsh`, `fish`.

Optional flags: `--with-completion`, `--without-completion`.

## hydra skill

Emit the embedded agent skill contract.

```bash
hydra skill                 # print to stdout
hydra skill --install       # install for agent tooling
```

Source of truth: [skills/hydra/SKILL.md](../../skills/hydra/SKILL.md).

## hydra commands

Publish the whole command surface — every command, its local flags, and the complete
error-code→exit-status table — as one machine-readable document.

```bash
hydra commands --output json    # the full surface, for tooling
hydra commands --output text    # the same, human-readable
```

This is the discovery entry point for an agent: it needs no workspace, and it removes any need to
scrape `--help`. [`SURFACE.txt`](../../SURFACE.txt) in the repo root is a committed snapshot of the
text form, so any change to the surface shows up as a reviewable diff rather than silently.

## Decision Tree

```
Starting new project?
├── Guided setup → hydra new
├── Existing directory → hydra init
├── Remote URL → hydra repo add <url>
└── Existing checkout → hydra repo add --adopt

Need a worktree?
├── hydra add <repo> <branch>
└── Specific base → hydra add <repo> <branch> --from=<base>

Need a path (scripts/agents)?
└── hydra path <worktree>

Need to switch (interactive shell)?
└── hydra switch <worktree>

Need status or list?
├── hydra list [--all]
└── hydra status [--all]

Need updates?
└── hydra sync [--yes] [--force]

Something broken?
└── hydra doctor [--fix] [--output json]

Need cleanup?
├── hydra remove <repo> <branch> [--delete-branch]
└── hydra prune [--dry-run]
```

## Error Codes and Exit Codes

| code | exit | raised when |
|------|------|-------------|
| `not_in_project` | 2 | no `.hydra/config.yaml` found walking up, and no `--project` |
| `config_version_unsupported` | 2 | `.hydra/config.yaml` `version` is not `"3"` or `"2"` (v2 loads and upgrades on write) |
| `config_invalid` | 2 | a manifest `path:` or `bare_dir` that leaves the workspace |
| `project_unknown` | 2 | `--project <name>` not in the registry |
| `repo_unknown` | 1 | alias not present in any group |
| `bare_missing` | 1 | `<bare_dir>/<alias>.git` absent |
| `branch_unknown` | 1 | branch does not exist where required |
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
| `topic_not_closeable` | 1 | a child is open or unmerged; `details.blocked_by` names every reason |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` was written by a newer hydra |
| `branch_provider_failed` | 1 | a configured `branch_provider` failed or timed out |
| `project_exists` | 1 | that project name is already registered |
| `unknown_command` | 1 | no such subcommand; `details.did_you_mean` lists real ones |
| `usage` | 2 | a bad flag value, or flags that exclude each other |
| `manifest_untrusted` | 2 | the manifest can execute and is unapproved; run `hydra trust` |
| `busy` | 6 | a git or state lock was held — **the only retryable code** |
| `needs_input` | 7 | a value is missing and output is machine-readable; `details.missing` names it |
| `internal` | 1 | anything unclassified |

In JSON mode, branch on `error.code` — never on message text.

## See Also

- [Configuration](../configuration.md) — `.hydra/config.yaml` schema v3 (v2 loads and upgrades on write)
- [skills/hydra/SKILL.md](../../skills/hydra/SKILL.md) — Agent contract
- [Worktree Management](./worktree-management.md) — `add` / `remove` details
- [Project Bootstrap](./project-bootstrap.md) — `new` / `init` / `repo add` / `repo add --adopt`

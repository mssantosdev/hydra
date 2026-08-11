---
title: "Worktree Management Commands"
description: "hydra add, remove, and status — create, delete, and inspect Git worktrees"
ai_context: "Reference for add, remove, and status with flags, layout, and error codes"
---

# Worktree Management

Commands for creating and deleting Git worktrees.

---

## hydra add

Create a new worktree for a repository branch.

### Description

`hydra add` creates a Git worktree — a separate working directory linked to a branch. Worktrees are **real directories** under `<group>/`, never symlinks.

When you run `hydra add`:

1. Resolves the repo alias from `.hydra/config.yaml`
2. Derives the directory name (`<alias>` for the default branch, `<alias>-<slug>` otherwise, or `--as` override)
3. Creates the worktree at `<group>/<dirName>/`
4. Bare git data stays in `.bare/<alias>.git` only
5. Runs `post_add` hooks (failures do not roll back the worktree)

Interactive mode separates checking out an existing branch from creating a new one. For new branches, the base ref resolves in order: `--from` → `defaults.base_branch` → repo `default_branch` → `origin/HEAD`.

### Usage

```bash
hydra add [<repo-alias> <branch-name>] [flags]
```

### Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--from` | `-f` | Base branch or ref when creating a new branch |
| `--as` | | Directory name under the group (overrides derived name) |
| `--yes` | `-y` | Skip confirmation prompts |
| `--help` | `-h` | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `repo-alias` | No* | Alias from `.hydra/config.yaml` |
| `branch-name` | No* | Branch to check out or create |

\*Both required together for direct mode. Omit both for interactive mode.

### Examples

#### Interactive mode

```bash
hydra add
```

#### Direct mode

```bash
hydra add api feature-x
```

```bash
hydra add api feature-y --from=develop
```

```bash
hydra add api hotfix/critical-bug --from=prod
```

```bash
hydra add api feature/long-name --as short-name
```

### Directory naming

| Branch | Alias | Worktree path |
|--------|-------|---------------|
| `main` (default) | `api` | `backend/api/` |
| `feature/login` | `api` | `backend/api-feature-login/` |
| `hotfix/urgent` | `api` | `backend/api-hotfix-urgent/` |

Slugs preserve case; `/` becomes `-`. Use `hydra path <worktree>` or JSON `data[].path` for the authoritative path.

### When the worktree already exists

Re-running the same `add` is a no-op: exit 0, `disposition: "skipped"`, and the existing worktree
in `data`. Creation is convergent, so a provisioning script may re-run its own steps.

`worktree_exists` (exit 1) means the request cannot be satisfied — the branch is checked out at a
DIFFERENT directory from the one `--as <name>` asks for. Git cannot check one branch out twice, so
the requested directory is unreachable.

### Error codes

| code | exit | when |
|------|------|------|
| `repo_unknown` | 1 | alias not in config |
| `bare_missing` | 1 | `.bare/<alias>.git` missing |
| `branch_unknown` | 1 | `--from` or base branch invalid |
| `worktree_exists` | 1 | branch is checked out at another directory; a re-run of the same `add` exits 0 |
| `worktree_name_conflict` | 1 | directory name taken by another branch |
| `hook_failed` | 1 | required `post_add` hook failed |
| `git_failed` | 1 | underlying git error |
| `not_in_project` | 2 | no `.hydra/config.yaml` |

---

## hydra status

Worktree summary and the default interactive route on a TTY. `ui` and `tui` are deprecated aliases
of `status`.

### Two routes

| Route | When | Output |
|-------|------|--------|
| Interactive board | `hydra status` on a TTY with default `--output auto` | Full-screen UI |
| Machine or text | `--output text` or `--output json` | One row per worktree |

Agents and scripts should use `hydra status --output json`. Without a TTY and without
selector-narrowing flags (`--topic`, `--repos`, `--group`, `--all`, `--filter`), hydra returns
`needs_input` (exit 7).

### Interactive board

- Rows load from the same selectors as `list` and `status --output json`.
- Aggregate counts (total, dirty, behind, and similar) are derived from the loaded rows inside the
  browser — not passed in as a separate summary field.
- With `--against <ref>`, each row can show ahead/behind/merged vs that ref.
- With `--all`, rows are grouped under per-project headers (`Project` on each row).
- Navigation hints print on **stderr** so stdout stays clean for `cd "$(hydra status)"` when using
  `--output text` with a selector.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Move selection |
| `enter` | Confirm selection (print path on stdout in text mode) |
| `y` | Copy selection to clipboard |
| `/` | Filter rows |
| `d` | Toggle dirty-only filter |
| `r` | Refresh rows |
| `q` | Quit |

### Usage

```bash
hydra status
hydra status --output json
hydra status --all --against main
hydra status --topic 2072958 --filter dirty
```

### Flags

| Flag | Description |
|------|-------------|
| `--against` | Compare each branch to a ref (ahead/behind/merged) |
| `--all` | Every registered project |
| `--filter` | Narrow rows (`dirty`, `behind`, `branch:<glob>`) |
| `--group` | Repositories in a group |
| `--repos` | Comma-separated repo aliases |
| `--topic` | Worktrees recorded for a unit of work |

### Error codes

Same selector and project errors as `list`. Non-TTY interactive invocation without narrowing flags
returns `needs_input` (exit 7).

## hydra remove

Remove a worktree for a repository branch.

### Description

`hydra remove` deletes a worktree and optionally the Git branch. Runs `pre_remove` before deletion and `post_remove` after (with project root as cwd).

### Usage

```bash
hydra remove [<repo-alias> <branch-name>] [flags]
```

### Aliases

- `remove`
- `rm`

### Flags

| Flag | Short | Default | Description |
|------|-------|---------|-------------|
| `--force` | `-f` | `false` | Remove despite uncommitted changes |
| `--delete-branch` | `-d` | `false` | Delete the Git branch too |
| `--yes` | `-y` | `false` | Skip confirmation prompts |
| `--help` | `-h` | — | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `repo-alias` | No* | Alias from `.hydra/config.yaml` |
| `branch-name` | No* | Branch whose worktree to remove |

### Examples

```bash
hydra remove api old-feature
hydra remove api temp-feature --force --yes
hydra remove api merged-feature --delete-branch
```

### Dirty worktrees

Without `--force`, uncommitted changes block removal with `worktree_dirty` (exit 5). Commit, stash, or pass `--force`.

### Deleting the branch

`--delete-branch` is atomic with the worktree removal: hydra decides whether the
branch is safe to delete **before** touching anything, so you never end up with the
worktree gone and an orphaned branch left behind.

A branch is deletable without `--force` only when its work is already merged into
the repo's **default branch** (`origin/<default_branch>`). Being present on origin
is *not* enough — that only proves the branch was pushed, not that it landed — so a
live feature branch fully mirrored on the remote is still refused:

```console
$ hydra remove api feature --yes --delete-branch
Error: branch "feature" is not merged into origin/main; nothing was removed.
       Re-run with --force to delete both the worktree and the branch, or drop
       --delete-branch to keep the branch
```

The refusal carries `code: git_failed`, `details.merge_target`, and
`details.worktree_removed: false`. `--force` deletes both unconditionally.

hydra also refuses to delete the repo's default branch outright.

### Error codes

| code | exit | when |
|------|------|------|
| `worktree_unknown` | 1 | worktree not found |
| `worktree_dirty` | 5 | uncommitted changes and no `--force` |
| `hook_failed` | 1 | required hook failed |
| `git_failed` | 1 | underlying git error |
| `not_in_project` | 2 | no `.hydra/config.yaml` |

---

## Related commands

| Task | Command |
|------|---------|
| Locate path (scripts) | `hydra path <worktree>` |
| Switch (interactive) | `hydra switch <worktree>` |
| Worktree status / board | `hydra status` (`ui`/`tui` alias) |
| List worktrees | `hydra list` / `hydra ls` |
| Retry failed hook | `hydra hooks run post_add --worktree <name>` |

---

## See Also

- [Commands index](./README.md) — navigation, sync, doctor, hooks
- [Configuration](../configuration.md) — groups, hooks, naming rules
- [skills/hydra/SKILL.md](../../skills/hydra/SKILL.md) — agent contract

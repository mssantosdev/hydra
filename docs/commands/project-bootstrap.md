---
title: "Project Bootstrap"
description: "hydra init, new, clone, adopt — create and register Hydra projects"
ai_context: "Reference for project creation and first-repository bootstrap"
---

# Project Bootstrap

Commands that create a Hydra workspace, register it globally, and add the first repositories.

## hydra init

Initialize Hydra in the current directory.

### Description

Creates `.hydra/config.yaml` (schema v2) and `.bare/` in the current directory, then registers the project in the global registry (`<config-dir>/projects.yaml`).

### Usage

```bash
hydra init
hydra init --project-name my-project
```

### Creates

```text
./.hydra/config.yaml
./.bare/
```

Add repositories with `hydra clone`, `hydra new`, or by editing `.hydra/config.yaml` and running `hydra doctor`.

---

## hydra new

Create a new Hydra project and bootstrap the first repository.

### Description

Interactive flow that creates a project root, `.hydra/config.yaml`, `.bare/`, and the first worktree. Modes:

1. **Create local repo** — initialize a new Git repository
2. **Clone remote repo** — clone from a URL

### Usage

```bash
hydra new
```

### Interactive flow

1. Enter project path (relative to current directory; may contain `/`)
2. Choose local or remote mode
3. Enter group, alias, and initial branch
4. Local mode: choose the source directory name for the initial clone target
5. Remote mode: enter remote URL
6. Hydra creates the project and prints next steps

### Path rules

- Project path may contain `/` but must be relative (no `..`)
- Group and alias are names, not paths (no `/`)

### Example

```bash
$ hydra new

Project Path: client-a/api-platform
First Repository: Create local repo
Group: backend
Alias: api
Initial Branch: main
Local Repository Directory: api-repo
```

On disk (schema v2):

```text
./client-a/api-platform/.hydra/config.yaml
./client-a/api-platform/.bare/api.git/
./client-a/api-platform/backend/api/    # real worktree, not a symlink
```

### Notes

- Local bootstrap creates an initial commit
- Remote bootstrap uses the same clone machinery as `hydra clone`
- After creation: `cd <project>` and `hydra list`

---

## hydra clone

Clone a remote repository into a Hydra project and create worktrees.

### Usage

```bash
hydra clone <url>
hydra clone git@github.com:org/my-api.git --group backend --alias api
hydra clone <url> --branches main,develop
```

### Flags

| Flag | Description |
|------|-------------|
| `--group` | Group name for the repository |
| `--alias` | Repo alias (map key in `.hydra/config.yaml`) |
| `--branches` | Comma-separated branches to check out as worktrees |
| `--all` | Create a worktree for every branch on origin |
| `--yes` | Skip confirmation prompts |
| `--dry-run` | Report the plan without touching disk |

### Behavior

1. Registers the repo under `groups.<group>.<alias>` in `.hydra/config.yaml`
2. Creates bare git data at `.bare/<alias>.git` with an explicit
   `remote.origin.fetch` refspec, then fetches
3. Records the resolved `default_branch`
4. Creates real worktree directories under `<group>/`
5. Runs `post_clone` hooks, once per worktree, with cwd set to that worktree

Default-branch worktrees use `<group>/<alias>/`. Other branches use `<group>/<alias>-<slug>/`.

### Interrupted clones resume

Registration happens in step 1, **before** the network fetch in step 2. Cloning a
large repository takes minutes, and an interrupted fetch used to leave
`.bare/<alias>.git` on disk with nothing in `.hydra/config.yaml` referencing it — an orphan
every command ignored, while the directory looked cloned. Registering first means an
interruption always leaves a repo hydra can see:

- `hydra doctor` reports it (`bare_unregistered` if the crash beat registration).
- Re-running the same `hydra clone` **completes** it instead of failing or starting
  over: the existing bare repo is brought up to spec (refspec, fetch, `origin/HEAD`)
  and the worktrees are created. The run reports this in `warnings`.

Re-cloning a *different* remote over a registered alias is refused with
`worktree_exists`; hydra never silently repoints an existing repo.

---

## hydra adopt

Import an existing Git checkout into the current Hydra project.

### Usage

```bash
hydra adopt
hydra adopt --group backend --alias api /path/to/existing/checkout
```

### Description

Converts a standalone repository into a Hydra-managed repo: bare storage under `.bare/<alias>.git`, worktree at the expected group path, and an updated `.hydra/config.yaml` entry.

Use when you already have a checkout and want to bring it under Hydra without re-cloning.

---

## hydra project

Manage the global project registry (not the same as `hydra init`).

### Usage

```bash
hydra project ls
hydra project add <name> <path>
hydra project rm <name>
hydra project ls --prune
```

The registry file lives at `<config-dir>/projects.yaml`. Override `<config-dir>` with `HYDRA_CONFIG_DIR`. See [Configuration](../configuration.md).

Use `hydra --project <name>` from any directory to target a registered workspace.

---

## See Also

- [Configuration](../configuration.md) — `.hydra/config.yaml` fields
- [Worktree Management](./worktree-management.md) — `add` / `remove` after bootstrap
- [Commands index](./README.md) — full command list

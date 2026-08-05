---
title: "Hydra Documentation"
description: "Complete guide for managing Git worktrees with Hydra"
version: "0.1.0"
ai_context: "Entry point for Hydra documentation: overview, quick start, and navigation"
---

# Hydra Documentation 🐍

> A beautiful CLI tool for managing Git worktrees with group organization.

## What is Hydra?

Hydra helps you work with multiple Git branches simultaneously by creating separate working
directories (worktrees) for each branch. Instead of stashing changes and switching branches, you can
have all branches open in different directories — and when one piece of work spans several
repositories, Hydra treats that whole set as **one topic** rather than N unrelated worktrees.

### Key Features

- 🌿 **Worktree Management**: Create, switch, and remove Git worktrees easily
- 🏗️ **Group Organization**: Group related repositories (backend, frontend, infra)
- 🎯 **Topics**: One unit of work spanning repositories — `hydra start <branch> --repos a,b --topic <id>`
  creates the whole set in one command, and `hydra topic` inspects it. Membership is recorded, never
  guessed from a branch name.
- 🏃 **Fan-out execution**: `hydra run --topic <id> -- <argv>` runs one command per worktree
- 📋 **Reproducible workspaces**: `hydra list -o json | hydra apply -` recreates a workspace elsewhere
- 🎨 **Beautiful CLI**: Tokyo Night theme with styled output (more themes via `hydra config`)
- ⚡ **Fast**: Compiled Go binary for instant startup
- 🔧 **Shell Integration**: Automatic directory switching with `hydra switch`
- 🤖 **Machine-readable output**: a JSON envelope with stable error codes on every command; the whole
  surface is published by `hydra commands --output json`

## On-Disk Layout

```text
<project-root>/
  .hydra/config.yaml     # shared manifest
  .bare/api.git/          # git data only
  backend/
    api/                  # default-branch worktree
    api-feat-login/       # branch feat/login
```

Bare repositories hold git objects only. Worktrees are real sibling directories under each group — never symlinks.

## Quick Start

### Installation

```bash
go install github.com/mssantosdev/hydra@latest
```

### 1. Initialize a Project

```bash
cd ~/projects/my-monorepo
hydra init
```

### 2. Setup Shell Integration

```bash
# Installs into your shell rc (default) — do not redirect stdout into rc
hydra init-shell
source ~/.bashrc

# Or print only the loader snippet:
hydra init-shell --install=false >> ~/.bashrc
source ~/.bashrc
```

### 3. Add a Worktree

```bash
hydra add api feature/new-endpoint
```

### 4. Switch or Locate a Worktree

```bash
# Interactive shells (auto-cd when helper is installed)
hydra switch api-feature-new-endpoint

# Scripts and agents
hydra path api-feature-new-endpoint
```

### 5. List Worktrees

```bash
hydra list    # alias: hydra ls
```

## Documentation Structure

### [Commands](./commands/README.md)

Complete command reference:

| Category | Commands |
|----------|----------|
| Project bootstrap | `init`, `new`, `repo add\|list\|remove`, `project` |
| Worktree lifecycle | `add`, `remove`, `path`, `switch` |
| Units of work | `start`, `topic`, `run`, `apply` |
| Inspection | `list` / `ls`, `status`, `doctor`, `prune` |
| Maintenance | `sync`, `hooks` |
| Settings | `config`, `init-shell`, `completion`, `skill`, `commands` |

Detailed pages:

- [Worktree Management](./commands/worktree-management.md) — `add`, `remove`
- [Topics and execution](./commands/topics-and-execution.md) — `topic`, `start`, `run`, `apply`
- [Project Bootstrap](./commands/project-bootstrap.md) — `new`, `init`, `repo add`

### [Configuration](./configuration.md)

- `.hydra/config.yaml` schema v2
- Group and alias layout
- Hooks and environment variables
- Global project registry

### Agents and Automation

Use the embedded skill — not a separate markdown guide:

- [skills/hydra/SKILL.md](../skills/hydra/SKILL.md), emitted by `hydra skill` / `hydra skill --install`
- `hydra commands --output json` publishes the whole command surface plus the complete
  error-code→exit table, so nothing needs to scrape `--help`. It works without a workspace.
- Always use `--output json` (or `--output auto` with a pipe) and branch on `error.code`
- `busy` is the only retryable code; everything else is terminal

## Common Workflows

### Feature Development

```bash
hydra add api feature/JIRA-123
hydra switch api-feature-JIRA-123
# ... work ...
hydra switch api
hydra remove api feature/JIRA-123
```

### One unit of work across repositories

```bash
# create the branch in both repos and record the topic in one command
hydra start feat/JIRA-123 --repos api,web --topic JIRA-123

# what is in this unit of work, and is any of it merged yet?
hydra status --topic JIRA-123 --against main

# run the same command in each of its worktrees
hydra run --topic JIRA-123 -- go build ./...

# tear the whole set down when it ships
hydra topic remove JIRA-123 --with-worktrees --yes
```

### Hotfix Workflow

```bash
hydra add api hotfix/critical-bug --from=prod
hydra switch api-hotfix-critical-bug
# ... fix ...
git push
hydra remove api hotfix/critical-bug --delete-branch
```

## Error Codes

Hydra uses stable `error.code` values in JSON output. Full table:

| code | exit | raised when |
|------|------|-------------|
| `not_in_project` | 2 | no `.hydra/config.yaml` found walking up, and no `--project` |
| `config_version_unsupported` | 2 | `.hydra/config.yaml` `version` is not `"2"` |
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
| `internal` | 1 | anything unclassified |

See [Configuration](./configuration.md) and [skills/hydra/SKILL.md](../skills/hydra/SKILL.md) for details.

## Getting Help

- **Command help**: `hydra <command> --help`
- **All commands**: `hydra --help`
- **Version**: `hydra --version`
- **Machine-readable surface**: `hydra commands --output json`
- **Diagnostics**: `hydra doctor`

## Contributing

See [GitHub repository](https://github.com/mssantosdev/hydra) for contribution guidelines.

## License

MIT License — See LICENSE file for details.

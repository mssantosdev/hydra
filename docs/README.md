---
title: "Hydra Documentation"
description: "Complete guide for managing Git worktrees with Hydra"
version: "0.1.0"
ai_context: "Entry point for Hydra documentation: overview, quick start, and navigation"
---

# Hydra Documentation 🐍

> A beautiful CLI tool for managing Git worktrees with group organization.

## What is Hydra?

Hydra helps you work with multiple Git branches simultaneously by creating separate working directories (worktrees) for each branch. Instead of stashing changes and switching branches, you can have all branches open in different directories.

### Key Features

- 🌿 **Worktree Management**: Create, switch, and remove Git worktrees easily
- 🏗️ **Group Organization**: Group related repositories (backend, frontend, infra)
- 🎨 **Beautiful CLI**: Tokyo Night theme with styled output (more themes via `hydra config`)
- ⚡ **Fast**: Compiled Go binary for instant startup
- 🔧 **Shell Integration**: Automatic directory switching with `hydra switch`
- 🤖 **Machine-readable output**: JSON envelopes for scripting and agents
- 🔖 **Version Visibility**: Root output and help include version information

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
| Project bootstrap | `init`, `new`, `clone`, `adopt`, `project` |
| Worktree lifecycle | `add`, `remove`, `path`, `switch` |
| Inspection | `list` / `ls`, `status`, `doctor`, `prune` |
| Maintenance | `sync`, `hooks` |
| Settings | `config`, `init-shell`, `completion`, `glossary`, `skill` |

Detailed pages:

- [Worktree Management](./commands/worktree-management.md) — `add`, `remove`
- [Project Bootstrap](./commands/project-bootstrap.md) — `new`, `init`, `clone`, `adopt`

### [Configuration](./configuration.md)

- `.hydra/config.yaml` schema v2
- Group and alias layout
- Hooks and environment variables
- Global project registry

### Agents and Automation

Use the embedded skill — not a separate markdown guide:

- [skills/hydra/SKILL.md](../skills/hydra/SKILL.md)
- `hydra skill` / `hydra skill --install`
- Always use `--output json` (or `--output auto` with a pipe) and branch on `error.code`

## Common Workflows

### Feature Development

```bash
hydra add api feature/JIRA-123
hydra switch api-feature-JIRA-123
# ... work ...
hydra switch api
hydra remove api feature/JIRA-123
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
- **Glossary**: `hydra glossary`
- **Diagnostics**: `hydra doctor`

## Contributing

See [GitHub repository](https://github.com/mssantosdev/hydra) for contribution guidelines.

## License

MIT License — See LICENSE file for details.

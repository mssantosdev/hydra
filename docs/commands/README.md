---
title: "Hydra Commands Reference"
description: "Complete reference for all Hydra commands organized by category"
ai_context: "Command index with decision trees and quick reference tables for AI agents"
---

# Commands Reference

Complete reference for all Hydra commands.

## Quick Command Table

| Task | Command | Documentation |
|------|---------|---------------|
| **Initialize project** | `hydra init` | [Project Setup](./init-clone.md#hydra-init) |
| **Bootstrap new project** | `hydra new` | [Project Bootstrap](./project-bootstrap.md#hydra-new) |
| **Clone repository** | `hydra clone <url>` | [Project Setup](./init-clone.md#hydra-clone) |
| **Add worktree** | `hydra add [<repo> <branch>]` | [Worktree Management](./worktree-management.md#hydra-add) |
| **Remove worktree** | `hydra remove [<repo> <branch>]` | [Worktree Management](./worktree-management.md#hydra-remove) |
| **Switch worktree** | `hydra switch [<worktree>]` | [Navigation](./navigation.md#hydra-switch) |
| **List worktrees** | `hydra list` | [Navigation](./navigation.md#hydra-list) |
| **Check status** | `hydra status` | [Navigation](./navigation.md#hydra-status) |
| **Sync updates** | `hydra sync [<alias>]` | [Sync](./sync.md#hydra-sync) |
| **Configure settings** | `hydra config` | [Configuration](./config-shell.md#hydra-config) |
| **Setup shell** | `hydra init-shell [bash\|zsh\|fish]` | [Configuration](./config-shell.md#hydra-init-shell) |
| **Show glossary** | `hydra glossary` | Built-in help |

## Command Categories

### [Project Setup](./init-clone.md)
Commands for initializing and cloning:
- `hydra init` - Initialize Hydra in current directory
- `hydra clone <url>` - Clone repository and setup worktrees

### [Project Bootstrap](./project-bootstrap.md)
Commands for creating a new Hydra project and first repository:
- `hydra new` - Create project root and bootstrap the first repo

### [Worktree Management](./worktree-management.md)
Commands for creating and deleting worktrees:
- `hydra add` - Create new worktree
- `hydra remove` - Delete worktree

### [Navigation](./navigation.md)
Commands for moving between and viewing worktrees:
- `hydra switch` - Switch to worktree (with auto-cd)
- `hydra list` - List all worktrees
- `hydra status` - Show worktree overview

### [Sync](./sync.md)
Commands for keeping worktrees up to date:
- `hydra sync` - Pull updates across worktrees

### [Configuration & Shell](./config-shell.md)
Commands for settings and shell integration:
- `hydra config` - Manage global configuration
- `hydra init-shell` - Setup shell integration

## Decision Tree for Command Selection

```
Starting new project?
├── Yes
│   ├── Want a guided local-first setup? → hydra new
│   └── Already have the project directory? → hydra init
│       └── Then: hydra clone <url>
└── No
    └── Continue...

Need to create worktree?
├── Yes
│   ├── Know repo and branch?
│   │   ├── Yes → hydra add <repo> <branch>
│   │   └── No → hydra add (interactive)
│   └── With specific base?
│       └── hydra add <repo> <branch> --from=<base>
└── No
    └── Continue...

Need to switch worktrees?
├── Yes
│   └── hydra switch <worktree>
│       └── Note: Requires shell helper initialized
└── No
    └── Continue...

Need to see what's available?
├── List all worktrees → hydra list
├── Check status → hydra status
└── Sync updates → hydra sync

Need to cleanup?
├── Remove worktree → hydra remove <repo> <branch>
└── Also delete branch → hydra remove <repo> <branch> --delete-branch
```

## Common Flag Patterns

### Global Flags (Available on all commands)

| Flag | Description |
|------|-------------|
| `--config string` | Config file path (default: `.hydra.yaml`) |
| `-h, --help` | Help for command |

### Common Patterns by Category

#### Creating Worktrees
```bash
# From specific base branch
hydra add api feature-x --from=develop

# Interactive mode (no args)
hydra add
```

#### Removing Worktrees
```bash
# Force remove (ignore uncommitted changes)
hydra remove api old-feature --force

# Also delete git branch
hydra remove api merged-feature --delete-branch

# Skip confirmation
hydra remove api temp --yes
```

#### Syncing
```bash
# Sync all repos
hydra sync --all

# Sync without prompts
hydra sync --yes

# Force pull dirty worktrees
hydra sync --force
```

## Exit Codes Reference

| Code | Meaning | Common Causes |
|------|---------|---------------|
| 0 | Success | Command completed successfully |
| 1 | General error | Invalid arguments, operation failed |
| 2 | Config not found | No `.hydra.yaml` in current or parent directories |
| 3 | Not found | Repository, worktree, or branch not found |

## See Also

- [AI Agent Guide](../ai-agent-guide.md) - For programmatic usage
- [Configuration](../configuration.md) - `.hydra.yaml` specification
- [Examples](../examples.md) - Real-world workflows

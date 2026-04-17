---
title: "Hydra Documentation"
description: "Complete guide for managing Git worktrees with Hydra - AI optimized"
version: "0.1.0"
ai_context: "Entry point for AI agents learning Hydra. Contains overview, quick start, and navigation to all documentation."
---

# Hydra Documentation 🐍

> A beautiful CLI tool for managing Git worktrees with ecosystem organization.

## What is Hydra?

Hydra helps you work with multiple Git branches simultaneously by creating separate working directories (worktrees) for each branch. Instead of stashing changes and switching branches, you can have all branches open in different directories.

### Key Features

- 🌿 **Worktree Management**: Create, switch, and remove Git worktrees easily
- 🏗️ **Ecosystem Organization**: Group related repositories (backend, frontend, infra)
- 🎨 **Beautiful CLI**: Tokyo Night theme with styled output
- ⚡ **Fast**: Compiled Go binary for instant startup
- 🔧 **Shell Integration**: Automatic directory switching with `hydra switch`

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

This creates `.hydra.yaml` configuration file.

### 2. Add a Worktree

```bash
# Interactive mode
hydra add

# Or direct
hydra add backend-api feature/new-endpoint
```

### 3. Switch Between Worktrees

```bash
# Requires shell helper (one-time setup)
hydra init-shell
source your shell rc/config file

# Or install completion at the same time
hydra init-shell --with-completion

# Then switch (auto-cd!)
hydra switch backend-api-feature-new-endpoint
```

### 4. List All Worktrees

```bash
hydra list
```

## Documentation Structure

### [Commands](./commands/)
Complete command reference organized by category:

| Category | Commands | Description |
|----------|----------|-------------|
| [Project Setup](./commands/init-clone.md) | `init`, `clone` | Initialize and clone repositories |
| [Worktree Management](./commands/worktree-management.md) | `add`, `remove` | Create and delete worktrees |
| [Navigation](./commands/navigation.md) | `switch`, `list`, `status` | Move between and view worktrees |
| [Sync](./commands/sync.md) | `sync` | Pull updates across worktrees |
| [Configuration](./commands/config-shell.md) | `config`, `init-shell` | Settings and shell integration |

### [Configuration](./configuration.md)
- `.hydra.yaml` specification
- JSON Schema for validation
- Example configurations

### [Shell Integration](./shell-integration.md)
- Automatic directory switching
- `hsw` alias setup
- Troubleshooting

### [Themes](./themes.md)
- Available themes (Tokyo Night, Catppuccin, Dracula, Nord, One Dark)
- Theme configuration
- Custom theme creation

### [Examples](./examples.md)
- Real-world workflows with ASCII diagrams
- Feature development workflow
- Hotfix workflow
- Multi-repo management

### [Troubleshooting](./troubleshooting.md)
- Common errors and solutions
- Exit codes reference
- FAQ

### [AI Agent Guide](./ai-agent-guide.md)
**Specialized documentation for AI agents:**
- Decision trees for command selection
- Programmatic usage patterns
- Automation scripts and templates
- Error handling in scripts

## Common Workflows

### Feature Development
```bash
# 1. Create feature worktree
hydra add api feature/JIRA-123

# 2. Switch to it
hydra switch api-feature-JIRA-123

# 3. Do work...
git commit -m "feat: add new endpoint"

# 4. Switch back to main
hydra switch api-main

# 5. Remove when merged
hydra remove api feature/JIRA-123
```

### Hotfix Workflow
```bash
# Create hotfix from production
hydra add api hotfix/critical-bug --from=prod

# Fix and deploy
hydra switch api-hotfix-critical-bug
# ... fix code ...
git commit -m "fix: critical bug"
git push

# Cleanup
hydra remove api hotfix/critical-bug
```

## Command Decision Tree

```
Need to create worktree?
  ├── Yes, for new branch
  │   └── hydra add <repo> <new-branch>
  └── No
      └── Continue...

Need to switch worktrees?
  ├── Yes, and auto-cd
  │   └── hydra switch <worktree>
  └── No
      └── Continue...

Need to cleanup?
  ├── Remove worktree only
  │   └── hydra remove <repo> <branch>
  └── Remove worktree + delete branch
      └── hydra remove <repo> <branch> --delete-branch
```

## Getting Help

- **Command help**: `hydra <command> --help`
- **All commands**: `hydra --help`
- **Glossary**: `hydra glossary`
- **This docs**: See links above

## Contributing

See [GitHub repository](https://github.com/mssantosdev/hydra) for contribution guidelines.

## License

MIT License - See LICENSE file for details.

---

**For AI Agents**: Start with the [AI Agent Guide](./ai-agent-guide.md) for decision trees and programmatic usage patterns.

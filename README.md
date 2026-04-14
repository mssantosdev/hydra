# Hydra 🐍

A beautiful CLI tool for managing Git worktrees with ecosystem organization.

## Features

- **Ecosystem Organization**: Group related repositories into logical ecosystems
- **Beautiful CLI**: Tokyo Night theme with modern styled output
- **Simple Commands**: Easy-to-use commands for listing, creating, and managing worktrees
- **Fast**: Compiled Go binary for instant startup

## Installation

```bash
go install github.com/mssantosdev/hydra@latest
```

Or clone and build:

```bash
git clone https://github.com/mssantosdev/hydra.git
cd hydra
make install
```

## Quick Start

1. **Initialize Hydra** in your project directory:
```bash
cd ~/projects/my-project
hydra init
```

This will:
- Detect Git repositories in your current directory
- Help you organize them into ecosystems
- Create a `.hydra.yaml` configuration file

2. **List all worktrees**:
```bash
hydra list
```

3. **Create a new worktree**:
```bash
hydra checkout <repo-alias> <branch-name>
```

Example:
```bash
hydra checkout api feature-new-endpoint
```

4. **Check status**:
```bash
hydra status
```

## Configuration

Hydra uses a `.hydra.yaml` file in your project root:

```yaml
version: "1.0"

paths:
  bare_dir: ".bare"      # Where bare repos are stored
  worktree_dir: "."      # Where worktrees are organized

ecosystems:
  backend:
    api: my-project-api
    worker: my-project-worker
  
  frontend:
    web: my-project-web
```

## Commands

- `hydra init` - Initialize configuration
- `hydra list` - List all worktrees with status
- `hydra checkout <alias> [branch]` - Create or switch to a worktree
- `hydra status` - Show worktree overview
- `hydra help` - Show help

## Theming

Hydra uses the Tokyo Night color scheme with:
- Clean, modern badges for status indication
- Styled headers and sections
- Consistent color scheme throughout

## License

MIT

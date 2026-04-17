# Hydra 🐍

A beautiful CLI tool for managing Git worktrees with ecosystem organization.

[![Go Report Card](https://goreportcard.com/badge/github.com/mssantosdev/hydra)](https://goreportcard.com/report/github.com/mssantosdev/hydra)

## Features

- 🌿 **Worktree Management**: Create, switch, and remove Git worktrees easily
- 🏗️ **Ecosystem Organization**: Group related repositories (backend, frontend, infra)
- 🎨 **Beautiful CLI**: Multiple themes (Tokyo Night, Catppuccin, Dracula, Nord, One Dark)
- ⚡ **Fast**: Compiled Go binary for instant startup
- 🔧 **Shell Integration**: Automatic directory switching with `hydra switch`
- 🌍 **Multi-language**: English and Portuguese (BR) support
- 🔖 **Version Visibility**: `hydra`, `hydra --help`, and `hydra --version` show version info

## Installation

```bash
go install github.com/mssantosdev/hydra@latest
```

Or clone and build:

```bash
git clone https://github.com/mssantosdev/hydra.git
cd hydra
go build -o hydra .
```

## Quick Start

### 1. Initialize Hydra

```bash
cd ~/projects/my-project
hydra init
```

This creates `.hydra.yaml` configuration file.

### 2. Setup Shell Integration (Recommended)

```bash
hydra init-shell
source ~/.bashrc  # or ~/.zshrc
```

This enables automatic directory switching.

### 3. Add a Worktree

```bash
# Interactive mode
hydra add

# Or direct
hydra add api feature/new-endpoint
```

### 4. Switch Between Worktrees

```bash
# With shell helper - automatically changes directory!
hydra switch api-feature-new-endpoint

# Without shell helper - shows cd command
hydra switch api-feature-new-endpoint
```

### 5. List All Worktrees

```bash
hydra list
```

## Documentation

Complete documentation available in the [`docs/`](docs/) directory:

- **[Getting Started](docs/README.md)** - Overview and quick start
- **[Commands](docs/commands/)** - Complete command reference
  - [Worktree Management](docs/commands/worktree-management.md) - `add`, `remove`
  - [Navigation](docs/commands/navigation.md) - `switch`, `list`, `status`
  - [Project Setup](docs/commands/init-clone.md) - `init`, `clone`
  - [Sync](docs/commands/sync.md) - `sync`
  - [Configuration](docs/commands/config-shell.md) - `config`, `init-shell`
- **[Configuration](docs/configuration.md)** - `.hydra.yaml` specification
- **[Shell Integration](docs/shell-integration.md)** - Auto-cd setup
- **[Themes](docs/themes.md)** - Theme configuration
- **[AI Agent Guide](docs/ai-agent-guide.md)** - For AI automation

## Example Configuration

`.hydra.yaml`:

```yaml
version: "1.0"

ecosystems:
  backend:
    api: my-api
    worker: my-worker
  
  frontend:
    web: my-web
    admin: my-admin
```

## Common Workflows

### Feature Development

```bash
# 1. Create feature worktree
hydra add backend-api feature/JIRA-123

# 2. Switch to it (auto-cd!)
hydra switch backend-api-feature-JIRA-123

# 3. Do work...
git commit -m "feat: new feature"

# 4. Cleanup when done
hydra switch backend-api-main
hydra remove backend-api feature/JIRA-123
```

### Hotfix Production

```bash
# Create hotfix from prod branch
hydra add api hotfix/critical-bug --from=prod

# Fix and deploy
hydra switch api-hotfix-critical-bug
# ... fix ...
git push

# Cleanup
hydra remove api hotfix/critical-bug --delete-branch
```

## Commands Overview

| Command | Description |
|---------|-------------|
| `hydra init` | Initialize Hydra in current directory |
| `hydra clone <url>` | Clone repository and setup worktrees |
| `hydra add [<repo> <branch>]` | Create new worktree |
| `hydra remove [<repo> <branch>]` | Remove worktree |
| `hydra switch [<worktree>]` | Switch to worktree (auto-cd) |
| `hydra list` | List all worktrees |
| `hydra status` | Show worktree overview |
| `hydra sync [<alias>]` | Pull updates across worktrees |
| `hydra config` | Manage global configuration |
| `hydra init-shell` | Setup shell integration |
| `hydra glossary` | Show terminology help |

## Themes

Hydra supports multiple themes. Change with:

```bash
hydra config
# Select "Theme" and choose from:
# - tokyonight (default)
# - catppuccin
# - dracula
# - nord
# - onedark
```

## Shell Integration

The shell helper enables `hydra switch` to automatically change your directory:

```bash
# Install helper
hydra init-shell >> ~/.bashrc
source ~/.bashrc

# Now this changes directory automatically!
hydra switch my-worktree

# Or use the hsw alias
hsw my-worktree
```

## Configuration

Global configuration stored in:
- Linux: `~/.config/hydra/config.yaml`
- macOS: `~/Library/Application Support/hydra/config.yaml`
- Windows: `%APPDATA%/hydra/config.yaml`

Configure with:

```bash
hydra config
```

Settings include:
- Language (en-US, pt-BR)
- Theme
- Default editor

## AI Agent Usage

See [AI Agent Guide](docs/ai-agent-guide.md) for:
- Decision trees
- Programmatic usage patterns
- Automation scripts
- Error handling

## License

MIT License - See [LICENSE](LICENSE) file for details.

## Contributing

Contributions welcome! See [GitHub repository](https://github.com/mssantosdev/hydra).

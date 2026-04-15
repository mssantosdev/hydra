---
title: "Configuration"
description: "Hydra configuration file specification with inline JSON Schema"
ai_context: "Complete .hydra.yaml specification including JSON Schema for validation"
---

# Configuration

Hydra uses a `.hydra.yaml` file in your project root to store configuration.

## JSON Schema (Inline)

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Hydra Configuration",
  "description": "Configuration file for Hydra Git worktree manager",
  "type": "object",
  "required": ["version"],
  "properties": {
    "version": {
      "type": "string",
      "description": "Configuration file version",
      "enum": ["1.0"]
    },
    "paths": {
      "type": "object",
      "description": "Directory paths for Hydra",
      "properties": {
        "bare_dir": {
          "type": "string",
          "description": "Directory for bare repositories",
          "default": ".bare"
        },
        "worktree_dir": {
          "type": "string",
          "description": "Base directory for worktrees",
          "default": "."
        }
      }
    },
    "ecosystems": {
      "type": "object",
      "description": "Groups of related repositories",
      "additionalProperties": {
        "type": "object",
        "description": "Ecosystem group containing repositories",
        "additionalProperties": {
          "type": "string",
          "description": "Repository name mapped to bare repo name"
        }
      }
    },
    "defaults": {
      "type": "object",
      "description": "Default settings",
      "properties": {
        "base_branch": {
          "type": "string",
          "description": "Default branch for new worktrees",
          "default": "main"
        }
      }
    }
  }
}
```

## Configuration Sections

### version

Required. Must be `"1.0"`.

```yaml
version: "1.0"
```

### paths

Optional. Controls where Hydra stores repositories and worktrees.

```yaml
paths:
  bare_dir: ".bare"      # Where bare repos are stored
  worktree_dir: "."      # Where worktrees are organized
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `bare_dir` | string | `.bare` | Directory for bare repositories |
| `worktree_dir` | string | `.` | Base directory for worktree symlinks |

### ecosystems

Required. Defines groups of related repositories.

```yaml
ecosystems:
  backend:
    api: my-project-api
    worker: my-project-worker
  frontend:
    web: my-project-web
    admin: my-project-admin
```

Structure:
- **Ecosystem name** (e.g., `backend`): Group identifier
  - **Alias** (e.g., `api`): Short name used in commands
    - **Bare repo name** (e.g., `my-project-api`): Actual repository name

### defaults

Optional. Default settings for commands.

```yaml
defaults:
  base_branch: "stage"   # Default branch for new worktrees
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_branch` | string | `main` | Default branch when creating worktrees |

## Example Configurations

### Basic

Minimal configuration for a simple project:

```yaml
version: "1.0"
ecosystems:
  default:
    app: my-app
```

### Standard Multi-Service

Common setup with backend and frontend:

```yaml
version: "1.0"
paths:
  bare_dir: ".bare"
  worktree_dir: "."

ecosystems:
  backend:
    api: my-api
    worker: my-worker
    scheduler: my-scheduler
  
  frontend:
    web: my-web
    admin: my-admin
  
  infra:
    terraform: my-terraform
    ansible: my-ansible

defaults:
  base_branch: stage
```

### Complex Monorepo

Large project with multiple ecosystems:

```yaml
version: "1.0"

paths:
  bare_dir: ".bare"
  worktree_dir: "."

ecosystems:
  # Core services
  core:
    api-gateway: core-api-gateway
    auth-service: core-auth
    user-service: core-users
  
  # Business logic
  services:
    billing: svc-billing
    notifications: svc-notifications
    reports: svc-reports
  
  # Frontend applications
  frontend:
    web-app: fe-web
    mobile-app: fe-mobile
    admin-panel: fe-admin
  
  # Infrastructure
  infrastructure:
    terraform: infra-terraform
    kubernetes: infra-k8s
    monitoring: infra-monitoring
  
  # Documentation
  docs:
    api-docs: docs-api
    user-guides: docs-guides

defaults:
  base_branch: develop
```

## File Location

Hydra searches for `.hydra.yaml` in:

1. Current directory
2. Parent directories (up to root)

The first found file is used.

## Validation

To validate your configuration:

```bash
# Hydra will validate on startup
hydra list
# If config is invalid, you'll see an error
```

Common validation errors:

| Error | Cause | Fix |
|-------|-------|-----|
| `version is required` | Missing version field | Add `version: "1.0"` |
| `ecosystems is required` | No ecosystems defined | Add at least one ecosystem |
| `invalid version` | Wrong version string | Use exactly `"1.0"` |

## Migration

### From v0.0.x to v0.0.9+

No changes needed. Configuration format is backward compatible.

### From checkout to add/remove

The config format didn't change, only the commands:

- Old: `hydra checkout api feature-x`
- New: `hydra add api feature-x`

## See Also

- [Commands](../commands/) - How to use Hydra commands
- [Examples](../examples.md) - Real-world configurations
- [AI Agent Guide](../ai-agent-guide.md) - Programmatic usage

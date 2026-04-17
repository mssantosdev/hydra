---
title: "Project Bootstrap"
description: "hydra new - Create a new Hydra project and bootstrap the first repository"
ai_context: "Reference for hydra new interactive project creation for local-first and remote-first onboarding"
---

# Project Bootstrap

## hydra new

Create a new Hydra project and bootstrap the first repository.

### Description

`hydra new` starts an interactive flow that creates a new Hydra project root under the current directory and sets up the first repository.

The project path is treated as a relative path, so nested paths like `client-a/platform` are allowed. Hydra creates `.hydra.yaml` and `.bare/` inside the final project root.

Current bootstrap modes:

1. `Create local repo`
2. `Clone remote repo`

### Usage

```bash
hydra new
```

### Interactive Flow

1. Enter the project path relative to the current directory
2. Choose the first repository mode
3. Enter group, alias, and initial branch
4. For local mode: choose the local repository directory name
5. For remote mode: enter the remote URL
6. Hydra creates the project and prints a `cd` hint

### Path Rules

- project path may contain `/`
- project path must be relative
- project path cannot escape upward with `..`
- group and alias are names, not paths, so they cannot contain path separators

### Examples

```bash
$ hydra new

Project Path: client-a/api-platform
First Repository: Create local repo
Group: backend
Alias: api
Initial Branch: main
Local Repository Directory: api-repo
```

This creates:

```text
./client-a/api-platform/.hydra.yaml
./client-a/api-platform/.bare/
./client-a/api-platform/api-repo/
./client-a/api-platform/backend/api
```

### Notes

- local repo bootstrap creates an initial Git repository and first commit
- remote bootstrap reuses the existing clone flow inside the new project root
- after project creation Hydra prints `cd <project>` and `hydra list` as the next steps

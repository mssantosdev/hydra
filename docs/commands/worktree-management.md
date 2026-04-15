---
title: "Worktree Management Commands"
description: "hydra add and hydra remove - Create and delete Git worktrees"
ai_context: "Complete reference for add and remove commands with all flags, examples, and combinations"
---

# Worktree Management

Commands for creating and deleting Git worktrees.

---

## hydra add

Create a new worktree for a repository branch.

### Description

`hydra add` creates a new Git worktree - a separate working directory linked to a specific branch. This allows you to work on multiple branches simultaneously without stashing or committing incomplete work.

When you run `hydra add`:
1. Creates a new worktree directory in `.bare/<repo>/<branch>/`
2. Creates a symlink in `<ecosystem>/<repo>-<branch>` for easy access
3. Checks out the specified branch (creating it if needed)

### Usage

```bash
hydra add [<repo-alias> <branch-name>] [flags]
```

### Aliases

- `add`
- `create` (not implemented yet)

### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--from` | `-f` | string | `HEAD` | Create branch from this branch |
| `--track` | `-t` | string | `""` | Track remote branch (e.g., `origin/feature-x`) |
| `--help` | `-h` | bool | - | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `repo-alias` | No* | Repository alias from `.hydra.yaml` |
| `branch-name` | No* | Name of branch to create/checkout |

*Both required together for direct mode. Omit both for interactive mode.

### Examples

#### Interactive Mode

Run without arguments to get an interactive form:

```bash
$ hydra add

┌─────────────────────────────────────────┐
│  Add New Worktree                       │
├─────────────────────────────────────────┤
│                                         │
│  Repository: [api ▼]                    │
│    ▸ api                                │
│      web                                │
│      worker                             │
│                                         │
│  Branch name: [feature/my-feature______]│
│                                         │
│         [Create]  [Cancel]              │
└─────────────────────────────────────────┘
```

#### Direct Mode - Basic

```bash
$ hydra add api feature-x
Creating worktree for api:feature-x...
✓ Worktree created
  Path: .bare/api.git/feature-x
  Branch: feature-x
  Symlink: backend/api-feature-x

Switch to it with: hydra switch api-feature-x
```

#### Create from Specific Branch

```bash
# Create feature branch from develop instead of HEAD
$ hydra add api feature-y --from=develop
Creating worktree for api:feature-y...
✓ Worktree created
  Branch created from: develop
```

#### Track Remote Branch

```bash
# Create worktree tracking origin/feature-z
$ hydra add api feature-z --track=origin/feature-z
Creating worktree for api:feature-z...
✓ Worktree created
  Tracking: origin/feature-z
```

#### Create from Production

```bash
# Common pattern: Create hotfix from prod
$ hydra add api hotfix/critical-bug --from=prod
Creating worktree for api:hotfix/critical-bug...
✓ Worktree created
  Branch created from: prod
```

### Common Combinations

| Goal | Command |
|------|---------|
| Create feature from main | `hydra add repo feature-x --from=main` |
| Create feature from develop | `hydra add repo feature-x --from=develop` |
| Create hotfix from prod | `hydra add repo hotfix-x --from=prod` |
| Track remote PR branch | `hydra add repo pr-123 --track=origin/pr-123` |
| Interactive selection | `hydra add` (no args) |

### Branch Naming

Branch names are normalized for filesystem compatibility:

| Input | Normalized | Worktree Path |
|-------|------------|---------------|
| `feature/new-api` | `feature-new-api` | `.bare/api.git/feature-new-api` |
| `hotfix/urgent` | `hotfix-urgent` | `.bare/api.git/hotfix-urgent` |
| `jira-123` | `jira-123` | `.bare/api.git/jira-123` |

### When Worktree Already Exists

If the worktree already exists:

```bash
$ hydra add api feature-x
✓ Worktree already exists
  Path: .bare/api.git/feature-x
  Branch: feature-x

Switch to it with: hydra switch api-feature-x
```

No error - just informs you it already exists.

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success (created or already exists) |
| 1 | General error (invalid args, etc.) |
| 2 | Config file not found |

### See Also

- [hydra remove](#hydra-remove) - Remove a worktree
- [hydra switch](./navigation.md#hydra-switch) - Switch to a worktree
- [Examples](../examples.md) - Real-world workflows

---

## hydra remove

Remove a worktree for a repository branch.

### Description

`hydra remove` deletes a Git worktree and optionally the associated branch. It performs safety checks to prevent accidental data loss.

When you run `hydra remove`:
1. Checks for uncommitted changes (warns unless `--force`)
2. Removes the worktree directory
3. Removes the symlink
4. Optionally deletes the git branch (with `--delete-branch`)

### Usage

```bash
hydra remove [<repo-alias> <branch-name>] [flags]
```

### Aliases

- `remove`
- `rm`
- `delete` (not implemented yet)

### Flags

| Flag | Short | Type | Default | Description |
|------|-------|------|---------|-------------|
| `--force` | `-f` | bool | `false` | Force remove (ignore uncommitted changes) |
| `--delete-branch` | `-d` | bool | `false` | Also delete the git branch |
| `--yes` | `-y` | bool | `false` | Skip confirmation prompts |
| `--help` | `-h` | bool | - | Show help |

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `repo-alias` | No* | Repository alias from `.hydra.yaml` |
| `branch-name` | No* | Name of branch to remove |

*Both required together for direct mode. Omit both for interactive mode.

### Examples

#### Interactive Mode

Run without arguments to select from list:

```bash
$ hydra remove

┌─────────────────────────────────────────┐
│  Remove Worktree          [Search...]   │
├─────────────────────────────────────────┤
│                                         │
│  [ ] api-main (clean)                   │
│  [ ] api-stage (clean)                  │
│  [✓] api-old-feature ⚠️                 │
│      └─ 3 uncommitted changes           │
│  [ ] web-main (clean)                   │
│                                         │
│  [✓] Delete branch too                  │
│                                         │
│         [Remove]  [Cancel]              │
└─────────────────────────────────────────┘
```

#### Basic Removal

```bash
$ hydra remove api old-feature
Removing worktree api:old-feature...
✓ Worktree removed
```

#### Force Remove (Dirty Worktree)

```bash
# Worktree has uncommitted changes
$ hydra remove api temp-feature
⚠ Warning: Worktree has uncommitted changes
  3 modified file(s)

Options:
  1. Commit or stash changes first
  2. Use --force to remove anyway (changes will be lost)

# Force remove
$ hydra remove api temp-feature --force --yes
⚠ Warning: 3 uncommitted changes will be lost!
✓ Worktree removed
```

#### Remove and Delete Branch

```bash
# Remove worktree AND delete the git branch
$ hydra remove api merged-feature --delete-branch
Removing worktree api:merged-feature...
✓ Worktree removed
Deleting branch merged-feature...
✓ Branch deleted
```

#### Skip All Prompts

```bash
# Use --yes to skip confirmation
$ hydra remove api temp --force --yes
✓ Worktree removed
```

### Safety Features

#### Uncommitted Changes Check

By default, `hydra remove` checks for uncommitted changes:

```bash
$ hydra remove api feature-x
⚠ Warning: Worktree has uncommitted changes
  M src/main.go
  D README.md
  ?? temp/debug.log

Use --force to remove anyway, or commit/stash changes first.
Error: worktree has uncommitted changes
```

**Solutions:**
1. Commit changes: `git commit -am "WIP"`
2. Stash changes: `git stash push -m "before-remove"`
3. Force remove: `hydra remove api feature-x --force`

#### Interactive Confirmation

Without `--yes`, shows confirmation:

```bash
$ hydra remove api feature-x
Remove worktree api:feature-x?
⚠ WARNING: This will delete uncommitted changes!

[Yes, remove]  [Cancel]
```

### Common Combinations

| Goal | Command |
|------|---------|
| Safe remove (clean worktree) | `hydra remove repo branch` |
| Force remove dirty worktree | `hydra remove repo branch --force` |
| Remove without prompts | `hydra remove repo branch --yes` |
| Remove worktree + branch | `hydra remove repo branch --delete-branch` |
| Force remove + delete branch | `hydra remove repo branch --force --delete-branch` |

### Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error (worktree not found, etc.) |
| 2 | Config file not found |

### When Worktree Doesn't Exist

```bash
$ hydra remove api nonexistent
Error: worktree does not exist: .bare/api.git/nonexistent
```

### See Also

- [hydra add](#hydra-add) - Create a worktree
- [hydra switch](./navigation.md#hydra-switch) - Switch to a worktree
- [Troubleshooting](../troubleshooting.md) - Common issues

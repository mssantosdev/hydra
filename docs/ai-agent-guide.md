---
title: "AI Agent Guide"
description: "Comprehensive guide for AI agents using Hydra - decision trees and programmatic usage"
ai_context: "Primary documentation for AI agents. Contains decision trees, automation patterns, and programmatic usage examples."
---

# AI Agent Guide to Hydra

This guide is specifically designed for AI agents and automation systems using Hydra.

## Quick Decision Tree

```
Need to work with Hydra?
│
├─ Starting new project?
│  └─ hydra init
│     └─ Then: hydra clone <url>
│
├─ Need to create worktree?
│  ├─ hydra add <repo> <branch>
│  └─ With specific base?
│     └─ hydra add <repo> <branch> --from=<base>
│
├─ Need to switch worktrees?
│  └─ hydra switch <worktree>
│     └─ Requires: HYDRA_SHELL_HELPER=1
│
├─ Need to cleanup?
│  ├─ Remove worktree only
│  │  └─ hydra remove <repo> <branch>
│  └─ Remove + delete branch
│     └─ hydra remove <repo> <branch> --delete-branch
│
└─ Need to sync updates?
   └─ hydra sync --all
```

## Programmatic Usage

### Check if in Hydra Project

```bash
# Check if hydra can find config
if hydra list &>/dev/null; then
    echo "In Hydra project"
    PROJECT_ROOT=$(pwd)
else
    echo "Not in Hydra project"
    exit 1
fi
```

### Check if Shell Helper Initialized

```bash
# Check environment variable
if [ -z "$HYDRA_SHELL_HELPER" ]; then
    echo "Shell helper not initialized"
    echo "Run: hydra init-shell && source ~/.bashrc"
    exit 1
fi
```

### Create and Switch Pattern

```bash
#!/bin/bash
# create-and-switch.sh - Template for AI agents

REPO=$1
BRANCH=$2
BASE=${3:-stage}

# 1. Create worktree
if ! hydra add "$REPO" "$BRANCH" --from="$BASE"; then
    echo "ERROR: Failed to create worktree"
    exit 1
fi

# 2. Switch to it (only if shell helper active)
if [ -n "$HYDRA_SHELL_HELPER" ]; then
    hydra switch "${REPO}-${BRANCH}"
    echo "SUCCESS: Now in worktree ${REPO}-${BRANCH}"
else
    echo "WARNING: Shell helper not active"
    echo "Manually run: cd ${REPO}-${BRANCH}"
fi
```

### Cleanup After Merge

```bash
#!/bin/bash
# cleanup-merged.sh - Remove worktree after PR merged

REPO=$1
BRANCH=$2

# Check if worktree exists
if ! hydra list | grep -q "${REPO}-${BRANCH}"; then
    echo "Worktree ${REPO}-${BRANCH} not found"
    exit 0
fi

# Switch to main first
hydra switch "${REPO}-main" || hydra switch "${REPO}-stage"

# Remove the feature worktree
if hydra remove "$REPO" "$BRANCH" --yes; then
    echo "SUCCESS: Removed ${REPO}-${BRANCH}"
else
    echo "ERROR: Failed to remove worktree"
    exit 1
fi
```

### Batch Operations

```bash
#!/bin/bash
# batch-create.sh - Create multiple worktrees

REPOS=("api" "web" "worker")
BRANCH="feature/JIRA-123"

for REPO in "${REPOS[@]}"; do
    echo "Creating worktree for ${REPO}..."
    if hydra add "$REPO" "$BRANCH" --from=stage; then
        echo "✓ ${REPO} created"
    else
        echo "✗ ${REPO} failed"
    fi
done
```

## Error Handling

### Common Errors and Solutions

| Error | Detection | Solution |
|-------|-----------|----------|
| `no .hydra.yaml found` | `hydra list` exit code 2 | Run `hydra init` or cd to project root |
| `Shell helper not initialized` | `$HYDRA_SHELL_HELPER` empty | Run `hydra init-shell && source ~/.bashrc` |
| `worktree has uncommitted changes` | `hydra remove` fails | Stash: `git stash` or force: `--force` |
| `unknown alias` | `hydra add` fails | Check `hydra list` for valid aliases |

### Robust Error Handling Script

```bash
#!/bin/bash
set -e  # Exit on error

# Function to check Hydra project
check_hydra_project() {
    if ! hydra list &>/dev/null; then
        echo "ERROR: Not in a Hydra project"
        echo "SOLUTION: Run 'hydra init' or cd to project root"
        return 1
    fi
    return 0
}

# Function to check shell helper
check_shell_helper() {
    if [ -z "$HYDRA_SHELL_HELPER" ]; then
        echo "WARNING: Shell helper not initialized"
        echo "SOLUTION: Run 'hydra init-shell && source ~/.bashrc'"
        return 1
    fi
    return 0
}

# Function to safe remove
safe_remove() {
    local repo=$1
    local branch=$2
    
    # Check if dirty
    if hydra list | grep "${repo}-${branch}" | grep -q "modified"; then
        echo "WARNING: ${repo}-${branch} has uncommitted changes"
        echo "Stashing before removal..."
        cd "${repo}-${branch}" || return 1
        git stash push -m "auto-stash-before-remove"
        cd - || return 1
    fi
    
    # Switch to main first
    hydra switch "${repo}-main" || true
    
    # Remove
    hydra remove "$repo" "$branch" --yes
}

# Main logic
check_hydra_project || exit 1

echo "Hydra project detected"
echo "Shell helper: $([ -n "$HYDRA_SHELL_HELPER" ] && echo "active" || echo "inactive")"
```

## Workflows for AI Agents

### Workflow 1: Feature Development

```bash
# Step 1: Create feature worktree
hydra add backend-api feature/JIRA-123 --from=stage

# Step 2: Switch to it (if shell helper active)
[ -n "$HYDRA_SHELL_HELPER" ] && hydra switch backend-api-feature-JIRA-123

# Step 3: Do work
git checkout -b feature/JIRA-123
git commit -m "feat: implement new feature"
git push -u origin feature/JIRA-123

# Step 4: Create PR (via API)
# ... create PR logic ...

# Step 5: After PR merged, cleanup
checkout main
hydra remove backend-api feature/JIRA-123 --delete-branch
```

### Workflow 2: Hotfix Production

```bash
# Step 1: Create hotfix from prod
hydra add backend-api hotfix/critical-bug --from=prod

# Step 2: Switch and fix
hydra switch backend-api-hotfix-critical-bug
# ... fix code ...
git commit -m "fix: critical bug"

# Step 3: Deploy to prod
git push origin hotfix/critical-bug
# ... deploy ...

# Step 4: Merge to stage and main
git checkout stage && git merge hotfix/critical-bug
git checkout main && git merge hotfix/critical-bug

# Step 5: Cleanup
hydra remove backend-api hotfix/critical-bug --delete-branch
```

### Workflow 3: Code Review

```bash
# Step 1: Fetch PR branch
PR_BRANCH="pr-456"
REMOTE_BRANCH="origin/pr-456"

# Step 2: Create worktree for review
hydra add backend-api "review-${PR_BRANCH}" --track="${REMOTE_BRANCH}"

# Step 3: Review code
hydra switch "backend-api-review-${PR_BRANCH}"
# ... review ...

# Step 4: Cleanup when done
hydra remove backend-api "review-${PR_BRANCH}"
```

## Best Practices

1. **Always check if in Hydra project** before running commands
2. **Check shell helper status** when using `hydra switch`
3. **Switch to stable branch** (main/stage) before removing worktrees
4. **Use `--yes` flag** in automation to avoid interactive prompts
5. **Handle dirty worktrees** by stashing or using `--force`

## API-Like Usage Summary

| Task | Command | Notes |
|------|---------|-------|
| Check project | `hydra list &>/dev/null` | Exit 0 = in project |
| Check shell helper | `[ -n "$HYDRA_SHELL_HELPER" ]` | True = active |
| Create worktree | `hydra add <repo> <branch>` | Use `--from` for base |
| Switch worktree | `hydra switch <worktree>` | Requires shell helper |
| Remove worktree | `hydra remove <repo> <branch> --yes` | Force with `--force` |
| Sync all | `hydra sync --all --yes` | Non-interactive |

## See Also

- [Commands Reference](./commands/) - Complete command documentation
- [Configuration](../configuration.md) - `.hydra.yaml` specification
- [Examples](../examples.md) - Real-world workflows

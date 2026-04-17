---
name: workspace-sync-awareness
description: Load the dogfood workspace sync contract and determine whether planning/OpenCode assets are aligned with main repo master.
---
## What to read

1. `planning/workspace-sync.md`
2. `AGENTS.md`
3. `planning/constitution.md`

## What to determine

- source of truth repo and branch
- last synced source commit
- whether the dogfood root is aligned or stale
- whether orchestration should pause for resync

## Role expectations

- manager checks sync state before planning-sensitive orchestration
- merger owns executing and recording resync

---
name: epic-reader
description: Load the active epic context, including constitution, branch plan, agent assignments, and release target.
---
## What to read

1. `AGENTS.md`
2. `planning/constitution.md`
3. `planning/README.md`
4. the active epic folder under `planning/epics/`
5. the active epic's `tasks.md`
6. the active epic's `tasks/` directory

## What to return

- active epic name
- target release
- current scope
- task index and acceptance criteria
- relevant branches
- relevant agent assignments

When a specific task is assigned, also identify the matching task file under `tasks/`.

## When to use

Use this before starting any non-trivial implementation, review, orchestration, or merge task.

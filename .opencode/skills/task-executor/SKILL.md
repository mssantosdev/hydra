---
name: task-executor
description: Execute an assigned epic task with validation and readiness-for-review discipline.
---
## Execution pattern

1. confirm assigned scope
2. restate the task definition from the assigned file under `tasks/`
3. implement the smallest correct change
4. run relevant validation
5. summarize what changed
6. mark the task ready for review with acceptance criteria status

## Review loop

If review returns `changes_requested`, update the same branch and resubmit.

Before handoff, update the task file with:

- current state
- observations/comments
- handoff summary

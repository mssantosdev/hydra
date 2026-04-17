---
name: task-executor
description: Execute an assigned epic task with validation and readiness-for-review discipline.
---
## Execution pattern

1. confirm assigned scope
2. implement the smallest correct change
3. run relevant validation
4. summarize what changed
5. mark the task ready for review with acceptance criteria status

## Review loop

If review returns `changes_requested`, update the same branch and resubmit.

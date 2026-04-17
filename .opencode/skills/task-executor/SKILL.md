---
name: task-executor
description: Execute an assigned epic task with validation and readiness-for-review discipline.
---
## Execution pattern

1. confirm assigned scope
2. restate the task definition from the assigned planning task file
3. implement the smallest correct change
4. run relevant validation
5. summarize what changed
6. mark the task ready for review with acceptance criteria status
7. record whether official review targets a committed SHA or a working tree

## Review loop

If review returns `changes_requested`, update the same branch and resubmit.

Before handoff, update the runtime task file with:

- current state
- observations/comments
- validation run
- ready-for-review or reviewed SHA
- handoff summary

Prefer official review against a committed branch HEAD SHA whenever practical so reviewer and merger can operate on the same artifact.

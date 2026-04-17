---
description: Assign an epic task to an implementer with branch, scope, and acceptance criteria.
agent: manager
subtask: true
---
Using the active epic docs and the arguments `$ARGUMENTS`, assign a task to an implementer.

The assignment must come from the active epic's `tasks.md` task index and must reference the matching task file under `tasks/`.

Return:

1. task summary
2. assigned branch/worktree
3. files or areas expected to change
4. acceptance criteria
5. review readiness expectations
6. task file to update during execution
7. whether sub-implementer delegation is expected or optional
8. runtime state fields that must be updated before review, including current status, validation run, and ready-for-review SHA

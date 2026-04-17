---
description: Prepare approved work for integration by checking merge order, readiness, and validation needs.
agent: merger
subtask: true
---
Using the active epic merge plan and `$ARGUMENTS`, assess whether the specified approved task or branch is ready to merge.

Use `tasks.md`, the planning task file, and the runtime task file to verify that the task is complete, approved, and ready for merge.

Do not treat chat-only approval as sufficient. Confirm the runtime task file records approved state and the reviewed SHA.

Return:

1. merge readiness
2. prerequisites still needed
3. expected conflict areas
4. validation steps to run after merge
5. approved SHA and whether it exists on the branch to merge
6. worktree cleanliness or intentional exceptions that could affect integration

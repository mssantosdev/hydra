---
description: Prepare approved work for integration by checking merge order, readiness, and validation needs.
agent: merger
subtask: true
---
Using the active epic merge plan and `$ARGUMENTS`, assess whether the specified approved task or branch is ready to merge.

Use `tasks.md` and the assigned task file under `tasks/` to verify that the task is complete, approved, and ready for merge.

Return:

1. merge readiness
2. prerequisites still needed
3. expected conflict areas
4. validation steps to run after merge

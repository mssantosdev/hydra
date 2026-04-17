---
description: Review a completed task and return approved or changes_requested with actionable guidance.
agent: reviewer
subtask: true
---
Review the task described in `$ARGUMENTS` using the Hydra review contract.

Use the active epic's `tasks.md` index and the assigned task file under `tasks/` as the review baseline.

Return:

1. status: `approved` or `changes_requested`
2. reviewed SHA
3. findings
4. required changes if any
5. guidance
6. validation expectations
7. whether integration-style validation was performed, and what was checked instead if not

Also update or return the text needed for the runtime task file's review history, review decision, next owner, and current state sections.

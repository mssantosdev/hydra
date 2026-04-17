---
description: Review a completed task and return approved or changes_requested with actionable guidance.
agent: reviewer
subtask: true
---
Review the task described in `$ARGUMENTS` using the Hydra review contract.

Use the active epic's `tasks.md` index and the assigned task file under `tasks/` as the review baseline.

Return:

1. status: `approved` or `changes_requested`
2. findings
3. required changes if any
4. guidance
5. validation expectations

Also update or return the text needed for the task file's review history and current state sections.

This command is for official review, not checkpoint review.

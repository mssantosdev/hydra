---
description: Review a completed task and return approved or changes_requested with actionable guidance.
agent: reviewer
subtask: true
---
Review the task described in `$ARGUMENTS` using the Hydra review contract.

Return:

1. status: `approved` or `changes_requested`
2. findings
3. required changes if any
4. guidance
5. validation expectations

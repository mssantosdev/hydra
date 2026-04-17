---
description: Start implementation of an assigned task using the implementer role and project workflow.
agent: implementer
subtask: true
---
Implement the assigned task described in `$ARGUMENTS`.

Before changing code:

1. load the active epic context
2. confirm branch/task scope from the assigned task file under `tasks/`
3. restate acceptance criteria

Then execute the task following project rules and return a concise readiness-for-review summary.

Before returning, update the task file with current state, observations, and handoff summary.

---
description: Start implementation of an assigned task using the implementer role and project workflow.
agent: implementer
subtask: true
---
Implement the assigned task described in `$ARGUMENTS`.

Before changing code:

1. load the active epic context
2. confirm branch/task scope from the assigned planning task file
3. restate acceptance criteria

Then execute the task following project rules and return a concise readiness-for-review summary.

Before returning:

1. run the repo-native validation and integration-style checks that are feasible for the task
2. prefer committing the reviewed work on the assigned branch before official review
3. update the runtime task file with current state, observations, validation run, branch HEAD or reviewed SHA, and handoff summary
4. state clearly whether the task is ready for official review against a committed SHA or only a working tree

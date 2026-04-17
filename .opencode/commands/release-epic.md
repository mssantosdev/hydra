---
description: Execute the release checklist for the active epic and prepare publication steps.
agent: merger
subtask: true
---
Read the active epic release plan and produce the exact release workflow for the current state.

Confirm all tasks defined in `tasks.md` are either approved and merged or intentionally excluded from the release, using the runtime task files and `planning/workspace-sync.md` for final state checks.

Do not recommend merging to `master`, tagging, pushing, or publishing unless the user explicitly requested release execution.

Include:

1. latest tag check
2. next tag recommendation
3. merge prerequisites
4. test and manual validation checklist
5. install/update steps after release

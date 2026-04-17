# Agent Assignments

## Manager

- role: `manager`
- responsibility: orchestrate task assignment, track readiness, delegate review and merge work

## Implementers

- role: `implementer`
- branch examples:
  - `feat/shell-completion-foundation`
  - `feat/cli-version-visibility`
  - `feat/new-bootstrap-hardening`

## Sub-Implementers

- role: `sub-implementer`
- used only when an implementer breaks a lane into smaller focused subtasks

## Reviewer

- role: `reviewer`
- responsibility: approve or request changes with actionable guidance

## Checkpoint Reviewer

- role: `checkpoint-reviewer`
- responsibility: advisory fast review for implementer or sub-implementer checkpoints only

## Merger

- role: `merger`
- branch: `dogfood/integration-v0.0.16`
- responsibility: integrate approved work and handle release flow

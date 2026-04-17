# T3 New Bootstrap Hardening

## Metadata

- Task ID: `T3`
- Epic: `v0.0.16-shell-bootstrap-foundation`
- Branch: `feat/new-bootstrap-hardening`
- Owner role: `implementer`
- Status: `assigned`
- Reviewer state: `pending`
- Merger state: `pending`
- Last updated: `2026-04-17`

## Objective

Harden `hydra new` so it reliably bootstraps a new Hydra project for local-first workflows.

## Included Scope

1. ensure `hydra new` is correctly treated as a startup/bootstrap command in root command flow
2. keep project path semantics:
   - relative path
   - nested relative paths allowed
   - no upward escape
   - no absolute paths
3. keep source modes:
   - create local repo
   - clone remote repo
4. validate name/path rules consistently:
   - project path may contain `/`
   - alias/group/repo dir are names, not paths
5. ensure local bootstrap creates a usable initial repository state
6. keep printed `cd` and next-step hints useful
7. update `hydra new` docs/help touched by this task

## Excluded Scope

1. importing an arbitrary existing local repo as source/remote
2. local-only file tracking
3. shell/completion architecture
4. version visibility

## Acceptance Criteria

1. `hydra new` works without requiring an existing Hydra config
2. nested relative project paths like `test/test-123` are allowed
3. absolute paths are rejected
4. upward escape like `../foo` is rejected
5. local bootstrap creates:
   - `.hydra.yaml`
   - `.bare/`
   - local repo
   - initial branch
   - initial worktree/symlink
6. remote bootstrap reuses clone flow correctly inside the new project root
7. help/docs reflect actual behavior
8. tests pass

## Dependencies

- watch for docs/help overlap with `T1`

## Current State

- current status: `assigned`
- current owner: `implementer`
- current blocker: none
- next expected action: implement bootstrap hardening in the assigned branch
- latest reviewer decision: none

## Observations / Comments

- empty

## Decision Log

- project path remains relative and may contain `/`
- alias/group/repo dir remain names, not paths

## Review History

- none yet

## Handoff Summary

- implementer should update readiness details and any bootstrap edge cases discovered before review

## Completion Notes

- not completed yet

# Constitution

## Mission

Hydra should be developed using a predictable multi-agent workflow that favors branch isolation, explicit review, and controlled integration.

## Durable Roles

- `manager` orchestrates epics, tasks, and delegation
- `implementer` executes assigned work on an owned branch/worktree
- `sub-implementer` executes smaller delegated subtasks for an implementer
- `reviewer` approves or requests changes with actionable guidance
- `checkpoint-reviewer` provides advisory fast review before official review
- `merger` integrates approved work and handles release flow

## State Model

Tasks move through these states:

- `pending`
- `assigned`
- `in_progress`
- `in_review`
- `changes_requested`
- `approved`
- `merged`
- `released`

## Review Loop

- Rejected reviews go back to the assigned implementer.
- Reviewer must return findings, required changes, guidance, and validation expectations.
- Reviewer does not directly implement fixes.
- Only approved work can be passed to the merger.
- Manager is the role that hands approved work to the merger.
- Checkpoint review is advisory only; it does not replace official review.
- Official review should target a committed branch HEAD SHA whenever practical.
- Reviewer records the reviewed SHA in runtime state.

## Delegation Discipline

- Implementers may delegate focused subtasks to `sub-implementer`.
- Parent implementer remains the owner of the full task.
- Sub-implementers may request checkpoint validation from `checkpoint-reviewer`.
- Only the parent implementer submits a task for official review.

## Branch Discipline

- One implementation concern per branch.
- Implementers do not work directly on `master`.
- Integration happens through an integration branch or the merger's designated flow.
- Branch names and ownership must be documented in the active epic.

## Conflict Discipline

- Prefer localized changes and helper extraction over editing shared hotspots.
- If overlap across branches becomes necessary, record it in the epic docs before proceeding.
- Large docs cleanup should follow behavior stabilization when possible.

## Validation Discipline

- Implementers validate their own branch before requesting review.
- Reviewer checks task acceptance criteria and project standards.
- Merger reruns integration validation after merging approved work.
- Repo-native validation commands and integration-style checks should be used where feasible.
- Runtime coordination state must reflect current owner, approval state, and reviewed or approved SHA before the next role proceeds.

## Release Discipline

- Release work is owned by the merger role.
- Tagging, pushing release tags, local install updates, and shell-helper refreshes are part of the release checklist.
- Do not publish from implementation branches.

## Required Context

Every agent should load or read:

1. `AGENTS.md`
2. `planning/constitution.md`
3. the active epic docs

The active epic must define executable task scope in `tasks.md` before manager-led parallel delegation begins.

Per-task files under `tasks/` are the durable working memory for task state, review outcomes, observations, and handoff notes.

## Dogfooding Rule

When practical, validate Hydra workflows from a Hydra-managed dogfood workspace using isolated worktrees per implementation branch.

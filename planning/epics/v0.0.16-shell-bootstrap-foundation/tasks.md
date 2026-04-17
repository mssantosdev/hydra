# Task Ledger

This file is the quick-reference index for all active tasks in the epic. Detailed execution state lives in `tasks/`.

## Epic Objective

Release `v0.0.16` to establish the first durable Hydra-on-Hydra development foundation by improving:

1. shell/completion architecture
2. CLI version visibility
3. `hydra new` bootstrap reliability

## Epic-wide Success Criteria

1. Hydra supports maintainable shell integration and completion installation
2. Hydra exposes version information in CLI entrypoints
3. `hydra new` is reliable enough for local-first project bootstrap
4. all three lanes are reviewed and approved independently
5. the integrated result passes validation before release

## Epic-wide Exclusions

- local-only file tracking or copying like `.env`
- broader local repo import or adoption lifecycle
- broad docs cleanup outside touched behavior
- unrelated refactors outside the three lanes

## Task Index

| Task | Lane | Branch | Owner | Status | Reviewer | Merger | Summary | File |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `T1` | shell completion foundation | `feat/shell-completion-foundation` | implementer | assigned | pending | pending | shell integration and completion architecture | `tasks/T1-shell-completion-foundation.md` |
| `T2` | CLI version visibility | `feat/cli-version-visibility` | implementer | assigned | pending | pending | version output and help visibility | `tasks/T2-cli-version-visibility.md` |
| `T3` | new bootstrap hardening | `feat/new-bootstrap-hardening` | implementer | assigned | pending | pending | `hydra new` reliability and path validation | `tasks/T3-new-bootstrap-hardening.md` |

## Status Model

- `pending`
- `assigned`
- `in_progress`
- `in_review`
- `changes_requested`
- `approved`
- `merged`
- `released`
- `blocked`

## Review Contract

Reviewer must return one of:

- `approved`
- `changes_requested`

If changes are requested, reviewer must include:

- findings
- required changes
- guidance
- validation expectations

## Merge Gating

Merger only accepts a task when:

1. review state is `approved`
2. branch is integration-ready
3. shared-surface conflicts are identified when relevant
4. post-merge validation steps are defined

## Search And Reference Rule

Use this file for fast searching and status checks. Use the corresponding file under `tasks/` for persistent execution notes, review history, decisions, blockers, and handoff summaries.

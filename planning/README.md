# Planning Overview

This directory contains the permanent operating model and epic-by-epic execution plans for developing Hydra with Hydra itself.

## Permanent Files

- `constitution.md` — stable workflow rules across all epics

## Epic Folders

Each folder under `epics/` defines one implementation wave.

Recommended epic files:

- `README.md`
- `orchestration.md`
- `branches.md`
- `agents.md`
- `tasks.md`
- `tasks/`
- `merge-release.md`

## Task Tracking Standard

Use `tasks.md` as the fast-search task index for an epic.

Use the files under `tasks/` as the durable execution records for:

- current state
- observations/comments
- decisions
- review history
- handoff summary

This keeps parallel work persistent and compaction-proof across long agent sessions.

Epic lifecycle states:

- `draft`
- `active`
- `integrating`
- `released`
- `archived`

We keep epic folders flat and track state inside each epic `README.md`.

## Roles

The project uses these durable agent roles:

- `manager`
- `implementer`
- `sub-implementer`
- `reviewer`
- `checkpoint-reviewer`
- `merger`

Their reusable OpenCode definitions live under `.opencode/agents/`, while per-epic assignments live in `planning/epics/<epic-id>/agents.md`.

# Planning Overview

This directory contains the permanent operating model and epic-by-epic execution plans for developing Hydra with Hydra itself.

## Permanent Files

- `constitution.md` — stable workflow rules across all epics

## Epic Folders

Each folder under `epics/` defines one implementation wave.

Epic lifecycle states:

- `draft`
- `active`
- `integrating`
- `released`
- `archived`

We keep epic folders flat and track state inside each epic `README.md`.

## Roles

The project uses four durable agent roles:

- `manager`
- `implementer`
- `reviewer`
- `merger`

Their reusable OpenCode definitions live under `.opencode/agents/`, while per-epic assignments live in `planning/epics/<epic-id>/agents.md`.

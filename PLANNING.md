# Planning System

Hydra uses a project-root planning system to support multi-agent implementation, review, merge, and release workflows.

Structure:

- `planning/constitution.md` — permanent operating rules
- `planning/README.md` — how planning is organized
- `planning/epics/<epic-id>/` — one folder per active or historical epic

Each epic folder should contain:

- `README.md` — scope, status, target release
- `orchestration.md` — phases, dependencies, and execution flow
- `branches.md` — branch ownership and merge order
- `agents.md` — role assignments for the epic
- `merge-release.md` — validation, merge, and publish checklist

OpenCode runtime assets live in:

- `.opencode/agents/`
- `.opencode/commands/`
- `.opencode/skills/`

This system is designed to be extended by adding new epic folders without changing the constitution or agent roles unless the workflow itself evolves.

# Hydra Agent Rules

This repository uses OpenCode project rules, custom agents, skills, and commands to support multi-agent development with Hydra itself.

Read these files before starting any non-trivial task:

1. `planning/constitution.md`
2. `planning/README.md`
3. The active epic folder under `planning/epics/`

For executable task scope, acceptance criteria, and review gates, use the active epic's `tasks.md` and the detailed task files under `tasks/`.

Core operating rules:

- Work from the active epic and assigned branch/task, not from assumptions.
- Implementers stay within assigned scope and branch ownership.
- Reviewer approves or requests changes; rejected work returns to the implementer with guidance.
- Merger only accepts approved work and owns release execution.
- Do not release from an implementation branch.
- Prefer small helpers and isolated edits to reduce future merge conflicts.

Planning system entrypoint:

- `PLANNING.md`

OpenCode project assets:

- Custom agents: `.opencode/agents/`
- Custom commands: `.opencode/commands/`
- Skills: `.opencode/skills/`

When epic docs and code disagree, treat epic docs as workflow guidance and code/tests as source of truth for current behavior. Update docs when behavior changes.

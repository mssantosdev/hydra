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
- Implementers may delegate focused subtasks to `sub-implementer`, but remain task owners.
- Reviewer approves or requests changes; rejected work returns to the implementer with guidance.
- `checkpoint-reviewer` is advisory only and does not replace official review.
- Merger only accepts approved work and owns release execution.
- Do not release from an implementation branch.
- Prefer small helpers and isolated edits to reduce future merge conflicts.
- Use `coordination/` as the live operational state layer; do not rely on chat summaries alone when runtime state is stale.
- Official review should target a committed branch HEAD SHA whenever practical.
- Review, merge, and release handoffs must record the reviewed or approved SHA in runtime state.
- Agents handling implementation, review, and integration should run repo-native validation commands where feasible, not only narrow unit tests.
- Language-specific validation commands are part of agent bash permissions, not LSP configuration.

Planning system entrypoint:

- `PLANNING.md`

OpenCode project assets:

- Custom agents: `.opencode/agents/`
- Custom commands: `.opencode/commands/`
- Skills: `.opencode/skills/`

When epic docs and code disagree, treat epic docs as workflow guidance and code/tests as source of truth for current behavior. Update docs when behavior changes.

# T1 Shell Completion Foundation

## Metadata

- Task ID: `T1`
- Epic: `v0.0.16-shell-bootstrap-foundation`
- Branch: `feat/shell-completion-foundation`
- Owner role: `implementer`
- Status: `assigned`
- Reviewer state: `pending`
- Merger state: `pending`
- Last updated: `2026-04-17`

## Objective

Create a maintainable shell integration architecture and first-class completion flow.

## Included Scope

1. add `hydra completion <shell>` for `bash`, `zsh`, and `fish`
2. refactor `hydra init-shell` to use generated files instead of large inline shell blocks
3. install helper through a small loader block in shell rc
4. support completion installation from `init-shell`
5. add `--with-completion` and `--without-completion`
6. if neither flag is provided, prompt whether to install completion too
7. preserve current `hydra switch` shell handoff behavior
8. preserve `noclobber` safety
9. update shell-related docs/help touched by this task

## Excluded Scope

1. version flag/help work
2. `hydra new` behavior changes
3. local file management

## Design Decisions

1. generated shell assets live under `~/.config/hydra/shell/`
2. shell rc contains one small loader block only
3. main generated shell file sources completion file if present
4. prompt default for completion install is `Yes`

## Acceptance Criteria

1. `hydra completion bash` prints a valid completion script
2. `hydra completion zsh` prints a valid completion script
3. `hydra completion fish` prints a valid completion script
4. `hydra init-shell` installs a small loader block, not a giant inline block
5. generated shell files are written under `~/.config/hydra/shell/`
6. `hydra init-shell --with-completion` installs helper and completion
7. `hydra init-shell --without-completion` installs helper only
8. `hydra init-shell` prompts when no completion flag is provided
9. `hydra switch <worktree>` still auto-cds correctly
10. the `noclobber` bug does not regress
11. shell docs/help match behavior
12. tests pass

## Dependencies

- watch for shared help-surface overlap with `T2`
- watch for shell-doc overlap with `T3`

## Current State

- current status: `assigned`
- current owner: `implementer`
- current blocker: none
- next expected action: implement shell/completion foundation in the assigned branch
- latest reviewer decision: none

## Observations / Comments

- empty

## Decision Log

- use generated shell files under `~/.config/hydra/shell/`
- keep one minimal loader block in shell rc

## Review History

- none yet

## Handoff Summary

- implementer should work from this file and update current state, observations, and review readiness summary before handing off to reviewer

## Completion Notes

- not completed yet

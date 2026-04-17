# Orchestration

## Phases

1. establish project planning and OpenCode framework
2. implement shell/completion foundation
3. implement CLI version visibility
4. harden `hydra new`
5. review and integrate approved work
6. release

## Parallelization

Up to three implementers can work in parallel when each owns a separate branch/worktree and the task boundaries are respected.

## Review Flow

1. implementer marks task ready for review
2. reviewer returns `approved` or `changes_requested`
3. rejected work returns to the same implementer branch
4. merger only accepts approved work

## Integration Rule

Merge branches in documented order and validate after each merge when a branch touches shared surfaces like shell integration, root help, or bootstrap logic.

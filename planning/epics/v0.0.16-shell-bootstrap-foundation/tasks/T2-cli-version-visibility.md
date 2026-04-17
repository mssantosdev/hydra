# T2 CLI Version Visibility

## Metadata

- Task ID: `T2`
- Epic: `v0.0.16-shell-bootstrap-foundation`
- Branch: `feat/cli-version-visibility`
- Owner role: `implementer`
- Status: `assigned`
- Reviewer state: `pending`
- Merger state: `pending`
- Last updated: `2026-04-17`

## Objective

Expose Hydra version information clearly in command-line entrypoints.

## Included Scope

1. add `hydra --version`
2. show version in `hydra`
3. show version in `hydra --help`
4. add build-time version metadata support
5. ensure local/dev fallback values work when build metadata is absent
6. update help/docs touched by this task

## Excluded Scope

1. shell/completion architecture
2. `hydra new` changes
3. local file management
4. release automation changes beyond version display support

## Acceptance Criteria

1. `hydra --version` prints version successfully
2. `hydra` output includes visible version information
3. `hydra --help` includes visible version information
4. local/dev builds still produce sensible output without injected release metadata
5. docs/help reflect the behavior
6. tests pass

## Dependencies

- watch for root help overlap with `T1`

## Current State

- current status: `in_review`
- current owner: `implementer`
- current blocker: none
- next expected action: reviewer validation
- latest reviewer decision: none

## Observations / Comments

- root command now renders version info in help/default output and supports `--version`.

## Decision Log

- help output should remain concise even when version is added

## Review History

- none yet

## Handoff Summary

- Implemented version visibility, default output/help version text, and dev fallback support. Validation passed with `go test ./...` and `go run . --version` / `go run . --help`.

## Completion Notes

- not completed yet

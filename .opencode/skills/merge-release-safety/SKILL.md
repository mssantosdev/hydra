---
name: merge-release-safety
description: Execute Hydra integration and release steps safely with tag, install, and shell-update validation.
---
## Merge rules

- only merge approved work
- follow epic merge order
- validate after integrating shared-surface changes
- use runtime task files to confirm approval state and handoff notes
- require an approved reviewed SHA in runtime state before merging
- verify the approved SHA exists on the implementation branch
- record any integration-only fixes separately from lane commits

## Release rules

1. check latest tag first
2. determine next release tag
3. push branch and tag only when requested
4. update the active installed binary path
5. rerun `hydra init-shell` if shell behavior changed
6. validate the installed binary and shell behavior

## Integration validation

- run the repo's integration test suite, for example `go test ./...`, after integration
- run built-binary or repo-native command validation for touched CLI surfaces when feasible
- explicitly note what was not practical to validate directly, such as real-shell evidence or environment-trust limitations

## Use when

Use this for integration branches, release prep, publishing, and local binary update workflows.

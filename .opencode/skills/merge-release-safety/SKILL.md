---
name: merge-release-safety
description: Execute Hydra integration and release steps safely with tag, install, and shell-update validation.
---
## Merge rules

- only merge approved work
- follow epic merge order
- validate after integrating shared-surface changes

## Release rules

1. check latest tag first
2. determine next release tag
3. push branch and tag only when requested
4. update the active installed binary path
5. rerun `hydra init-shell` if shell behavior changed
6. validate the installed binary and shell behavior

## Use when

Use this for integration branches, release prep, publishing, and local binary update workflows.

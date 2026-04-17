# Merge And Release

## Preconditions

- all merged tasks must be marked `approved`
- implementation branches must be rebased or otherwise integration-ready
- full test suite must pass on the integration branch before release

## Validation Checklist

- run `go test ./...`
- validate `hydra switch` in a real shell if shell integration changed
- validate `hydra init-shell` if shell behavior changed
- validate `hydra new` for project bootstrap changes
- check help/docs for touched commands

## Release Checklist

1. check latest tag first
2. merge integration branch to `master`
3. create release tag
4. push branch and tag
5. update the active local binary path
6. rerun `hydra init-shell` if shell assets changed
7. validate installed binary and shell behavior

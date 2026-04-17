# Branch Plan

Baseline branches:

- `master`
- `dogfood/integration-v0.0.16`

Implementation branches:

- `feat/shell-completion-foundation`
- `feat/cli-version-visibility`
- `feat/new-bootstrap-hardening`

Merge order:

1. `feat/shell-completion-foundation`
2. `feat/cli-version-visibility`
3. `feat/new-bootstrap-hardening`
4. integration cleanup/docs alignment on `dogfood/integration-v0.0.16`
5. merge integration branch to `master`
6. tag and release

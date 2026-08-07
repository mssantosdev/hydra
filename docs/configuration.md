---
title: "Configuration"
description: "Hydra .hydra/config.yaml schema v2 specification"
ai_context: "Complete .hydra/config.yaml specification for schema version 2"
---

# Configuration

Hydra uses a `.hydra/config.yaml` file in your project root (schema version `"2"`). Hydra walks up from the current directory until it finds one; there is no migration path from older schemas — re-create the workspace with `hydra init` if needed.

## Schema overview

```yaml
version: "2"
project: my-project

paths:
  bare_dir: ".bare"

groups:
  <group>:
    <alias>:
      remote: <git-url>
      default_branch: main   # optional

defaults:
  base_branch: ""           # optional

hooks:
  post_clone: []
  post_add: []
  pre_remove: []
  post_remove: []
  post_sync: []
```

## Fields

### `version` (required)

| Type | Default | Description |
|------|---------|-------------|
| string | — | Must be exactly `"2"`. Any other value yields `config_version_unsupported` (exit 2). |

### `project` (optional)

| Type | Default | Description |
|------|---------|-------------|
| string | parent directory name | Logical project name registered in the global registry and exposed as `HYDRA_PROJECT` in hooks. |

### `paths.bare_dir` (optional)

| Type | Default | Description |
|------|---------|-------------|
| string | `.bare` | Project-relative directory holding bare repositories. Each repo is stored at `<bare_dir>/<alias>.git`. Git data only — never work in this directory directly. |

Hydra does not support a configurable worktree root path. Worktrees always live as real sibling directories under their group.

### `groups` (required)

| Type | Default | Description |
|------|---------|-------------|
| map of maps | `{}` | Top-level keys are **group** names (directory names such as `backend`, `frontend`). Each group maps **alias → repo**. |

#### Repo entry (`groups.<group>.<alias>`)

The map key **is** the repo alias. It is the single source of truth for:

- Bare path: `.bare/<alias>.git`
- Default-branch worktree directory: `<group>/<alias>/`
- Non-default worktree base name: `<alias>-<slug>`

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `remote` | string | — | Git remote URL for the repository. |
| `default_branch` | string | inferred from `origin/HEAD` | Branch name treated as the default worktree (`<group>/<alias>/` with no suffix). |
| `branches` | list of strings | — | The **declared shape**: the branches this repo keeps worktrees for. `hydra repo restore` creates these, so a manifest alone reproduces a workspace. Written by `repo add --branches` and `repo set --branches`; never by `hydra add` or `hydra start`, which is what keeps work-in-progress out of a committed file. |
| `branch_pattern` | string | inherits `defaults.branch_pattern` | Overrides the project pattern for this repo. |
| `branch_provider` | string | inherits `defaults.branch_provider` | Overrides the project provider for this repo. |
| `carry` | list | — | Files a new worktree needs that git ignores. See [`carry`](#carry-optional). |

Example:

```yaml
groups:
  backend:
    api:
      remote: git@github.com:org/my-api.git
      default_branch: main
    worker:
      remote: git@github.com:org/my-worker.git
```

On disk:

```text
.bare/api.git/
.bare/worker.git/
backend/api/           # default branch worktree
backend/worker/
backend/api-feat-x/    # feature branch worktree
```

### `defaults.base_branch` (optional)

| Type | Default | Description |
|------|---------|-------------|
| string | `""` (empty) | Project-wide fallback when creating a **new** branch without `--from`. Leaving empty is normal. |

**Base-branch resolution chain** (first match wins):

1. `--from` on `hydra add`
2. `defaults.base_branch`
3. The repo's `default_branch`
4. `origin/HEAD` from the bare repository

### `carry` (optional)

Files a new worktree needs that git will not bring: a `.env`, a dev certificate, a
`docker-compose.override.yml`. A fresh worktree has every tracked file and none of these, so it
cannot run until someone copies them by hand — per worktree, per repo, every time.

Declarable at the workspace (top level) and per repo. Resolution **appends**, workspace first, so a
workspace-wide certificate and a repo's own `.env` both apply. A later level naming the same
destination replaces that entry, which is how a repo changes *how* an inherited file arrives without
having to suppress it first.

Two forms, because the sources differ:

```yaml
carry:
  - from: .shared/dev-ca.pem     # a WORKSPACE path — no repo, no source worktree
    to: certs/ca.pem
    mode: link                   # copy (default) | link

groups:
  backend:
    api:
      remote: git@github.com:org/my-api.git
      carry:
        - .env                   # from the SOURCE WORKTREE of this repo
```

| Field | Type | Description |
|-------|------|-------------|
| *(bare string)* | string | Shorthand for `path:`. Copied from the same relative location in the source worktree. |
| `path` | string | Source **and** destination, relative to the worktree. Mutually exclusive with `from`/`to`. |
| `from` | string | Source path relative to the **workspace root**. |
| `to` | string | Destination inside the worktree. Defaults to `from`. |
| `mode` | string | `copy` (default) duplicates the file; `link` symlinks it, so one file is edited in one place. |

**The source worktree** for a bare entry is the worktree of the branch `--from` named when it has
one, otherwise the repo's default-branch worktree. Both are deterministic; hydra never searches other
worktrees, because searching is guessing.

**Carrying never overwrites.** A file already in the new worktree is left alone and reported as
`skipped`, so re-running a command is a genuine no-op and an edited `.env` is never clobbered.

**A missing source is a warning, never a failure.** On a fresh clone there is no source worktree, so
bare entries cannot be satisfied — `repo restore` and `apply` replay **structure, not secrets**. Only
`from:` entries, whose source is a fixed workspace path, survive a fresh machine. The warning names
the file so you know what to provide.

Paths may not escape: an absolute or `..`-containing `path`, `from` or `to` is **refused when the
manifest is parsed**, and every write is confined to the worktree by the kernel. A manifest is meant
to be shared, so it must not be able to write outside the workspace it describes.

### `hooks` (optional)

Declarative shell commands run at lifecycle events. Each event holds an ordered list of hook entries.

| Event | When it runs |
|-------|----------------|
| `post_clone` | After a repository is cloned into the project |
| `post_add` | After a worktree is created |
| `pre_remove` | Before a worktree is removed |
| `post_remove` | After a worktree is removed (cwd is project root) |
| `post_sync` | After a successful sync |

Hook entry:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `run` | string | — | Shell command executed via `sh -c`. |
| `optional` | bool | `false` | When `true`, a non-zero exit logs a warning and continues; when `false`, failure returns `hook_failed` (exit 1). |

**Injected environment variables** (every hook):

| Variable | Value |
|----------|-------|
| `HYDRA_EVENT` | Event name |
| `HYDRA_PROJECT` | Project name from config |
| `HYDRA_PROJECT_ROOT` | Absolute workspace root |
| `HYDRA_GROUP` | Group name |
| `HYDRA_REPO` | Repo alias |
| `HYDRA_BRANCH` | Branch name |
| `HYDRA_WORKTREE_PATH` | Absolute worktree path |
| `HYDRA_BARE_PATH` | Absolute bare repository path |

**Important:** a failing hook never rolls back work Hydra already completed. For example, a failed `post_add` hook does not remove the worktree — fix the hook and run `hydra hooks run post_add`.

Pass `--no-hooks` on any command to skip every configured hook.

Example:

```yaml
hooks:
  post_add:
    - run: npm install
  post_sync:
    - run: echo "synced"
      optional: true
```

## Worktree directory naming

Worktrees are **real directories** under `<group>/`, never symlinks.

| Case | Directory |
|------|-----------|
| Default branch for the repo | `<group>/<alias>/` |
| Any other branch | `<group>/<alias>-<slug>/` |
| Branch slug | `/` and path separators become `-` (case preserved) |
| Override | `hydra add … --as <name>` uses `<group>/<name>/` instead |

Examples:

| Branch | Alias | Directory |
|--------|-------|-----------|
| `main` (default) | `api` | `backend/api/` |
| `feature/login` | `api` | `backend/api-feature-login/` |
| `hotfix/urgent` | `api` | `backend/api-hotfix-urgent/` |

Always read the actual path from `hydra path` or `data[].path` in JSON output — do not reconstruct paths by hand when `--as` may apply.

## Global configuration

User-wide settings (theme, editor) live outside the project:

| Platform | Path |
|----------|------|
| Linux | `~/.config/hydra/config.yaml` |
| macOS | `~/Library/Application Support/hydra/config.yaml` |
| Windows | `%APPDATA%/hydra/config.yaml` |

Edit interactively with `hydra config`.

## Global project registry

Hydra registers workspace roots by project name in:

```text
<config-dir>/projects.yaml
```

where `<config-dir>` is the same directory as `config.yaml` above. Override the entire config directory (including the registry) with:

```bash
export HYDRA_CONFIG_DIR=/path/to/config
```

Manage entries with `hydra project ls`, `hydra project add`, and `hydra project rm`. Use `hydra --project <name>` to target a registered workspace from anywhere.

## Example configurations

### Minimal

```yaml
version: "2"
project: my-app
groups:
  default:
    app:
      remote: git@github.com:org/my-app.git
```

### Multi-service

```yaml
version: "2"
project: platform

paths:
  bare_dir: ".bare"

groups:
  backend:
    api:
      remote: git@github.com:org/my-api.git
      default_branch: main
    worker:
      remote: git@github.com:org/my-worker.git
  frontend:
    web:
      remote: git@github.com:org/my-web.git

defaults:
  base_branch: ""

hooks:
  post_add:
    - run: make setup
```

## Validation

Hydra validates configuration on load. Common failures:

| Error code | Cause | Fix |
|------------|-------|-----|
| `config_version_unsupported` | `version` is not `"2"` | Re-create with `hydra init` |
| `not_in_project` | No `.hydra/config.yaml` in tree | Run `hydra init` or `cd` to project root |

```bash
hydra doctor --output json
```

## See Also

- [Commands](./commands/README.md) — Command reference
- [Configuration example](../hydra.config.yaml.example) — Annotated template
- [skills/hydra/SKILL.md](../skills/hydra/SKILL.md) — Agent contract and error codes

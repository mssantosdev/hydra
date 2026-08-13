---
title: "Configuration"
description: "Hydra .hydra/config.yaml schema v3 specification"
ai_context: "Complete .hydra/config.yaml specification for schema version 3; version 2 loads and upgrades on write"
---

# Configuration

Hydra uses a `.hydra/config.yaml` file in your project root (schema version `"2"`). Hydra walks up from the current directory until it finds one; there is no migration path from older schemas — re-create the workspace with `hydra init` if needed.

## Schema overview

```yaml
version: "3"
project: my-project

paths:
  bare_dir: ".bare"

# Workspace level. Every level below carries the same three keys.
defaults:
  base_branch: ""            # optional
  branch_pattern: ""         # optional
hooks:
  post_clone: []
  post_add: []
  pre_remove: []
  post_remove: []
  post_sync: []
carry: []                    # optional

groups:
  <group>:
    path: <dir>              # optional; defaults to the group name
    defaults: {}             # optional
    hooks: {}                # optional
    carry: []                # optional
    repos:                   # NOTE: schema 3 nests repos under `repos:`
      <alias>:
        remote: <git-url>
        default_branch: main # optional
        branches: []         # optional; the declared shape restored by `repo restore`
        defaults: {}         # optional
        hooks: {}            # optional
        carry: []            # optional
```

Schema 3 moved repositories under a `repos:` key so a group can carry properties of its own. A
schema-2 manifest — where `groups.<group>.<alias>` was the repo directly — still loads and is
upgraded on the next write.

`defaults`, `hooks` and `carry` exist at all three levels. **Scalars resolve nearest-wins**
(repo beats group beats workspace); **lists append** (workspace, then group, then repo). What
belongs at which level is your modelling choice: grouping by language puts the toolchain hook on
the group, grouping by domain puts it on each repo. Both are correct.

## Fields

### `version` (required)

| Type | Default | Description |
|------|---------|-------------|
| string | — | `"3"` is current. `"2"` still loads and is upgraded on the next write. Anything else yields `config_version_unsupported` (exit 2), so a manifest written by a newer hydra is never half-read. |

### `project` (optional)

| Type | Default | Description |
|------|---------|-------------|
| string | parent directory name | Logical project name registered in the global registry and exposed as `HYDRA_PROJECT` in hooks. |

### `paths.bare_dir` (optional)

| Type | Default | Description |
|------|---------|-------------|
| string | `.bare` | Project-relative directory holding bare repositories. Each repo is stored at `<bare_dir>/<alias>.git`. Git data only — never work in this directory directly. |

`bare_dir` and every group `path:` must stay **inside the workspace**. An absolute path, or one
that climbs out with `..`, is refused with `config_invalid` (exit 2) before any command runs — a
manifest is a shared, committed file, so it must not be able to place files outside the directory
it lives in.

Hydra does not support a configurable worktree root path. Worktrees always live as real sibling directories under their group.

### `groups` (required)

| Type | Default | Description |
|------|---------|-------------|
| map of groups | `{}` | Top-level keys are **group** names. Each group is an object carrying its own `path`, `defaults`, `hooks`, `carry` and `repos`. |

#### Group entry (`groups.<group>`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `repos` | map | — | alias → repo. The alias is the single source of the bare path and the worktree base name. |
| `path` | string | the group name | Directory this group's worktrees live under, relative to the workspace root. Use it for nesting (`platform/infra`) — group **names** stay one segment, because a slash in the key makes selectors, completion and rename ambiguous while a path field does not. |
| `defaults` | object | — | Overrides the workspace defaults for every repo in this group. |
| `hooks` | object | — | Hook chains for this group's repos. |
| `carry` | list | — | Files every worktree in this group needs. See [`carry`](#carry-optional). |

**Resolution is workspace → group → repo.** Scalars are nearest-wins: a repo's `branch_pattern` beats
its group's, which beats the workspace's. An empty value means *inherit*, not *clear*. Lists —
`hooks` and `carry` — **append**, so a workspace-wide certificate and a repo's own `.env` both apply.

`branch_pattern_strict` is the exception, because a boolean has no *unset*: any level that sets it
turns strictness **on**, and no lower level can turn it back off. Escaping an inherited strict
pattern is therefore a deliberate edit at the level that set it, not something a child can do by
having no opinion. `hydra config show` prints the row only when it is on.

Run `hydra config show` to see the resolved value of every key and the level that supplied it,
rather than working the chain out by reading three places.

hydra does not decide what a group means. `go-projects`/`java-projects` and `backend`/`web` are
equally valid partitions: every level carries the same keys, the chain is the only rule, and what
belongs where is your modelling choice.

**Version 2 manifests still load.** Before schema 3 a group mapped straight to its repositories, with
nowhere to put the group's own settings. Such a manifest is renested in memory and written back as
version 3 on the next mutation, comments included — no migration command, and the upgrade shows up as
a diff in a file you had already committed.

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
    repos:
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
    repos:
      api:
        remote: git@github.com:org/my-api.git
        carry:
          - .env                 # from the SOURCE WORKTREE of this repo
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

**Carry what git ignores.** A carried file that is *not* in `.gitignore` is an untracked file like
any other, so the worktree reports `dirty` the moment it is created and `hydra remove` refuses it
until you commit, stash or `--force`. That is not hydra being surprising — the file genuinely is
untracked — but it is why `carry` is for `.env` and friends rather than for anything git could have
brought itself.

### `hooks` (optional)

Declarative shell commands run at lifecycle events. Each event holds an ordered list of hook entries.

| Event | When it runs |
|-------|----------------|
| `post_clone` | After a repository is cloned into the project |
| `post_add` | After a worktree is created |
| `pre_remove` | Before a worktree is removed |
| `post_remove` | After a worktree is removed (cwd is project root) |
| `post_sync` | After a successful sync |
| `post_topic_start` | **Once**, after `start --topic` created its worktrees |
| `pre_topic_close` | **Once**, before a topic is closed — the only place a check can veto finishing a unit of work |
| `post_topic_close` | **Once**, after a topic is closed |
| `pre_topic_remove` | **Once**, before the first member of a topic is touched |

Hook entry:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `run` | string | — | Shell command executed via `sh -c`. |
| `optional` | bool | `false` | When `true`, a non-zero exit logs a warning and continues; when `false`, failure returns `hook_failed` (exit 1). |
| `timeout` | duration | `10m` | Go duration (`30s`, `5m`) bounding this hook. `0` disables the bound. A hook that outlives it is killed and reported as `timed out after <d>`. |

Hooks used to be the one unbounded wait in hydra — the state lock is 5s, the manifest lock 10s,
`run` takes `--timeout` — so a hook that hung hung the tool, worst on an instance bootstrap whose own
deadline is minutes. The default is generous because a dependency install on a cold cache genuinely
takes minutes; a bound that fires during normal work would be worse than none.

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
| `HYDRA_TOPIC` | Topic this worktree belongs to, empty when none |
| `HYDRA_SOURCE_WORKTREE` | Worktree a new one was derived from, empty when none |

`HYDRA_TOPIC` and `HYDRA_SOURCE_WORKTREE` are always exported, even empty, so a hook running under
`set -u` can test them without aborting on an unset variable. Use `HYDRA_SOURCE_WORKTREE` rather than
rebuilding `<root>/<group>/<repo>` — `--as` can override the derived name.

The four topic events fire **once per operation**, not once per worktree. Wiring a notification to
`post_add` posts one message per created worktree for a single piece of work; the fix is not a
`run_once:` flag — that papers over a modelling error — but an event whose shape matches the thing it
reports. `HYDRA_TOPIC` is set for all four; `HYDRA_REPO`, `HYDRA_BRANCH` and `HYDRA_WORKTREE_PATH` are
empty, because a topic-level event has no single one and inventing one would make the hook look
per-worktree.

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
version: "3"
project: my-app
groups:
  default:
    repos:
      app:
        remote: git@github.com:org/my-app.git
```

### Multi-service

Two teams with different release flows, mixed toolchains, and a shared local certificate. Note
where each thing sits: the branching convention is a group property because the team shares it;
the toolchain is a repo property because `api` is Go and `worker` is Rust.

```yaml
version: "3"
project: platform

paths:
  bare_dir: ".bare"

carry:
  - from: .shared/dev-ca.pem     # every worktree in every repo needs the internal CA
    to: certs/ca.pem

groups:
  backend:
    path: services               # worktrees land under services/, not backend/
    defaults:
      base_branch: develop       # this team cuts features from develop
    repos:
      api:
        remote: git@github.com:org/my-api.git
        default_branch: main
        branches: [main, stage, prod]
        carry: [.env]
        hooks:
          post_add:
            - run: go mod download
      worker:
        remote: git@github.com:org/my-worker.git
        branches: [main]
        hooks:
          post_add:
            - run: cargo fetch

  frontend:
    repos:
      web:
        remote: git@github.com:org/my-web.git
        branches: [main, stage]
        carry: [.env.local]

hooks:
  post_add:
    - run: direnv allow          # true for every repo, so it lives at the workspace
```

`hydra repo restore` on a fresh machine rebuilds every repo above and the branches each declares.
Bare-form `carry` entries have no source on a fresh clone and warn instead of failing; the
`from:`-form entry above works, because the file lives in the workspace rather than in a worktree.

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

- [Visual guide](https://mssantosdev.github.io/hydra/) — the published walkthrough, with every
  terminal panel copied from a real run
- [Commands](./commands/README.md) — Command reference
- [Configuration example](../hydra.config.yaml.example) — Annotated template
- [skills/hydra/SKILL.md](../skills/hydra/SKILL.md) — Agent contract and error codes

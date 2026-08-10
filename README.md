# Hydra 🐍

`hydra` manages git worktrees across many repositories — from your terminal or through AI agents.

Name a repo and a branch from anywhere in the workspace and hydra places the worktree, registers it,
and every other command finds it. One name then addresses a whole set of worktrees across
repositories: create it, run across it, sync it, remove it. It never merges, never rebases, never
resolves a conflict, and never talks to a forge.

[![Go Report Card](https://goreportcard.com/badge/github.com/mssantosdev/hydra)](https://goreportcard.com/report/github.com/mssantosdev/hydra)

## Features

- 🌿 **Worktree management**: Create, switch, and remove git worktrees across many repos
- 🏗️ **Groups**: Partition a workspace by whatever you like — domain, team, or language — with their own layout, defaults and hooks
- 📐 **Declared shape**: `branches:` in the manifest records which branches each repo keeps, so `hydra repo restore` rebuilds a workspace from that file alone
- 📌 **Topics**: A unit of work spanning repositories, with optional parent/child nesting and a `topic close` gate that checks children are merged first
- 🚀 **Multi-repo start**: `hydra start` creates one branch across several repos in one command
- 📦 **carry**: Bring the files git ignores — `.env`, dev certs — into every new worktree
- ▶️ **Fan-out execution**: `hydra run` runs one command per worktree with no implicit shell
- 📋 **Workspace replay**: `hydra apply` takes a file or stdin and reproduces a captured workspace
- 🪝 **Hooks at every level**: Nine events, resolved workspace → group → repo, with four that fire once per topic operation instead of once per worktree
- 🎨 **Themes**: Inherits your terminal's own colours by default; `hydra`, Tokyo Night, Catppuccin, Dracula, Nord and One Dark are all selectable
- 🔧 **Shell integration**: Automatic directory switching with `hydra switch`
- 🤖 **Machine-readable output**: JSON envelopes with stable `error.code` values, and creation is convergent — `add`, `start` and `apply` re-run as no-ops that exit 0. Removing something already gone is an error, not a silent success

## Installation

```bash
go install github.com/mssantosdev/hydra@latest
```

Or clone and build:

```bash
git clone https://github.com/mssantosdev/hydra.git
cd hydra
go build -o hydra .
```

`go build` produces a binary that reports `dev`. A released build stamps the version
through ldflags, which `make` wires up:

```bash
make build VERSION=v0.0.17 COMMIT=$(git rev-parse --short HEAD)
./hydra --version   # v0.0.17 <commit>
```

`make gate` runs the full quality gate: `gofmt`, `go vet`, `golangci-lint` (version-pinned),
`govulncheck`, tests with coverage, and the race detector. Individual steps are
available as `make gate-fmt`, `gate-vet`, `gate-lint`, `gate-vuln`, `gate-test`, `gate-race`.

## Quick Start

### 1. Initialize Hydra

```bash
cd ~/projects/my-project
hydra init
```

This creates `.hydra/config.yaml` configuration file.

### 2. Setup Shell Integration (Recommended)

`hydra init-shell` installs the shell helper by default: it writes a loader block into your shell rc file **and** prints a human success message to stdout. Do **not** redirect the default command into your rc file.

```bash
# Install helper and loader block (default)
hydra init-shell
source ~/.bashrc   # or your shell rc file
```

To capture only the raw loader snippet for manual installation:

```bash
hydra init-shell --install=false >> ~/.bashrc
source ~/.bashrc
```

Generated shell assets live under `~/.config/hydra/shell/`. Use `hydra completion bash|zsh|fish` if you want the completion script directly.

### 3. Add a Worktree

```bash
# Interactive mode
hydra add

# Or direct
hydra add api feature/new-endpoint
```

### 4. Switch Between Worktrees

```bash
# Prints the worktree path and exits 0 (works without shell helper)
hydra switch api-feature-new-endpoint

# With shell helper installed — automatically changes directory
hydra switch api-feature-new-endpoint

# Requires shell helper; fails with exit 3 if missing
hydra switch api-feature-new-endpoint --cd
```

For scripts and agents, use `hydra path <worktree>` instead of `switch`.

### 5. List All Worktrees

```bash
hydra list
# alias: hydra ls
```

### 6. Start a Unit of Work Across Repositories

```bash
hydra start <branch> --repos a,b --topic <id>
hydra list --topic <id>
```

## On-Disk Layout

Hydra keeps bare git data separate from real working directories. Worktrees are **sibling directories**, never symlinks:

```text
<project-root>/
  .hydra/config.yaml     # shared manifest
  .bare/
    api.git/              # git data only — never cd into or write here
    web.git/
  backend/
    api/                  # default-branch worktree for alias api
    api-feat-login/       # branch feat/login  →  api-feat-login
  frontend/
    web/
    web-hotfix-urgent/
```

The map key under each group **is** the repo alias. It determines both `.bare/<alias>.git` and the worktree directory base name.

## Documentation

Complete documentation is in [`docs/`](docs/):

- **[Visual guide](https://mssantosdev.github.io/hydra/)** ([source](docs/guide.html)) — a single self-contained page covering install, the model,
  the interactive routes, and the contract scripts and agents rely on. Every terminal panel in it is verbatim output
  from a real run. Open it directly in a browser; it makes no external requests.
- **[Getting Started](docs/README.md)** — Overview and quick start
- **[Commands](docs/commands/README.md)** — Complete command reference
  - [Worktree Management](docs/commands/worktree-management.md) — `add`, `remove`
  - [Topics and execution](docs/commands/topics-and-execution.md) — topics, `start`, `run`, and `apply`
  - [Project Bootstrap](docs/commands/project-bootstrap.md) — `new`, `init`, `repo add`, `repo add --adopt`
- **[Configuration](docs/configuration.md)** — `.hydra/config.yaml` schema v2
- **[CHANGELOG.md](CHANGELOG.md)** — release notes, including breaking changes and how to migrate

For AI agents and automation, use the embedded skill contract:

- **[skills/hydra/SKILL.md](skills/hydra/SKILL.md)** — version-locked agent contract
- **`hydra skill`** — print the skill to stdout
- **`hydra skill --install`** — install the skill for your agent environment

## Example Configuration

`.hydra/config.yaml` (schema v2):

```yaml
version: "2"
project: my-project

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
  base_branch: ""   # usually leave empty; see configuration.md

hooks:
  post_add:
    - run: npm install
  post_sync:
    - run: echo "synced $HYDRA_BRANCH"
      optional: true
```

See [Configuration](docs/configuration.md) for every field, hook events, and the global project registry.

## Common Workflows

### Feature Development

```bash
# 1. Create feature worktree
hydra add api feature/JIRA-123

# 2. Switch to it (auto-cd when shell helper is installed)
hydra switch api-feature-JIRA-123

# 3. Do work...
git commit -m "feat: new feature"

# 4. Cleanup when done
hydra switch api
hydra remove api feature/JIRA-123
```

### Hotfix Production

```bash
# Create hotfix from prod branch
hydra add api hotfix/critical-bug --from=prod

# Fix and deploy
hydra switch api-hotfix-critical-bug
# ... fix ...
git push

# Cleanup, once the hotfix is merged into the default branch.
# --delete-branch is refused if the branch is not merged there yet, and nothing is
# removed in that case. Being pushed to origin does not count as merged.
hydra remove api hotfix/critical-bug --delete-branch
```

### Multi-Repo Topic

```bash
# Start a topic across two repos
hydra start feat/oauth --repos api,web --topic JIRA-456

# Check tracking status against main
hydra status --topic JIRA-456 --against main

# Run a command in each worktree (no implicit shell — everything after -- is argv)
hydra run --topic JIRA-456 -- go test ./...

# Tear down the topic and its worktrees
hydra topic remove JIRA-456 --with-worktrees
```

## Commands Overview

| Command | Description |
|---------|-------------|
| `hydra init` | Initialize Hydra in the current directory |
| `hydra new` | Bootstrap a new project and first repository |
| `hydra repo add <url>` | Clone a remote into a project |
| `hydra repo add --adopt` | Import an existing checkout into the current project |
| `hydra add [<repo> <branch>]` | Create a new worktree |
| `hydra remove [<repo> <branch>]` | Remove a worktree |
| `hydra path <worktree>` | Print a worktree's absolute path |
| `hydra switch [<worktree>]` | Print path; auto-cd with shell helper |
| `hydra status` | Full-screen interactive board on a TTY (`ui`/`tui` are aliases); `--output json` for agents |
| `hydra list` / `hydra ls` | List worktrees (`--all` includes every project) |
| `hydra sync [<alias>]` | Fast-forward worktrees from upstreams |
| `hydra doctor` | Diagnose workspace problems (`--fix`, `--all`) |
| `hydra prune` | Drop stale worktree registrations (`--dry-run`) |
| `hydra project` | Manage global registry (`ls` / `add` / `rm`) |
| `hydra hooks` | Inspect or run hooks (`ls` / `run <event>`) |
| `hydra config` | Manage global configuration (theme, editor) |
| `hydra init-shell` | Install shell integration |
| `hydra completion` | Emit shell completion script |
| `hydra skill` | Emit the agent skill (`--install`) |

## Machine-readable output

Hydra can emit structured JSON for scripting and agents.

### Output mode

| Mechanism | Behavior |
|-----------|----------|
| `--output auto` | Default. JSON when stdout is not a terminal; styled text in a TTY |
| `--output json` | Always JSON on stdout |
| `--output text` | Always human text |
| `HYDRA_OUTPUT` | Environment override for the default mode (`auto`, `json`, or `text`) |
| `NO_COLOR` | Disables color in text mode |

Two commands are value emitters rather than reporters, and are exempt from the
`auto` upgrade so shell substitution keeps working:

- `hydra path` prints a bare path on stdout. `cd "$(hydra path api)"` works as written;
  pass `--output json` explicitly to get the envelope with group, repo, and branch.
- `hydra skill` writes `SKILL.md` verbatim, so `hydra skill > SKILL.md` is never mangled.

Global flags also include `--project <name>`, `--config <path>`, `--verbose`, `--yes`, and `--no-hooks`.

### Success envelope

On success, stdout contains:

```json
{"schema":3,"command":"list","outcome":"success","summary":"2 worktree(s)","data":{},"warnings":[]}
```

- `outcome` is `success` or `partial`. A partial outcome still carries real `data`: the items that
  succeeded are reported even though the process also exits `4`.
- `summary` is the one-line answer, so a caller never has to reconstruct it from `data`.
- `next` appears only when there is a follow-up worth suggesting, and hydra never acts on it. It is
  named `next` rather than `breadcrumbs` because breadcrumbs mean where you came from.

### Error envelope

On failure, stderr contains:

```json
{"schema":3,"command":"add","outcome":"failure","error":{"code":"worktree_name_conflict","retryable":false,"message":"…","details":{}}}
```

Branch on `error.code`, not message text. Codes are stable; messages are not.

`retryable` is the one fact a caller cannot derive: the code-to-exit map is published, but "is it worth
trying again" is not inferable from either the code or the exit status. Only `busy` is retryable.

There is no `exit` field. The process exit status already carries it, and duplicating it in the payload
would create a second place that could disagree.

### Error codes and exit codes

| code | exit | raised when |
|------|------|-------------|
| `not_in_project` | 2 | no `.hydra/config.yaml` found walking up, and no `--project` |
| `config_version_unsupported` | 2 | `.hydra/config.yaml` `version` is not `"2"` |
| `project_unknown` | 2 | `--project <name>` not in the registry |
| `repo_unknown` | 1 | alias not present in any group |
| `bare_missing` | 1 | `<bare_dir>/<alias>.git` absent |
| `branch_unknown` | 1 | branch does not exist where an existing branch was required |
| `worktree_exists` | 1 | target worktree already exists for that branch |
| `worktree_unknown` | 1 | named worktree not found |
| `worktree_name_conflict` | 1 | a name does not identify exactly one worktree — a derived directory already taken, or an ambiguous handle |
| `worktree_dirty` | 5 | destructive op blocked by uncommitted changes |
| `hook_failed` | 1 | a non-`optional` hook exited non-zero |
| `shell_helper_missing` | 3 | `switch --cd` with no shell helper installed |
| `partial_failure` | 4 | some items succeeded, some failed |
| `git_failed` | 1 | an underlying git invocation failed |
| `topic_unknown` | 1 | `--topic <id>` is not recorded; `details.known` lists valid ids |
| `topic_conflict` | 1 | that worktree already belongs to another topic |
| `state_version_unsupported` | 2 | `.hydra/state.yaml` was written by a newer hydra |
| `branch_provider_failed` | 1 | a configured `branch_provider` failed or timed out |
| `busy` | 6 | a git or state lock was held — **the only retryable code** |
| `needs_input` | 7 | a value is missing and output is machine-readable; `details.missing` names the flag |
| `internal` | 1 | anything unclassified |

### Agent onboarding

```bash
hydra skill --install    # install the embedded skill for your agent
hydra skill              # print skills/hydra/SKILL.md to stdout
```

Always pass `--output json` (or rely on `--output auto` with a pipe) and parse the envelope. See [skills/hydra/SKILL.md](skills/hydra/SKILL.md) for the full contract.

## Themes

Hydra defaults to your terminal's own palette. Change with:

```bash
hydra config
# Select "Theme" and choose from:
# - terminal (default)  — inherits your terminal's palette; paints no background
# - hydra               — the one first-party palette
# - tokyonight
# - catppuccin
# - dracula
# - nord
# - onedark
```

`terminal` sets every colour to an ANSI slot rather than a hex value, so your terminal
resolves it from its own theme. That needs no OSC query and no config parsing, so it works
over SSH, inside tmux, and it follows a live theme change for free. Hydra paints no
background under it: the palette belongs to whoever configured the terminal.

Matching your terminal and tools to it:

```bash
python3 scripts/gen-themes.py --install
```

That regenerates `contrib/` from the palette and installs for the current user:

| target | path | activate |
|---|---|---|
| Ghostty | `~/.config/ghostty/themes/hydra` | `theme = hydra` in `~/.config/ghostty/config` |
| omp | `~/.omp/agent/themes/hydra.json` | `theme.dark: hydra` in `~/.omp/agent/config.yml` |

Both files are generated from `internal/ui/themes/themes.go`, never hand-written, and
`make gate` fails if they drift from it.

The `hydra` theme is not just another entry in the list: its role names (`Primary`,
`Success`, `Warning`, `Error`, …) are the same ones the HTML guide renders from, so the
tool and its documentation are one design system rather than two palettes that happen to
resemble each other. Both render on a neutral dark ground, and colour is spent only where it
carries meaning — a reference, a topic, a state.

## Shell Integration

The shell helper enables `hydra switch` to automatically change your directory when the helper is loaded:

```bash
hydra init-shell
source ~/.bashrc

hydra switch my-worktree   # auto-cd when helper is active
hsw my-worktree            # alias provided by the helper
```

Without the helper, `hydra switch` still prints the worktree path and exits 0. Only `hydra switch --cd` requires the helper (exit 3 when missing).

## Configuration

Project configuration lives in `.hydra/config.yaml` at the workspace root. Global user settings (theme, editor) are stored in:

- Linux: `~/.config/hydra/config.yaml`
- macOS: `~/Library/Application Support/hydra/config.yaml`
- Windows: `%APPDATA%/hydra/config.yaml`

The global **project registry** (`projects.yaml` in the same config directory) maps project names to workspace roots. Override the config directory with `HYDRA_CONFIG_DIR`.

Configure interactively:

```bash
hydra config
```

## AI Agent Usage

Hydra ships a single version-locked agent contract at [skills/hydra/SKILL.md](skills/hydra/SKILL.md). Install it with `hydra skill --install`, or read it with `hydra skill`. Use `--output json`, branch on `error.code`, and prefer `hydra path` over `hydra switch` in scripts.

## License

MIT License.

## Contributing

Contributions welcome! See [GitHub repository](https://github.com/mssantosdev/hydra).

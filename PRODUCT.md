# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

Single self-contained static HTML file. Fonts, styles, scripts, and images inline or data-URI, so the
document opens offline from `file://` or out of a zip with zero external requests. No framework, no
build step. Chosen by the user over a multi-page Pages site so the guide can be attached and handed
around directly.

## Users

Two audiences read the same artifact, and they read it differently.

**Developers** who work across more than one git repository at a time — a backend, a frontend, and
an infra repo that move together for one piece of work. They live in a terminal, already know git
worktrees exist, and have usually been managing them by hand with a directory convention and
memory. They arrive wanting to know what this replaces and whether it will lose their work.

**AI coding agents** driving hydra as a subprocess. They cannot see pixels, do not read prose for
instruction, and need the output contract: the JSON envelope shape, the stable error codes, the exit
statuses, and which commands mutate. They arrive through their operator, who pastes something.

The guide's job is onboarding both from one document. The developer scrolls; the agent parses.

## Product Purpose

hydra manages git worktrees across several repositories at once. `git worktree` already handles
many worktrees in a single repo perfectly well; what it does not do is cross the repo boundary, so
nothing ties worktrees in three separate repos together as one piece of work. hydra records that
grouping (a *topic*), creates a worktree per repo/branch pair under the same layout every time,
reports their combined state, and removes the whole set together.

Success is that a developer stops hand-managing worktree directories and stops losing track of
which branch in which repo belongs to which piece of work — and that an agent can drive the same
operations without a human interpreting the output.

## Positioning

Two things a neighbouring tool could not truthfully copy:

**A deliberately narrow remit.** hydra owns identity, layout, freshness, and reporting. It never
merges, never rebases, never resolves a conflict, and never talks to a forge. Those are git's and
the developer's. This is why it can be trusted in an agent loop: the destructive operations are not
in its vocabulary.

**A verdict that cannot lie.** Every command emits exactly one envelope on stdout. `success` is
structurally impossible to claim beside a failure or a workspace-integrity warning, and the process
exit status is derived from the envelope that was actually emitted rather than from what the command
returned. A command cannot report `all clean` while a worktree is missing, and cannot exit 0 after
emitting `partial`. This was not designed up front — it was arrived at by an adversarial test loop
that found the same class of bug in five consecutive releases, and closed it at both boundaries.

## Operating Context

- A terminal, on Linux, macOS, or Windows. Distributed as a single Go binary via `go install`;
  builds with `CGO_ENABLED=0`, so no toolchain is required on the target.
- Alongside git. hydra reads real git state on every query — `git worktree list --porcelain`,
  `rev-list` — and caches none of it.
- Optional shell integration, because a child process cannot change its parent's directory:
  `hydra switch` prints a path and exits 0 without the helper, and changes directory with it.
- Inside agent harnesses, invoked with `--output json`. `hydra skill` prints an embedded agent
  contract; `hydra commands --output json` enumerates the whole surface, including every error code
  and its exit status.
- Against remotes that are frequently slow or SSH-gated (Azure DevOps in the author's case), which
  is why concurrency and interrupted-clone recovery are load-bearing rather than theoretical.

## Capabilities and Constraints

- 40 commands. Core nouns: `project` (a workspace), `repo` (a registered repository), worktrees
  (`add`, `list`, `status`, `switch`, `remove`, `where`, `path`), `topic` (the multi-repo unit of
  work: `attach`, `detach`, `list`, `show`, `remove`), and execution (`run`, `sync`, `apply`).
  Recovery: `doctor`, `prune`, `repo restore`. Integration: `hooks`, `skill`, `commands`,
  `completion`, `init-shell`. `doctor`'s check count is not a constant: it scales with registered
  repos and worktrees (57 on a 3-repo/8-worktree workspace, 67 on a 5-repo/9-worktree one), so any
  figure quoted must name the workspace it came from.
- **An interactive surface is a first-class route, not a convenience.** hydra ships `huh`
  (Bubble Tea) forms for `new`, `add` (repo select, branch select, new-branch input), `sync`
  (worktree multi-select plus a dirty-handling choice of stash/reset/skip), `config`, `remove`,
  `switch`, `topic`, and `repo add`. The standing requirement is **full feature parity between the
  interactive and non-interactive routes**: anything reachable by flags must be reachable
  interactively. It is currently NOT at parity — every reporting command (`list`, `status`,
  `where`, `doctor`, `topic show`, `run`, `repo branches`, `project ls`, `prune`, `apply`) has no
  interactive route, and there is no full-screen program (`tea.NewProgram` appears nowhere). Closing
  that gap is the next work item after this guide.
- 23 stable error codes mapped to 8 distinct exit statuses. `busy` (6) is the only retryable one.
  `needs_input` (7) means a TTY was required and absent.
- Envelope schema 3 on stdout; on-disk manifest `.hydra/config.yaml` schema version 2; topic state
  `.hydra/state.yaml`, its own schema version, written under `flock` with atomic rename and a
  directory fsync.
- No SQLite and no database. State is two YAML files; git is the source of truth for everything
  git knows.
- Freshness is dated, never asserted: hydra does not fetch to answer a query, so every `behind`
  count carries an `upstream_as_of` timestamp read from the bare repo's `FETCH_HEAD`. Whether
  `status` should offer to fetch is an open product decision, deliberately unresolved.
- Not an Arvia product and carries no Arvia vocabulary: no hardcoded `prod`/`stage`, no forge, no
  vendor branch names.
- Version numbering is the author's call alone. Current: `v0.3.9`. It is not v1 until he says so.

## Brand Commitments

- Name `hydra`, lowercase in prose and as the binary. Repository
  `github.com/mssantosdev/hydra`. Author Marcus Santos.
- The many-heads-one-body reading of the name is inherent, not decorative: one workspace, many
  repositories, one unit of work across them.
- Voice: terse, evidence-first, no marketing register. The user's explicit instruction for this
  surface is that it is a technical presentation and guide, not marketing.
- Ships five borrowed terminal themes (tokyonight, catppuccin, dracula, nord, onedark) and has no
  first-party palette. The user has approved creating one and shipping it into the CLI as the new
  default, so the guide and the tool render in the same colours.
- **The interactive surface is never to be removed.** It is the human-interactable face of the tool
  and is held to feature parity with the flag-driven routes. Treating it as optional, or reading its
  absence from a `bubbletea` grep as evidence it does not exist, is a documented past error.

## Evidence on Hand

Real, and no claim in the guide needs inventing:

- The working binary at `~/projects/tools/hydra`, installed as `v0.3.9`.
- `hydra commands --output json` — the complete command and error-code surface, machine-generated.
- `scripts/e2e.sh` — 219 assertions passing; `make gate` green (gofmt, vet, golangci-lint 2.12.2,
  govulncheck, race, coverage 58.3%).
- `CHANGELOG.md` — 11 documented versions, every tag with a section, every compare link resolving.
- `~/projects/tests/_runs/` — 18 findings files from 16 independent zero-context agents across four
  adversarial rounds, which found 16 real bugs. This is the strongest evidence the product has and
  it is unusual: the tool was onboarding-tested by agents that had never been told how it works.
- No users, no downloads, no testimonials, no benchmarks, no adoption numbers. None may be
  fabricated or implied.

## Product Principles

1. **Narrow remit, kept.** New capability that would make hydra merge, rebase, resolve, or talk to
   a forge is refused, not deferred.
2. **The machine contract is the product.** Envelope, codes, and exit statuses are as load-bearing
   as the human output, and breaking them is a versioned event.
3. **Never report a state git can contradict.** Read git on every query; cache nothing; date what
   cannot be known fresh.
4. **Prove it, don't assert it.** Claims about behaviour are backed by a runnable command or they
   are not made.
5. **Both audiences, one artifact.** Anything a developer can learn from the tool, an agent can
   obtain as structured data.

## Accessibility & Inclusion

- The CLI must degrade to plain ASCII with no ANSI leakage under `NO_COLOR` or when piped; one
  lever gates every style.
- Non-TTY invocation is a first-class path, not a fallback: commands emit JSON rather than
  prompting, and return `needs_input` (7) when input was genuinely required.
- For the web artifact: the terminal transcripts must be real selectable text, not images, so they
  survive screen readers, zoom, and copy-paste. Motion must respect `prefers-reduced-motion`, and
  status must never be encoded in colour alone — the CLI's own `dot + word` rule carries over.

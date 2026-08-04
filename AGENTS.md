These rules apply to every task in this project unless explicitly overridden.
Bias: caution over speed on non-trivial work. Use judgment on trivial tasks.

## Project Context

This project is `hydra`: a Go CLI/TUI that manages a standardized workspace of
projects → groups → repos → git worktrees.

The four levels are the whole model:

- **project** — a workspace root holding `.hydra/config.yaml`, registered by name in a global registry.
- **group** — a directory under the project root grouping related repos (`backend`, `core`, …).
- **repo** — an alias registered under a group; the alias is the single source of both the bare
  path (`<bare_dir>/<alias>.git`) and the worktree directory base name.
- **worktree** — a real directory at `<project>/<group>/<alias>[-<slug>]`.

Primary design constraints:

- The bare repo holds git data **only**. No working tree ever lives inside `$GIT_DIR`.
- Worktrees are real sibling directories under `<group>/`. **Never symlinks**, never nested inside
  `<bare_dir>/`.
- Worktree identity is always resolved from `git worktree list --porcelain`. **Never** reconstruct a
  worktree path from a branch name, and never derive a branch name from a directory name.
- Bare repos are created with `git init --bare` + `remote add` + an explicit
  `remote.origin.fetch` refspec + `fetch`, never with `git clone --bare`. `refs/heads/*` starts
  empty; `refs/remotes/origin/*` is the authoritative remote view.
- Every worktree creation path either configures upstream tracking or explicitly records that the
  branch has none. Inventing an upstream is worse than reporting `local-only`.
- Every command must work non-interactively and emit the JSON output contract. Never open a `huh`
  form or Bubble Tea program when the effective output mode is JSON or stdin is not a TTY.
- JSON output is never localized and never colorized. Logs and hook output go to stderr so they
  cannot corrupt the envelope on stdout.
- **No command may report success for work it did not do.** A `TODO` followed by a success message
  is a defect, not a placeholder.
- Errors carry a stable `code` from the `internal/output` enum and a real exit code. Callers branch
  on `error.code`, never on message wording.

## Documentation

Documentation ships in the same change as the code and describes the current system:

- Command behavior, flags, and exit codes → the command's `Long` help **and** `README.md`. The
  `EXIT CODES` block of every command must match the `internal/output` error-code table exactly.
- Config schema → `docs/configuration.md` and `hydra.config.yaml.example`, together.
- The agent-facing contract → `skills/hydra/SKILL.md` only (see below). Never add a second
  agent-facing document; a hand-maintained duplicate rots, which this repo has already proven.
- Never leave a dead link. Either write the file or drop the link; there is no third option.

## Skills Guidance

Hydra ships its own agent skill at [`skills/hydra/SKILL.md`](skills/hydra/SKILL.md). It is
`go:embed`ed into the binary by `internal/skill`, so the shipped skill can never describe a
different version than the binary emitting it. `hydra skill` writes it to stdout and
`hydra skill --install` drops it into a consuming workspace at `.agents/skills/hydra/SKILL.md`.

`internal/skill/skill_test.go` mechanically guards it against drift: the documented command set must
equal `rootCmd`'s command set, and the documented error codes must equal the `internal/output`
code→exit map. Adding a command or an error code without updating the skill fails the build. Fix the
skill; never relax the test.

Third-party skills (Kiro SDD, selected
[mattpocock/skills](https://github.com/mattpocock/skills), caveman skills) are added manually by the
maintainer. **Do not vendor any third-party skill into this repo.** No `arvia-*` skill applies here —
hydra is not an Arvia project.

**Use `ast-grep` to search for code.**

## Go Guidance

- Keep `cobra` `RunE` handlers thin. Business logic belongs in `internal/…` packages.
- All git access goes through `internal/git`. Never `exec.Command("git", …)` from `internal/cmd`.
- Never discard an error from a git invocation. A swallowed git error is how this codebase shipped an
  inert `sync` and a silently missing upstream.
- Return `*output.Error` with a code from the enum for anything user-facing; `main.go` is the single
  exit-code authority.
- No `panic` outside `main`.
- Do not mutate shared logger or global state per call; `sync` runs concurrent goroutines.
- Run `make gate` before calling work done. It is the Arvia Go quality gate, in CI's
  order: `gofmt` (must print nothing), `go vet`, `golangci-lint run` (pinned to the
  version CI uses — a mismatch fails the target rather than drifting silently),
  `govulncheck`, `go test` with coverage, and `CGO_ENABLED=1 go test -race`.
  The race step is deliberately stricter than CI, which has it commented out; there
  is no local reason to inherit that gap, and `sync` is concurrent.
- Linter policy: `.golangci.yml` mirrors Arvia's linter set. gosec stays enabled on
  shipped code. Where a flagged construct is genuinely the feature — `exec.Command`
  in the git wrapper, `sh -c` in the hooks engine, a config file that must stay
  world-readable — suppress it inline with `//nolint:gosec // <rule>: <reason>` on
  its own line above the statement, never by widening the config. Test fixtures are
  excluded from gosec in one documented rule.
- Concurrency changes must come with a test that actually runs wide. A green
  `-race` on a single goroutine proves nothing.

## Rule 1 — Think Before Coding

State assumptions explicitly. If uncertain, ask rather than guess.
Present multiple interpretations when ambiguity exists.
Push back when a simpler approach exists.
Stop when confused. Name what's unclear.

## Rule 2 — Simplicity First

Minimum code that solves the problem. Nothing speculative.
No features beyond what was asked. No abstractions for single-use code.
Test: would a senior engineer say this is overcomplicated? If yes, simplify.

## Rule 3 — Surgical Changes

Touch only what you must. Clean up only your own mess.
Don't "improve" adjacent code, comments, or formatting.
Don't refactor what isn't broken. Match existing style.

## Rule 4 — Goal-Driven Execution

Define success criteria. Loop until verified.
Don't follow steps. Define success and iterate.
Strong success criteria let you loop independently.

## Rule 5 — Use the model only for judgment calls

Use me for: classification, drafting, summarization, extraction.
Do NOT use me for: routing, retries, deterministic transforms.
If code can answer, code answers.

## Rule 6 — Token budgets are not advisory

Per-task: 15,000 tokens. Per-session: 100,000 tokens.
If approaching a budget, summarize and start fresh.
Surface the breach. Do not silently overrun.

## Rule 7 — Surface conflicts, don't average them

If two patterns contradict, pick one (more recent / more tested).
Explain why. Flag the other for cleanup.
Don't blend conflicting patterns.

## Rule 8 — Read before you write

Before adding code, read exports, immediate callers, shared utilities.
"Looks orthogonal" is dangerous. If unsure why code is structured a way, ask.

## Rule 9 — Tests verify intent, not just behavior

Tests must encode WHY behavior matters, not just WHAT it does.
A test that can't fail when business logic changes is wrong.

## Rule 10 — Checkpoint after every significant step

Summarize what was done, what's verified, what's left.
Don't continue from a state you can't describe back.
If you lose track, stop and restate.

## Rule 11 — Match the codebase's conventions, even if you disagree

Conformance > taste inside the codebase.
If you genuinely think a convention is harmful, surface it. Don't fork silently.

## Rule 12 — Fail loud

"Completed" is wrong if anything was skipped silently.
"Tests pass" is wrong if any were skipped.
Default to surfacing uncertainty, not hiding it.

## Rule 13 - Understand the Product/Project

It's not enough to know what to do, you need to understand "WHY".
How does the change fit the overall architecture and project goal?
Keep the conceptual integrity of the design by keeping changes simple.

## Rule 14 - Go-like philosophy

Simplicity demands more work/thought than complexity, but results in a coherent and secure design.
Complexity is multiplicative: fixing a problem by making the system more complex slowly bleeds into other modules.
Good change: it can be accommodated without compromising the conceptual integrity of the design.
Bad change: trading simplicity for convenience.
It is easy to neglect simplicity, even though long-term simplicity is key for maintainable software.

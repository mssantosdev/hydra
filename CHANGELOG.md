# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases before `0.1.0` predate this file and are not reconstructed here; see the git history.

## [0.1.0] - 2026-08-05

The release that gives hydra a **unit of work**. Before this, the only noun was the worktree: you
asked for a directory for one branch in one repository, once per repository. A single piece of work
spanning three repositories was three unrelated worktrees, and nothing recorded that they belonged
together. A **topic** is now that missing noun — identity layered over worktrees, never a mode and
never a replacement for them.

Everything below is one of: that noun and the commands that use it, the machine-readable contract an
agent or script drives hydra through, or a bug found while building those.

### Breaking

Read this section before upgrading. Each entry says what to do.

- **The manifest moved from `.hydra.yaml` to `.hydra/config.yaml`, and there is no migration path.**
  hydra now owns one directory per workspace instead of a dotfile beside a dotdir. `FindConfig` looks
  for the new location and nothing else, so an existing workspace is not found at all until you move
  the file:

  ```bash
  mkdir -p .hydra && mv .hydra.yaml .hydra/config.yaml
  ```

  The manifest's own `version:` is **unchanged at `"2"`** — only its location moved, so the file
  contents need no edit. `.hydra/` also holds local state (`state.yaml`), its lock, and a `.gitignore`
  that keeps the local files out of git while the manifest stays committable.

- **The JSON envelope is `schema: 2`.** Four fields were added: `outcome`
  (`success` | `partial` | `failure`), `summary`, `next` (present only when there is a suggestion),
  and `error.retryable`. A consumer that pins `schema == 1` must update. There is still no `exit`
  field — the process status carries it.

- **`hydra clone` and `hydra adopt` are gone**, replaced by one front door:

  | before | now |
  |---|---|
  | `hydra clone <url>` | `hydra repo add <url>` |
  | `hydra adopt <path>` | `hydra repo add <path> --adopt` |
  | — | `hydra repo list`, `hydra repo remove <alias>` |

  `--adopt` is **required** rather than inferred. Guessing from the argument was tried and reverted:
  `git clone /local/path` is completely ordinary, so treating a local path as "adopt" silently turned
  *clone from this path* into *track this directory in place* — a different operation on a different
  directory. Passing `--branches` with `--adopt` is now an error instead of a quietly dropped flag.

- **A handle that matches several worktrees is refused instead of silently picking one.** Every
  repository has a `main`, and `hydra path main` / `switch main` / `remove main` used to take
  whichever repository sorted first and report success — `remove` would delete a worktree you never
  named. Ambiguity now fails with `worktree_name_conflict`, listing the candidates. Qualify the
  handle (`backend/api-main`) or pass a selector. If a script depended on the accidental pick, it was
  depending on a bug.

- **`worktree_name_conflict` now covers both collision kinds:** a derived directory name already
  taken, *and* a handle that names several worktrees. They are one failure — a name that does not
  identify exactly one worktree.

- **Missing values now return `needs_input` (exit 7), not `worktree_unknown` or `internal`.**
  `switch`, `remove`, `repo add` and `new` used to report the wrong thing when you simply did not name
  a target: nothing was unknown and nothing was broken. The error carries `details.missing`, or
  `details.one_of` where several flags would satisfy it, plus the valid values. Branch on
  `needs_input` instead of the old codes.

- **`hydra glossary` was removed.** Seven vocabulary entries did not justify a 316-line Bubble Tea
  application. `skills/hydra/SKILL.md` (via `hydra skill`) is what agents read, and `--help` serves
  humans.

- **`hydra clone --yes` was removed.** It was verified dead before deletion — declared and
  registered, never read. A flag that promises to skip prompts and silently does nothing is worse
  than an absent one. Use `--branches main` to state which branches you want.

- **Text output for `list` and `status` is laid out differently.** Both now render through one table,
  column widths come from content, and an over-long name truncates with an ellipsis rather than
  wrapping onto a second line. Text output was never a stable interface — use `--output json`.

### Added

- **`hydra topic`** — `list`, `show`, `attach`, `detach`, `remove`. A topic is a unit of work spanning
  repositories. There is deliberately **no `topic create`**: `attach` and `start` are the only
  commands that bring one into existence, so identity and work cannot drift apart. Membership is
  **recorded, never inferred from a branch name** — a branch stem is a fuzzy query, and using one as a
  destructive handle would let `remove --topic api` match every `api-*` worktree.
- **`hydra start <branch> --repos a,b --topic <id>`** — one branch across several repositories in one
  command. Which repositories and which branch resolve independently; either being unresolvable is
  `needs_input` naming that specific flag. `hydra start` with no selector never means "every
  repository".
- **`hydra run --topic <id> -- <argv>`** — one command per worktree. Everything after `--` is an argv
  array handed to exec: hydra **never** wraps it in `sh -c`, so a path with a space or a branch with a
  metacharacter cannot become a second command. A shell stays available explicitly via
  `-- sh -c '…'`. Each invocation gets `HYDRA_TOPIC`, `HYDRA_REPO`, `HYDRA_GROUP`, `HYDRA_BRANCH`,
  `HYDRA_PATH` and an explicit working directory. `--jobs` bounds concurrency; `--timeout` kills a
  hung command.
- **`hydra apply -`** — reads JSON on stdin in exactly the shape `hydra list --output json` emits (or
  a bare `jq`-filtered array) and creates the worktrees it describes, so a workspace can be captured
  and reproduced elsewhere. Convergent, and `--dry-run` reports the plan. It creates worktrees but
  never repositories, so the target workspace must already have them registered. Observed state in
  the document (`ahead`, `dirty`, `path`) is ignored rather than half-honoured.
- **`hydra commands`** — publishes the whole command surface plus the complete error-code→exit table
  as JSON, without needing a workspace. `SURFACE.txt` is a committed snapshot of the text form, so
  surface drift appears as a reviewable diff; a contract test fails when the two disagree.
- **`--against REF` on `list` and `status`** — answers "is this work in REF yet" as
  `{ref, merged, ahead, behind}`, computed from git at query time so it cannot go stale. Kept separate
  from `ahead`/`behind` against the upstream, because those are different questions. An unresolvable
  ref is a per-worktree warning, never fatal — a release branch legitimately exists in some
  repositories and not others.
- **One selector surface**, shared by `list`, `status`, `path`, `sync` and `run`: `--topic`,
  `--repos`, `--group`, `--all`, and `--filter dirty|behind|branch:<glob>`. Selectors intersect.
- **A branch-name precedence chain** (`internal/branchresolve`), in this order: positional `<branch>`,
  `--branch`, the unanimous branch of `--topic`'s existing members, `branch_provider`,
  `branch_pattern`. There is no step 6 — with none of these available you get `needs_input` naming
  `--branch`, because the alternative was creating a branch literally named after a ticket id.
  Patterns are one literal string over a closed placeholder set; `{kind}` and `{slug}` are never
  inferred.
- **`sync --dirty stash|reset|skip`** — the dirty-policy prompt as a flag.
- **`hydra config show` and `hydra config set theme|editor <value>`** — configuration was previously
  readable but only writable through an interactive form.
- **New error codes:** `topic_unknown`, `topic_conflict`, `state_version_unsupported`,
  `branch_provider_failed`, `busy` (exit 6), `needs_input` (exit 7). **`busy` is the only retryable
  code**, so a caller can distinguish "another hydra is mid-write, retry me" from a real failure.
- **`doctor` gained a `topic_dangling_member` check** with `--fix`, so a member whose worktree is gone
  is named and repairable.

### Changed

- `sync` and `repo add` now run on one shared execution engine (`internal/fanout`) instead of two
  hand-rolled executors. This makes result order deterministic (so envelopes can be diffed), runs
  each `post_sync`/`post_clone` hook immediately after its own item rather than in a second pass,
  stops a single failing hook from dropping successful work out of the envelope, and bounds
  concurrency instead of spawning one goroutine per worktree.
- Worktree **creation** serialises per bare repository; **pulls** stay parallel. This was measured,
  not assumed: eight concurrent `git worktree add` with upstream config left three successes and —
  the dangerous part — worktrees with *no upstream at all*, while concurrent `pull --ff-only` after a
  pre-fetch was 4/4.
- `summary` is a required argument to the emit path rather than an optional field, so all 26 call
  sites had to state their answer. `error.retryable` is derived from the code rather than passed in,
  so no call site can produce an error whose retryability disagrees with its code.
- `outcome: partial` travels on the **success** envelope on stdout while the error envelope goes to
  stderr and the process exits 4 — the work that landed is real data and a caller must see both.
- `remove` detaches from a topic **after** git removes the worktree. Detach-first, if interrupted,
  leaves a live worktree that looks unassigned and that nothing reports; detach-after leaves a member
  with no worktree, which `doctor` names and fixes. A findable inconsistency beats an invisible one.
- Sync progress is reported per item as it completes, to stderr so stdout stays pipeable, and is not
  gated on a TTY — a CI log is exactly where "which repo is slow" is worth knowing.
- `skills/hydra/SKILL.md` was rewritten around invariants, the envelope, decisions, the command
  table, error codes and anti-patterns.
- `huh` upgraded v0.6.0 → v1.0.0 (and `catppuccin/go` v0.2.0 → v0.3.0) with no source change.
  Deleting `glossary` also removed the last *direct* `bubbletea`/`bubbles` usage.

### Fixed

- **Re-running an identical `clone` on a complete repository failed** with
  `git_failed: no worktree could be created for "api"`, because every branch that already had its
  worktree counted as a failure. It now converges: exit 0, with both worktrees reported. An agent
  re-running a clone to confirm it landed got a hard error naming no real problem.
- **`sync`'s dirty handling was silent, not merely missing.** The dirty-policy prompt had no
  interactive guard, so in a non-TTY the form failed, the error branch deselected the worktree, and
  the envelope reported nothing pulled with no explanation.
- **`config set theme <garbage>` persisted an unusable value** that only surfaced later as a silent
  fallback at render time. The value is now validated where it is set.
- **`git stash push` was missing `--include-untracked`** while hydra's own definition of *dirty*
  counts untracked files, so hydra could call a worktree dirty, stash nothing, pull, and then fail
  popping a stash that never existed. A `HasStash` guard means "nothing was saved" can no longer be
  reported as a sync failure.
- Result ordering had a second source: entries were reassembled in `git worktree list` order after
  the engine had sorted them. The sort has to be on what is serialised.

### Security

- **`golang.org/x/text` v0.23.0 → v0.39.0**, clearing GO-2026-5970 (an infinite loop in
  `norm.Form.Transform` on invalid input). Not theoretical: `govulncheck` traced it to newly written
  code that normalises `--slug` and `--kind`, which come straight from a user, so invalid UTF-8 would
  have hung hydra with nothing done. `TestSlugSegment_TerminatesOnInvalidUTF8` feeds lone
  continuation bytes and a truncated multi-byte sequence through it behind a deadline, so the
  property is proven rather than assumed from a version bump.

[0.1.0]: https://github.com/mssantosdev/hydra/compare/v0.0.19...v0.1.0

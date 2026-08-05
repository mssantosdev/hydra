# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases before `0.2.0` predate this file and are not reconstructed here; see the git history.
There is no `0.1.0`: that version string was published once in an earlier life of this repository and
is permanently bound to different content in the Go checksum database, so it can never be installed.

## [0.3.4] - 2026-08-05

Both found by handing the installed binary to four agents with no prior knowledge of hydra.

### Fixed

- **A usage mistake now carries its own recovery.** `hydra path a b` reported nothing but
  `accepts at most 1 arg(s), received 2`, which reads as hydra breaking rather than as the
  caller mis-typing. Argument-count, unknown-flag and invalid-argument errors now attach the
  offending command's own usage line as `details.usage` and a `next[]` pointing at its help:

  ```json
  {"error":{"code":"internal","details":{"usage":"hydra path [<worktree-name>] [flags]"}},
   "next":[{"argv":["hydra","path","--help"],"why":"show this command's arguments and flags"}]}
  ```

  The code stays `internal`, which the contract already documents as the unclassified
  catch-all including a bad flag value. What changed is that it is now actionable.

- **A zero-match query no longer blanks the project scope.** `list --filter branch:nope-*`
  returned `"project": ""` and `"root": ""`, which a caller cannot tell apart from "not in a
  project" and cannot use to locate the workspace it just queried. The text listing still
  omits an empty project, so a narrowed view never prints a heading with nothing under it.

### Verified, not changed

Reported by the same round and checked to be correct as-is:

- `--output text` does force text when stdout is not a terminal.
- The unsuffixed worktree directory follows the remote's default branch, not the position of
  a branch in `--branches`. Two agents independently believed it was positional, so the rule
  is under-documented even though the behaviour is right.

[0.3.4]: https://github.com/mssantosdev/hydra/compare/v0.3.3...v0.3.4

## [0.3.3] - 2026-08-05

### Breaking

- **An unregistered bare repository now FAILS `doctor` instead of warning.** `.bare/` is
  hydra's own directory — nothing else puts a bare repository there — so one that is absent
  from the manifest is real state hydra cannot see, not a note. `list`, `status`, `run` and
  `sync` all silently omit that repository. A warning was too quiet for that, and a script
  gating on `doctor` exiting 0 would have shipped straight past it.

  A worktree-shaped directory that is not registered stays a **warning**: that one can
  legitimately be something of yours that hydra was never asked to manage.

  The failure now reads the bare repository's `origin` and names the exact recovery, so the
  fix is copyable rather than something to work out:

  ```
  ci-ops.git exists on disk but is not in the manifest, so hydra cannot see it;
  run "hydra repo add vs-ssh.../arvia-ci-ops --as ci-ops --group <group>" to register it
  ```

  That command is convergent — it completes the registration in place rather than
  re-cloning.

### Fixed

- **`doctor`'s outcome now agrees with its exit status.** With a failing check it emitted
  `outcome: success` and no error object, then exited 4. That is precisely the contradiction
  schema 3 removed from `run`: the envelope said clean while the process said otherwise. It
  now reports `outcome: partial` with `partial_failure`, and the checks stay in `data` where
  a caller needs them.

[0.3.3]: https://github.com/mssantosdev/hydra/compare/v0.3.2...v0.3.3

## [0.3.2] - 2026-08-05

### Fixed

- **Concurrent `repo add` silently lost registrations.** Running several `repo add`
  invocations at once against remote repositories registered only some of them. Every
  invocation reported `outcome: success`, every bare repository was cloned, and the
  manifest ended up with half the entries:

  ```
  four concurrent repo add  ->  all four report success
  .bare/                    ->  4 repositories cloned
  .hydra/config.yaml        ->  2 registered
  ```

  `Config.Save` marshals the caller's in-memory manifest over the whole file, so two
  processes that both loaded it before either finished cloning each wrote their own stale
  view — and the second erased the first. A clone takes seconds, which is what makes the
  window wide enough to lose reliably; the same test with local fixtures passes because the
  window is microseconds.

  `config.Update` now takes a lock, re-reads the manifest, applies the mutation and writes
  — the same shape `.hydra/state.yaml` has used since 0.2.0, which the manifest never got.
  Contention reports `busy`, the one retryable code. The lock is a separate `config.lock`
  because writing replaces the manifest and swaps its inode.

  `doctor` did detect the aftermath, as `bare_unregistered` and `worktree_unregistered`
  warnings; it is not a silent loss once you look. Recovery was, and still is, re-running
  the same `repo add`, which is convergent.

- **`repo restore` guessed `main` for a repository whose manifest entry had no default
  branch**, then failed with `branch "main" does not exist on origin` on repositories whose
  branches are `prod` and `stage` — presenting a guess back as the user's own
  configuration. An absent default branch now means "resolve the remote's HEAD", which is
  what `repo add` already does when given no `--branches`.

- **`repo restore`'s summary understated what it had done.** It reported only the repository
  count, which read as completion; a workspace restored from a manifest deliberately has
  one worktree per repository rather than the set the source had open. The summary now says
  `default-branch worktrees only`, alongside the `next[]` that already points at `apply -`.

### Verified, not changed

Reported by the same testing and checked to be correct as-is:

- stdout carries pure JSON during a clone — git's progress is on stderr, and the reports to
  the contrary came from merging the two streams.
- The unsuffixed worktree directory follows the remote's default branch, not the order of
  `--branches`, so `--branches stage,prod` does not silently swap which one gets the bare
  name.

[0.3.2]: https://github.com/mssantosdev/hydra/compare/v0.3.1...v0.3.2

## [0.3.1] - 2026-08-05

### Fixed

- **Help text no longer recommends commands that were deleted.** `clone` and `adopt` became
  `repo add` in 0.2.0, but eleven places still named them — including the `Next:` block
  `hydra init` prints on success, and `doctor`'s repair advice for an interrupted clone.

  Found by handing the installed binary to four agents with no prior knowledge of hydra and
  asking them to build a workspace. All four read `hydra init`'s own output, ran
  `hydra clone`, and got `unknown_command` — the tool teaching something false at the first
  opportunity, which is precisely what 0.3.0's affordances were meant to prevent. Every site
  now names `hydra repo add`, with `--adopt` for a checkout and `--as` rather than the
  long-removed `--alias`.

  `TestHelpNeverNamesAMissingCommand` walks every registered command's short, long and
  example text, extracts invocation-shaped mentions, and fails on any that is not a
  registered command or alias. Prose that merely contains the word "hydra" is not matched.

[0.3.1]: https://github.com/mssantosdev/hydra/compare/v0.3.0...v0.3.1

## [0.3.0] - 2026-08-05

The release that makes hydra correct the caller instead of letting a wrong assumption stand.

Everything here was found by building a real 13-repository, 24-worktree workspace against live
remotes and recording every point where the tool allowed a mistaken belief to survive. Two of the
results are bugs that predate this work; three of the findings turned out to be wrong and are
recorded as such below, because they were wrong in a way worth remembering.

### Breaking

The **envelope** version is now `schema: 3`. The manifest's own `version: "2"` is unchanged and needs
no edit — they are separate contracts.

- **The failure envelope moved from stderr to stdout.** One envelope, one stream, success or failure.
  Errors were on stderr and so was git's fetch progress, so a caller wanting both the data and the
  reason reached for `cmd 2>&1 | jq` — which corrupted the JSON on the *success* path. The exit status
  still carries the code and `hydra commands` still publishes the code→exit table, so stderr never
  held anything machine-readable worth keeping there. Read stdout; you no longer need to merge
  streams. Our own end-to-end suite lost 38 uses of the `2>&1 >/dev/null` dance.

- **`next[]` is `{argv, why}`, not `{action, cmd}`.** A caller execs the array instead of parsing a
  shell line, so a branch name containing a space stops being the caller's quoting problem. `why`
  is required: a suggested invocation with no stated reason is a guess the caller has to justify on
  hydra's behalf.

- **`outcome` no longer reports `partial` when nothing succeeded.** If every item failed the outcome
  is `failure`. Code treating `partial` as "some of it worked" was, in that case, being lied to.

### Fixed

- **`run` reported `outcome: partial` when every worktree failed** — stdout claimed work had landed
  when none had, while stderr said `git_failed` and the process exited 1. The outcome is now derived
  from whether anything actually landed.

- **A healthy listing could report `partial_failure`.** `list` computed success as
  `len(repos) - len(warnings)`, so any advisory warning — an unresolvable `--against` ref, an empty
  selector — was counted as a failed repository. The check tripped whenever those two numbers
  coincided and masked real failures whenever they did not. Repository failures are now counted
  explicitly rather than inferred.

### Added

- **`hydra run` captures per-worktree `stdout` and `stderr`.** It was an exit-code poller: output from
  several worktrees arrived unattributed, and under `--jobs` it interleaved mid-line, so no caller
  could tell which worktree said what. Each result now carries the output with `*_bytes` and
  `*_truncated`. stdout keeps its **head** and stderr its **tail**, because the reason a command
  failed is the last thing it writes before a non-zero exit. Under a TTY the streams still pass
  through live. There is no `--capture` flag: it would be a mode, and hydra has none.

- **`hydra where`** answers "where am I" — workspace root, manifest, and, inside a worktree, its group,
  repo, branch and topic. Outside a workspace it **succeeds** with `in_project: false`, because that
  is the answer a caller dropped into an unknown directory needs; failing would make "no workspace"
  indistinguishable from "broken workspace".

- **`hydra repo branches <url|alias>`** lists a remote's branches without cloning, so `--branches` can
  be chosen rather than guessed. Auditing 14 repositories by hand previously meant 67 seconds of
  `git ls-remote`.

- **`hydra repo restore`** rebuilds every repository the committed manifest declares, **additive only**
  — it never removes, never rewrites a remote, and reports disagreement as `warnings[]`. `--jobs N`
  clones concurrently, which is the actual fix for thirteen repositories taking eight minutes one
  `repo add` at a time. The manifest records repositories, not the worktrees you had open, so its
  `next[]` points at `apply -` for that half.

- **`unknown_command`** (exit 1) replaces `internal` for a mistyped subcommand, carrying
  `details.did_you_mean` and `details.available` as data instead of cobra's English prose, plus a
  `next[]` pointing at `hydra commands --output json`. That is the error a caller with no prior
  knowledge of hydra is most likely to hit first, and the recovery was previously undiscoverable
  from it.

- **`apply` reports `group`, `name` and `path`.** It reported having created a worktree without saying
  where — in the one command whose entire purpose is reproducing a workspace elsewhere. `start` gains
  `name`, the handle every other command accepts. A contract test now holds those fields across all
  three worktree-reporting shapes and was verified to fail when one drifts.

- **A selector that matches nothing says how many candidates it considered**, so a typo'd glob is
  distinguishable from a true empty. It stays exit 0: "nothing is dirty" is a correct answer, not an
  error.

### Changed

- `COLUMNS` is honoured whenever set and parseable, TTY or not. `term.GetSize` fails on a pipe and
  fell back to 80, so a piped caller had no way to ask for anything narrower.
- The interactive branch picker bounds its list to 15 rows. `huh` sets no default height, so a
  repository with 140 branches rendered all of them before you could reach the filter — which `huh`
  already enables under `/`, and which matches case-insensitive substrings.
- `hydra init` reports the registry path it wrote to, so a throwaway workspace no longer accumulates
  an entry in the real global config with no hint that `HYDRA_CONFIG_DIR` exists.

### Retracted findings

Recorded because they were wrong, and because two external reviews ranked this bundle on the first
of them:

- *"`start` returns counts, not worktrees"* — **false.** It already returned full objects with `path`
  in `created[]`/`skipped[]`/`failed[]`. The claim came from reading `.data|keys`, seeing `created`,
  and assuming an integer.
- *"a bad `--branches` value offers no guidance"* — **false.** It already failed with `branch_unknown`
  carrying `details.available`.
- *"unknown `--topic`, `--repos` or `--group` silently return an empty list"* — **false.** All three
  already failed with the known values attached. Only `--filter` returned an empty success, which is
  legitimate.

### Verified, not changed

- `prune` already drops a registry entry whose root exists but has no `.hydra/config.yaml`.
- A child's exit code never becomes hydra's: `run` exits from its own code table and reports the
  child's in `results[].exit_code`.
- Four concurrent writers and eight concurrent readers on one workspace all succeed; the state lock
  does not thrash. Stale-lock recovery after a crash remains untested.

[0.3.0]: https://github.com/mssantosdev/hydra/compare/v0.2.0...v0.3.0

## [0.2.0] - 2026-08-05

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

[0.2.0]: https://github.com/mssantosdev/hydra/compare/v0.0.19...v0.2.0

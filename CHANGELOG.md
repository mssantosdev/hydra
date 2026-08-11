# Changelog

All notable changes to this project are documented here.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

Releases before `0.2.0` predate this file and are not reconstructed here; see the git history.
There is no `0.1.0`: that version string was published once in an earlier life of this repository and
is permanently bound to different content in the Go checksum database, so it can never be installed.


## [Unreleased]

### Fixed

- **`defaults` and `hooks` did nothing below the workspace level.** Every document since schema 3
  said the three keys exist at workspace, group and repo, and `hydra config show` reported a
  winning level of `group <name>` — while `add` branched from the workspace's `base_branch` and
  only the workspace hook chain ever ran. At the repo level neither was a struct field, so a
  manifest carrying `repos.api.hooks.post_add` parsed clean and the hook was dropped in silence;
  the guide, the manifest reference and the copy-me template all demonstrated exactly that.
  `defaults` now resolves nearest-wins through the chain and hook chains append workspace → group
  → repo, so the provenance `config show` prints is the chain a command executes. A repo may spell
  its policy flat (`branch_pattern:`) or in a `defaults:` block, the block winning.
- **`add --as` reported success for a worktree it did not create.** A branch already checked out
  under a different directory satisfied the convergence check, so `add api stage --as devcopy`
  exited 0 with `disposition: skipped` and `name` set to the other worktree, and the next command
  — `hydra path devcopy` — exited 1 with `worktree_unknown`. Convergence now requires the branch
  to be at the requested path. `add`, `start`, `clone` and `apply` shared the check and all four
  are fixed; `apply --dry-run` gained the third disposition so a predicted conflict is no longer
  counted as a creation.
- **`Save` could write a manifest hydra cannot read.** An anchor on a modelled key was lost when
  that key was re-encoded from the struct, while an unmodelled key holding the matching alias was
  carried verbatim. `repo remove` exited 0 having written a dangling `*name`, after which every
  command — `doctor` included — could only answer `not_in_project`. Anchors now travel with their
  value, and `Save` verifies the merged bytes still parse and still describe the same manifest,
  falling back to a plain marshal rather than leaving an unloadable workspace.
- **One write deleted a manifest's own header.** A comment block separated from the first key by a
  blank line attaches to the document node, which the merge never read, so the example lost the
  five lines stating the preservation guarantee. Document-level comments are kept, as are comments
  on a modelled key that `omitempty` omits, and comments inside a list when the list is
  structurally unchanged — a list a command rewrote still drops them rather than moving a comment
  onto an entry it does not describe.
- **`hydra apply` could not reproduce a workspace that used `--as`.** The captured `name` was
  discarded and the directory re-derived from the branch, which is the one thing the command
  exists to do. `applyItem` now carries `name`, validated as a path segment because the document
  is untrusted input, and a rejected name fails that item rather than the run.
- **`hydra hooks --help` advertised eight environment variables while ten were injected.** The
  ENVIRONMENT block is generated from the same list `hooks ls --output json` publishes.
- **`hooks ls` under-reported every group and repo chain.** It counted `cfg.Hooks` only, so it
  answered `1` for an event that ran three hooks — the same drift as the level model above, in the
  command whose help text this release also fixed. It now counts all three levels and publishes the
  breakdown, so the number cannot disagree with what runs.
- **`apply` returned a different exit code for the same end state.** A topic the document could not
  record was a warning when this run created the worktree and an error when it found one already
  there, so a convergent document exited 0 then 1. The disposition now describes the worktree and
  the error describes whether the request was met, on both paths.
- **The docs gate reported success for checks that never ran.** Five of six sat behind a
  capability guard and the success line printed regardless, so hiding PyYAML or deleting `./hydra`
  produced exit 0 and the claim that all six agreed with the build. A missing prerequisite is now
  a failure that names it, and `gate-docs` depends on `build`.

### Changed

- Comments across the codebase state the rule the code obeys rather than recounting the defect
  that prompted it. Git history holds the incident.

## [0.5.1] - 2026-08-10

### Fixed

- **A caller with only the binary could not find the guide.** The URL lived in `README.md` and
  nowhere reachable: an agent installs hydra with `go install` plus `hydra skill --install`, so it
  has the binary and the skill and no repository checkout, and all four of the places the URL sat
  were files it cannot open.

  `hydra --help` now prints it, and the agent skill carries it on the invariant that already names
  `hydra commands`. `check-docs-claims.sh` asserts what `--help` prints against README's link, the
  guide's own canonical URL, and the skill, so the four copies cannot drift apart.

  It was first added to `hydra commands --output json` as a `docs` field, which was wrong: that
  command describes COMMANDS, and a URL is not one. Fitting it there meant a new field, a schema
  bump, a gate check and an e2e assertion — four pieces of machinery to avoid one line in a file
  with a line free. The difficulty was the signal that the change did not belong, and it has been
  reverted: `surface_schema` stays at 1 and the surface describes commands only.

- **`hydra.config.yaml.example` was a seventh schema-2 manifest** — and the least excusable, being
  the template `configuration.md` tells you to copy into your project root. It now shows schema 3
  and doubles as the worked example of the three-level model: a group with its own `path` and
  `base_branch`, per-repo `branches:` and toolchain hooks, a workspace-level `carry` entry, and a
  once-per-topic hook.

  The manifest check did not look at it, and when it was added to the list it still skipped it: the
  filter required a block to *start* with `version:`, and the template opens with a comment. So the
  one manifest a user runs verbatim was the one the guard ignored. The filter now decides on the
  parsed document. Verified by un-nesting one group in the real file: it names the group and reports
  `declares 4 repo(s), hydra sees 5` — the silent loss the check exists to catch.

- **The published guide was linked from `README.md` only.** `docs/README.md` — the page someone
  lands on when they open `docs/` — and `configuration.md`'s See Also now link it too.
  `docs/README.md` also carried `version: "0.1.0"` in its frontmatter, read as hydra's version, and
  0.1.0 is the one version string this repository can never publish. Nothing consumes the
  frontmatter, so the line is gone rather than given a value to maintain.

  `skills/hydra/SKILL.md` deliberately does NOT link it. The skill is the machine contract; every
  fact in the guide is already there in denser form, and at 119 of a 120-line cap that line is worth
  more as headroom for a future error code than as a pointer an agent cannot use.

- **Six documented manifests still showed the schema-2 shape**, in the release whose headline is
  schema 3 — including the guide plate, which declared `version: "3"` above a comment reading
  `# must be exactly "2"` above a schema-2 body. A group left in the old nesting parses as a group
  with ZERO repositories, so it drops one silently and looks fine to a reader who does not run it.
  All six now show schema 3.

- **The guard for that drift was inert on its first attempt.** `scripts/check_doc_manifests.py`
  loads every documented manifest and requires `hydra repo list` to see exactly as many
  repositories as the example declares. It was appended below check-docs-claims.sh's exit gate, so
  a broken example printed the drift report, printed "all agree with the build", and exited 0. Now
  above the gate, verified by exit code rather than by grepping for the message.

- **`carry` was used in the guide's §10 and defined nowhere.** Now defined where it is used, in
  both languages.

0.5.0 is unchanged and still installs; these are documentation fixes only.

## [0.5.0] - 2026-08-10

The manifest becomes enough on its own.

Before this release a manifest recorded which repositories a workspace had, and nothing about the
shape of it — so reproducing a setup meant sending someone a captured `hydra list --output json`,
which carried that machine's work-in-progress and topic membership along with it. `branches:` fixes
that: a manifest now declares what each repo keeps checked out, `repo restore` creates exactly that,
and work in progress cannot leak in because only `repo add --branches` and `repo set` ever write it.

Schema 3 makes a group an object, so the level that means "these repos belong together" finally has
somewhere to put a path, defaults, hooks and carried files. `defaults`, `hooks` and `carry` now exist
at all three levels, resolving workspace to group to repo — scalars nearest-wins, lists appending.
What belongs at which level is your modelling choice, not hydra's.

**Breaking**: `groups.<group>` is now an object with a `repos:` key. A schema-2 manifest still loads
and is upgraded on the next write, comments and unmodelled keys intact.


### Added

- **Topic hierarchy: `parent:`, and `hydra topic close`.** Containment is opt-in and recorded —
  a topic without a parent is flat, which stays the default. `topic close` is the gate: a leaf closes
  immediately, and a topic with children closes only when every child is closed and every child's
  branch has reached this topic's branch.

  Closeability is **derived from git on every call**, never stored: a stored answer is wrong the
  moment someone rebases. The rule is member-granular, because "is the child merged into the parent"
  is ambiguous once the two span different repositories — for each child member `(repo, branch)` the
  parent must have a member in that same repo to merge into. A child reaching into a repository its
  parent does not cover reports `no_integration_target` rather than passing vacuously, which would
  claim done over stranded work.

  `topic_not_closeable` carries every reason at once in `details.blocked_by`, each naming the child
  and, where relevant, the repo and branch. `--reopen` reverses a close. Cycles are refused: a topic
  cannot be its own ancestor.

  hydra still does not merge. It reports whether you *can* close; you merge.

- **Four once-per-operation hook events**: `post_topic_start`, `pre_topic_close`,
  `post_topic_close`, `pre_topic_remove`. `pre_topic_close` is the only place a check can veto
  *finishing* a unit of work — `post_add` fires before any work exists, `pre_remove` when it is
  being thrown away — so a quality gate finally has an honest home. They fire once per operation,
  not once per worktree: wiring a notification to `post_add` posts one message per created worktree
  for a single piece of work, and a `run_once:` flag would paper over that rather than fix it.
  `pre_topic_remove` fires before the first member is touched, since teardown detaches per member
  and a hook firing mid-loop would see a half-dismantled set.

- **Schema 3: a group is an object.** It was the only noun in the model with nowhere to put
  anything — a bare map key, no properties, no command — while the override chain ran
  project → repo and skipped the level that means "these repositories belong together". So a family
  convention had to be repeated on every repo, and `base_branch` could not vary below the project at
  all.

  A group now carries `path`, `defaults`, `hooks`, `carry` and `repos`. Resolution is
  **workspace → group → repo**: scalars nearest-wins, lists (`hooks`, `carry`) append. An empty value
  means *inherit*, not *clear*.

  `path:` places a group's worktrees, which is the design that replaced putting a slash in the group
  NAME — a slash made selectors, completion and rename ambiguous, where a path field does not.
  `platform/infra` goes in the field; names stay one segment.

  hydra still does not decide what a group means. `go-projects`/`java-projects` and `backend`/`web`
  are equally valid partitions: every level carries the same keys, the chain is the only rule.

  **Version 2 manifests still load.** A v2 group maps straight to its repositories; it is renested in
  memory and written back as version 3 on the next mutation, comments and unmodelled keys included.
  No migration command, and the upgrade lands as a diff in a file that was already committed. Anything
  older or newer is still refused, so a manifest from a hydra that knows more is never half-read.

- **The manifest can declare a repository's shape: `branches:`.** It recorded repositories and their
  default branch only, so a workspace rebuilt from a manifest had one worktree per repo and
  `repo restore` ended by pointing the caller at a captured `hydra list --output json` for the rest —
  a snapshot that also carries the source machine's topic membership. `repo add --branches
  main,stage,prod` created three worktrees and remembered none of it.

  A manifest that declares `branches: [master, stage, prod]` now reproduces the whole shape on its
  own: no snapshot, no topics, no secrets. `repo restore` creates the declared set and `repo list`
  reports it.

  `repo add --branches` persists the resolved set. That is a behaviour change to a shipped flag, and
  the distinction it rests on is the point: naming a branch set is a **declaration**, while
  `hydra add api feat/x` is **work**. Only the first writes the manifest, which is what keeps
  work-in-progress out of a committed file — and makes "what is baseline" answerable without a
  heuristic. Baseline is what the manifest declares; WIP is everything git has beyond it. An earlier
  attempt inferred it from topic membership, which is wrong: an ad-hoc `feat/x` worktree has no
  topic either.

  Additive field, no schema bump. A manifest without `branches:` behaves exactly as before, including
  the `apply -` hint — there the capture genuinely is the only way to get the rest. Two messages that
  became false are fixed with it: the summary claimed "default-branch worktrees only" over a complete
  restore, and `next[]` sent you looking for a capture you no longer need.

- **`hydra repo set <alias> --branches a,b,c`** changes a repository's declared shape after
  registration. Without it the only way to edit a declaration was by hand in
  `.hydra/config.yaml` — a file hydra also writes, so the user and the tool were competing over one
  document.

  The branch list is validated against origin, so a name that does not exist is refused rather than
  written and discovered by whoever restores the workspace next. On a terminal with no `--branches`
  the same multi-select `repo add` uses opens, and it now pre-selects the CURRENT declaration:
  previously it ticked only the default branch, so pressing enter on a repo declaring
  `[master, stage, prod]` would have silently narrowed it to `[master]`.

  Declaring fewer branches never deletes a worktree. The response reports `before`, `branches` and
  `undeclared` — the worktrees now outside the declaration — with a warning naming them, because a
  narrowing should be visible when it happens rather than discovered later as drift. Removing a
  worktree stays `hydra remove`, which asks about unmerged work.

  Without `--branches` and without a terminal it returns `needs_input` naming the flag and listing
  origin's branches in `details.one_of`, rather than falling through to the clone path's "nothing
  selected means the default branch" — which would have narrowed an existing declaration to one entry.

- **`carry:` places the files a new worktree needs and git ignores.** A fresh worktree has every
  tracked file and no `.env`, so it cannot run — and the workaround, copying it by hand per worktree
  per repo, is learned in week one and paid forever, which is why it never appears in an issue
  tracker. Declarable at the workspace and per repo, appending workspace-first so a shared dev
  certificate and a repo's own `.env` both apply.

  Two forms: a bare `- .env` copies from the source worktree (the worktree `--from` named when it has
  one, else the repo's default-branch worktree — deterministic, never searched for), and `from:`/`to:`
  reads a fixed workspace path. `mode: link` symlinks instead of copying.

  It is not a hook. Placing files is materialisation and hydra already owns layout: it knows the
  source worktree because it just resolved one, where a hook would have to rebuild
  `<root>/<group>/<repo>` — a path `--as` can override — and a missing file would surface as
  `hook_failed` rather than the warning it is. It runs before `post_add`, so a hook that installs
  dependencies can rely on the configuration being present.

  Carrying never overwrites: an existing file is reported `skipped`, so re-running is a genuine no-op
  and an edited `.env` is never clobbered. A missing source is a warning naming the file, never a
  failure — `apply` and `repo restore` replay **structure, not secrets**, and only `from:` entries
  survive a fresh machine.

  Every write is confined to the worktree by the kernel through `os.Root`, and an absolute or
  `..`-containing path is refused when the manifest is parsed. A manifest is meant to be handed
  between people, so it must not be able to write outside the workspace it describes.

- **Hooks take a `timeout:`, and have one by default.** Hooks were the one unbounded wait in hydra —
  the state lock is 5s, the manifest lock 10s, `run` takes `--timeout` — so a hook that hung hung the
  tool with no way to say "give up". That is worst exactly where hooks earn their place, on an
  instance bootstrap whose own deadline is minutes: a network hiccup in someone's `npm ci` hangs the
  boot. Default 10m, generous because a cold-cache dependency install genuinely takes minutes;
  `timeout: 0` keeps the old unbounded behaviour as a deliberate choice. A hook that outlives its
  bound is reported as `timed out after <d>` rather than as whatever exit status killing it produced.

- **`HYDRA_TOPIC` and `HYDRA_SOURCE_WORKTREE` are exported to hooks.** A `post_add` fired by
  `start --topic X` could not name X, so "do something for this unit of work" was impossible from a
  hook — a hole in the extension surface that is the product. And finding the originating worktree
  meant rebuilding `<root>/<group>/<repo>`, which SKILL.md lists as an anti-pattern in the same
  breath, because `--as` can override the derived name. Both are always exported, even empty, so a
  hook under `set -u` can test them without aborting on an unset variable.

  `hooks ls --output json` now publishes the whole list as `env[]`, so a hook author can discover
  what a hook receives instead of reading it off a page — and `make gate` asserts the guide and the
  field reference against that list. The published page advertised eight variables for one commit
  after two more existed, which is exactly the drift a self-describing surface prevents.

- **`hydra config show` reports the manifest, with the level each value came from.** It promised the
  configuration and returned three global settings; `.hydra/config.yaml` — the file everyone calls
  the config, holding the repos, defaults and hooks — was absent from every command, and two files
  are both named `config.yaml`. Each row now names its source:

  ```
  base_branch        master           project
  a.base_branch      develop          group g
  a.branch_pattern   feat/{slug}      repo a
  ```

  The `from` column is the point. With workspace, group and repo all able to set `base_branch`,
  "why is my base develop" had no answer you could read off the file — you had to run the
  resolution across three places yourself. Rows appear for the project level and for repos that
  actually override something, so a workspace whose repos all inherit shows one block rather than
  repo count times key count. A manifest that exists and cannot be parsed is now a warning:
  staying silent is worst in the command someone runs to find that out.

- **`hydra list` names its inverse.** `apply` consumes exactly what `list` emits, but nothing about
  the name said so — it was discoverable only from `apply --help` or `repo restore`'s hint, both of
  which you reach only if you already knew. `list` now emits
  `next: [{argv: ["hydra","apply","-"]}]`, so the round trip is visible from the end you are
  standing at.

- **`hydra apply` accepts a file path as well as `-`.** `< work.json` is shell composition, so
  stdin-only forced every caller that execs without a shell to plumb a pipe for a file it already
  had on disk. Same document, one more source; a missing file names itself rather than failing as
  an empty document.

### Fixed

- **Two documents claimed every command was convergent, and one command disproved it.** SKILL.md
  invariant 3 said "every command is convergent: doing it twice is a no-op that exits 0", and
  README repeated it. `hydra remove <name>` twice returns `worktree_unknown` at exit 1 — correctly,
  because removing something already gone is a "no such thing", not a silent success, and hiding a
  typo behind exit 0 is worse than reporting it.

  The invariant now states what holds: creation is convergent (`add`, `start`, `apply`, `repo add`,
  `repo restore`), and removing what is already gone is `worktree_unknown`/`topic_unknown`. The
  asymmetry is more useful to a caller than the blanket claim was, and it is checkable. Making
  `add` comply earlier this release fixed the one command that broke the rule for creation; it did
  not make the rule true for removal, and the documents were never corrected to match.

- **The project manifest no longer loses comments and unmodelled keys when hydra writes it.**
  `.hydra/config.yaml` is documented as the shareable, committable half of the state directory, and
  `Save` marshalled a closed struct over the whole file — so a manifest carrying
  `# Team manifest — reviewed in PR #412`, an inline `# do not move` and an `owners:` list lost all
  three to a single `hydra repo remove`, at exit 0, with no warning.

  `Save` now encodes into a `yaml.Node` and merges the prior file's comments and unmodelled keys onto
  it, amending the document instead of rebuilding it. Two rules the fix has to respect, both tested:
  unknown keys are carried only when the file on disk declares the version being written, so a
  migration that means to drop a field is not undone on the next save; and carrying applies only
  inside fixed-field structs, because `groups` and each group's repo map are what `repo remove`
  deletes from — at a map level a missing key means DELETED, and the first draft of this fix made
  `repo remove` a no-op by treating it as unknown. The merge walks the Go type beside the nodes to
  tell the two apart.

  Sequences are still replaced wholesale: a comment inside a list has no stable key to reattach to.
  The three other YAML writers named under 0.4.0's Known section are unchanged.

- **Registering a repository that already existed silently dropped its other fields.** Three sites
  replaced the whole manifest entry with a fresh struct
  (`SetRepo(group, alias, Repo{Remote, DefaultBranch})`), which on a re-registration lost
  `branch_pattern` and `branch_provider` — and would now lose `branches` and `carry`, so
  `repo restore` would strip the declaration it had just consumed and a convergent `repo add` would
  strip it on the second run. `RegisterRepo` reads the entry and sets only what changed. The
  comment-preserving writer could not rescue this and briefly masked it: these are known struct
  fields, so absent-in-struct means absent-on-disk.

- **`repo restore` discarded the clone's warnings**, so a fresh machine restoring from a manifest
  reported a clean rebuild of a workspace that could not run. They now ride the envelope per repo,
  which is the surface that needs them most.

- **The comment-preserving writer resurrected fields that had been cleared on purpose.**
  `omitempty` makes an empty field and an absent one identical in the encoded document, so carrying
  back "anything the new document lacks" undid deliberate clearing — and it briefly hid the
  whole-entry-replacement bug above, making `branches` and `carry` look like they survived a
  re-registration that had dropped them. Carrying is now confined to keys the struct genuinely does
  not model.

- **`hydra add` was the one command that broke the convergence invariant it documents.** SKILL.md
  invariant 3 promises every command is convergent — twice is a no-op that exits 0, reported as
  `skipped` — and agents are told to rely on the invariants instead of probing. `add` returned
  `worktree_exists` at exit 1 on a retry, so any provisioning script that re-ran its own steps died
  on the second `add`: a cloud-init retry, a resumed setup, an agent re-issuing a call after a
  timeout. `start`, `apply` and `clone` all already translated that code into a skip on the same
  `checkWorktreeNameConflict` call; `add` let it escape.

  `add` now reports `disposition: "created" | "skipped"` in its payload and exits 0 either way. The
  field is additive — the worktree object is embedded, so every path a caller already reads is
  unchanged. A directory held by a DIFFERENT branch is still `worktree_name_conflict` and still
  fails.

  Hooks fire only on creation, matching fanout's rule for `start`: re-running `add` must not
  re-provision a worktree that already exists. That makes SKILL.md's old advice wrong — it said to
  re-run `add` to retry a failed `post_add` — so the anti-pattern now points at
  `hooks run post_add --worktree <name>`, which is explicit about what it reruns.

## [0.4.0] - 2026-08-06

Breaking for anyone who never set a theme, and for anyone who ran `hydra status`
interactively expecting a table.

- The default theme is now `terminal`, which inherits the terminal's own palette and paints
  no background. Set `hydra config set theme hydra` for the previous fixed palette.
- `hydra status` with no arguments on a TTY opens an interactive board instead of printing a
  table. `--output text` and `--output json` are unchanged, and a pipe still gets the
  envelope, so scripts are unaffected.
- `hydra init` in an existing workspace returns `project_exists` instead of `internal`. Code
  that branched on `internal` for that case must be updated; `internal` never meant this.


### Added

- **`hydra status` on a TTY is the interactive board.** Eight mutating flows already prompted when
  run bare on a terminal, but every *reporting* command was flags-only, so exploring a workspace
  meant already knowing the flag that would answer the question. A form cannot close that: forms
  collect values and exit, and reporting needs view, refine and act in one place.

  The tool already had the convention that the interactive route is the same command rather than a
  separate noun; `hydra status` with no arguments on a TTY now opens the board. `ui` and `tui`
  remain registered as hidden aliases so muscle memory and scripts keep working.

  Browse, filter, and leave with a path, so it composes exactly like `switch`:
  `cd "$(hydra status)"`. The selection is written to stderr and stdout carries only the path.
  Quitting without selecting prints nothing and exits 0, because choosing not to choose is not a
  failure. `enter` prints the path and exits; `y` copies it via the clipboard so you can stay on
  the board.

  Two invariants from the rest of the tool are kept rather than reinvented: every refresh
  re-reads git instead of patching what is on screen, and the footer carries the
  `upstream_as_of` timestamp the counts were computed against, since hydra never fetches to
  answer a query.

  The filter vocabulary is identical to `--filter` (`dirty`, `behind`, `branch:<glob>`,
  plus `topic:<id>` mirroring the `--topic` flag), and it delegates to the same `path.Match`
  call, so `*` does not cross a `/` in either place. A first hand-rolled matcher diverged on
  exactly that case; a test now pins the two together. `ahead` is deliberately not a state
  word because `--filter` rejects it.

  Rendering reuses `collectWorktrees` and the topic index that `list` and `status` decorate
  from, so the three views cannot disagree. The board now carries aggregate counts, group and
  project headers that do not consume cursor indices, an `--against` column showing ahead/behind
  plus merged/unmerged when present, multi-project browsing via `--all`, and every status
  selector (`--topic`, `--repos`, `--group`, `--filter`, `--against`, `--all`) passed through
  as the board's opening state.

  This is browse-and-select only. Acting on a selection still means dropping to the existing
  `sync`, `remove` or `add` forms; wiring those in is the next increment.

- **A `terminal` theme.** Every role is an ANSI slot rather than hex, so the terminal resolves it
  from its own palette. `Background` and `Foreground` are empty on purpose: hydra paints neither.
  No OSC query and no config parsing, so it works over SSH and in tmux and follows live theme
  changes.

- **A first-party `hydra` theme.** The tool shipped five borrowed community palettes (tokyonight,
  catppuccin, dracula, nord, onedark) and none of its own, so its face was someone else's design
  decision. The new palette's role names are shared with `docs/guide.html`, which renders them
  on the same neutral dark ground: one design system rather than two palettes that resemble each
  other. `hydra config` still selects any of the previous five.

- **`scripts/gen-themes.py`**, which parses the ten `hydra` roles out of `internal/ui/themes/themes.go`
  and emits `contrib/ghostty/hydra` and `contrib/omp/hydra.json`. An ad-hoc ghostty theme had
  drifted from the tool and the documentation three times, caught only by eye; claiming a file is
  generated while the generator lives nowhere is the same class of unbacked assertion this project
  keeps fixing elsewhere. `make gate` now fails if either downstream theme drifts from the source
  palette.

- **`docs/guide.html`**, a single self-contained page covering install, the model, the
  interactive routes and the machine contract. No external requests, no fonts to download,
  no analytics. Every terminal panel is verbatim output from a real run, the interactive
  frames captured from a pty. Content is readable with JavaScript disabled.

### Changed

- **One visual language.** The `ui` board rendered plain — coloured foregrounds, a gutter caret,
  glyph-plus-word status, nothing painted — while `list` and `status` drew a rounded box masthead
  and filled status badges and `project` and `doctor` used a background-painted header chip. The
  plain rendering was judged better, so the others move to it rather than the reverse.

  Painting a background was also wrong on its own terms: a terminal program does not own the
  user's background, and `Foreground(BgDark)` coloured text using hydra's declared background,
  which is not the terminal's. `AppHeader`, `CleanBadge`, `ModifiedBadge`, `WarningBadge` and
  `hydraHeaderBox` are deleted; zero `Background(` calls remain in `internal/ui/styles` or
  `internal/cmd`. `status` renders its seven counts as one plain line, colouring only
  dirty/behind/detached when non-zero.

- **`terminal` is now the DEFAULT theme.** With the background-painted sites gone, an empty
  `Background` no longer strips a foreground anywhere, so hydra stops asserting a palette and
  inherits whatever the user configured. Every role is an ANSI slot; `Background` and
  `Foreground` are empty. Breaking for anyone who never set a theme. `hydra` remains selectable
  for a fixed look, and `contrib/` still generates matching Ghostty and omp themes for anyone who
  wants their terminal to follow hydra instead.

- **Hidden commands are now published in `hydra commands --output json`.** Hiding is a `--help`
  decision for humans; the command stays invocable, so it stays part of the machine contract. Both
  `hydra commands --output json` and the skill coverage test had filtered on cobra's
  `IsAvailableCommand` and dropped hidden routes; an agent must be able to discover a command that
  works.

### Fixed

- **Bare `hydra status` without a terminal briefly returned `needs_input` (exit 7).** `--output auto`
  has always meant JSON when stdout is not a terminal, and every script piping `hydra status`
  depends on it. The board is a TTY affordance and must never change what a pipe gets. e2e section
  19 now asserts that contract instead of the refusal.

- **`hydra init` in an existing workspace reported `internal`.** That code means "hydra is
  broken", and it is the one code an agent is told to treat as a tool defect, so a plain
  usage mistake cost a retry loop and a bug report. Both creation paths now return
  `project_exists`, and `init` no longer relabels a cause that already carries a stable
  code. Found while writing the guide.

### Known

- **A bad `--filter` value still reports `internal`** (`hydra list --filter ahead`). The
  message and `details.valid` are already correct and actionable; only the code is wrong.
  Fixing it properly needs a distinct code for caller error, which is an API addition and a
  versioning decision, so it is recorded rather than quietly patched.

- **Writing any YAML state file destroys unknown keys and every comment.** Four call sites
  marshal a closed Go struct over the whole file: `config/config.go:109` (the project
  manifest), `config/global/config.go:116` (global settings), `config/registry/registry.go:60`
  and `topic/topic.go:347`. `config/update.go` inherits it through `Save`.

  The manifest case is the serious one, because `.hydra/config.yaml` is documented as the
  shareable, committable file. Reproduced: a manifest carrying `# Team manifest — reviewed in
  PR #412`, a `ci:` block and an `owners:` list loses all three to a single
  `hydra repo remove`, producing a silent deletion in a reviewed diff.

  Not fixed here deliberately: it is unrelated to this release's work, and the fix needs a
  decision rather than a patch. A `map` round-trip would save unknown keys but still destroy
  comments, so it wants `yaml.Node`; and preserving unknowns unconditionally would resurrect
  fields that a future schema migration means to drop, so the rule has to be "preserve within
  the same schema version, and let an explicit migration remove them".


## [0.3.9] - 2026-08-06

Closes the last route by which one class of bug kept regenerating.

### Changed

- **The exit status is now derived from the envelope that reached stdout, not from what the
  command returned.** 0.3.5 made `success` impossible to claim beside a failure or a
  workspace-integrity warning, enforced at the one boundary every command's envelope passes
  through. But the *exit* still came from the command's return value, so a command could emit
  a corrected `partial` envelope and then return nil — exiting 0 while the caller had just
  been told something failed.

  `sync` did exactly that twice in the same release: once on its normal path, and again on its
  "nothing to pull" early return, which skipped the outcome logic entirely. Both were fixed
  individually in 0.3.7, which is the pattern this change ends: five releases fixed five
  instances of one class, each in the command where it was found, and each time it reappeared
  in whichever command aggregated next.

  A nil return is no longer read as proof of success. If the envelope said `partial` or
  `failure`, the process exits from that code's published mapping. Verified by deleting
  `sync`'s explicit error return and confirming the exit is still 4.

  For a consumer this is strictly narrowing: an exit status that was wrongly 0 becomes the
  correct non-zero. Nothing that exited correctly changes.

[0.4.0]: https://github.com/mssantosdev/hydra/compare/v0.3.9...v0.4.0
[0.3.9]: https://github.com/mssantosdev/hydra/compare/v0.3.8...v0.3.9

## [0.3.8] - 2026-08-06

### Fixed

- **A taken project name reported `project_unknown`**, which is the opposite problem: that
  code means the name is absent from the registry. Telling a caller its name was unknown when
  the name was correct and the collision was the point sent it to check its spelling. New code
  `project_exists` (exit 1), introduced in the release that added the wrong one.

### Verified, not changed

Five of six contradictions reported by an adversarial round did not reproduce, each checked
directly rather than taken on report:

- `partial_failure` maps to exit 4, confirmed by forcing one deterministically. The report of
  exit 1 came from `echo "exit=$?"` after a backgrounded command, which captures the launch
  rather than the command.
- Concurrent additions under the same alias leave the manifest and the checkout agreeing; the
  loser fails with `worktree_exists`.
- Three concurrent additions of the same remote and name all converge: every one reports
  success, the bare repository stays valid, `doctor` is clean, and the worktrees are correct.
- No lock hang: the same races complete in seconds under a timeout.
- A `bare_unregistered` warning seen while an addition is mid-flight is a snapshot of work in
  progress, not a fault.

Also checked and correct: a manifest truncated mid-document fails, one missing `version` gives
`config_version_unsupported`, and a valid manifest with no groups reports an empty workspace —
which is exactly what `hydra init` produces and cannot be distinguished from it.

[0.3.8]: https://github.com/mssantosdev/hydra/compare/v0.3.7...v0.3.8

## [0.3.7] - 2026-08-06

Round five stopped rebuilding workspaces and went after invariants instead: agents were given
the command surface up front and told to make the tool contradict itself. Branch names were
deliberately not `prod`/`stage`, and each repository's default branch deliberately was not the
first one passed to `--branches`, so two previously-untestable claims became testable.

### Fixed

- **One unreachable remote no longer stops `sync` from updating the others.** The pre-fetch
  loop returned on the first failure, so a single broken remote aborted the whole command:
  the envelope carried no summary and no counts, exit was 1, and repositories that were
  perfectly pullable were left stale. With three repositories and one broken remote, two were
  silently left behind. Unreachable remotes are now reported as `git_failed:`-coded warnings
  and the run continues, giving `outcome: partial` and exit 4. `run` already behaved this way;
  `sync` promising less was an inconsistency, not a policy.

- **`sync` reported `partial` while exiting 0**, and separately dropped the failure entirely
  once the reachable repositories were already current — the "nothing to pull" path took an
  early return that skipped the outcome logic, so an unreachable remote was visible on the
  first sync and gone on the second. Both paths now agree with the exit status.

- **`topic show` reported `present: true` for a member whose worktree had been deleted**, and
  `dangling: 0` alongside it — the one field a caller reads to find exactly that. Presence was
  taken from `git worktree list`, which keeps the registration after the directory is removed.
  `present` names a fact about disk, so it now stats the path.

- **`doctor --fix` suggested a command it could not complete.** The pruned-registration
  message interpolated a branch the check never carried, printing `hydra add api ` — an
  unrunnable command offered as the recovery. Worktree checks now carry their branch, and the
  suggestion is omitted rather than half-rendered when either half is unknown.

### Verified, not changed

- `hydra run`'s outcome, exit status and disk state agreed in every arrangement tried: none
  failing, one of four, two of four, three of four, and all four. Per-worktree output stayed
  correctly attributed with concurrency raised and with it forced to one at a time.
- Output larger than the cap is truncated with `stdout_truncated` set and `stdout_bytes`
  reporting the true size.
- `apply` does not roll back the worktrees it created when a later item fails, which is the
  intended behaviour: they exist and are reported.
- 13 of 14 `doctor` check ids were made to fire simultaneously — the first time most of them
  have been observed failing rather than passing.

[0.3.7]: https://github.com/mssantosdev/hydra/compare/v0.3.6...v0.3.7

## [0.3.6] - 2026-08-06

Round four probed surface the earlier rounds never touched — drift, hooks, multiple
workspaces, dirty state — and two adversarial reviews of round three named what to fix.

### Added

- **`upstream_as_of` on every worktree.** hydra never fetches to answer a query, so `ahead`
  and `behind` are computed against whatever remote refs are on disk. After an upstream push
  an agent saw `behind: 0` and `✓ clean` with nothing signalling staleness, and correctly
  called that dishonest. Each worktree now carries the time its remote refs were last
  fetched, from the bare repository's `FETCH_HEAD`, so `behind: 0` means "not behind as of
  this moment" rather than an unqualified claim. Null means the remote has never been
  fetched in this workspace. No fetch was added: querying stays cheap, it just stops
  overstating.

- **`default_branch` and `on_default_branch` on every worktree.** The worktree on a
  repository's default branch gets the bare alias as its directory; every other gets
  `alias-<branch-slug>`. Two agents independently concluded the suffix was decided by the
  ORDER of `--branches`, because in every test the default happened to be listed first —
  their data could not distinguish the two rules, so the inference was unfalsifiable rather
  than careless. The rule is now observable instead of inferred.

### Fixed

- **A rejected `hydra init` no longer leaves a workspace behind.** Registration is the last
  step and the one that fails on a name collision, so `config.yaml` was left on disk: the
  retry under a different name then found a manifest it had not created. The manifest is now
  removed on that failure (never the directory, which may hold something of yours), the code
  is `project_unknown` rather than `internal`, and a `next[]` points at
  `hydra project ls` to show which names are taken.

- **`worktree_missing_on_disk` covered two states with one `fixable` value**, so one of them
  was always mislabelled. An agent dead-ended on the wrong half: `--fix` pruned the
  registration, `hydra add` then failed `worktree_name_conflict` because the directory was
  still there, and `repo add --adopt` refused because it is not a git checkout — the actual
  recovery was a manual `mv` that nothing had mentioned. Now split:

  | check | state | fixable |
  |---|---|---|
  | `worktree_missing_on_disk` | path absent | yes — prune, then `hydra add` |
  | `worktree_orphaned_dir` | path present, not a valid worktree | no — move it aside first |

  Both messages name the exact follow-up command, and `--fix`'s prune result now says what
  to run next instead of stopping at "pruned".

### Verified, not changed

- Uncommitted work is properly defended: `remove` refuses with `worktree_dirty` (exit 5) and
  treats untracked files exactly like modified ones, `sync` refuses without a policy
  (`needs_input`, exit 7), and an unmerged branch is not deleted without `--force`. Work is
  destroyed only behind an explicit `--force` or `--dirty reset`.
- `--output text` is honoured in every flag position across `list`, `status` and `repo list`.
- Lifecycle hooks: a failing hook does not roll back or destroy a correctly-created
  worktree, and `optional: true` makes one non-fatal.

[0.3.6]: https://github.com/mssantosdev/hydra/compare/v0.3.5...v0.3.6

## [0.3.5] - 2026-08-05

The release that fixes the *class* rather than the fifth instance of it.

Four rounds of handing the binary to agents with no knowledge of hydra each found the same
shape of bug, including in code written specifically to prevent the previous one. Two
independent adversarial reviews named it identically: **hydra's per-item logic was correct,
but its aggregate verdict was computed from "my code path finished" rather than "every item
I claimed to cover actually verified."** Every fix so far had been point-local, so the class
regenerated in whichever command aggregated next.

### Breaking

- **`success` may no longer co-exist with a failure or a workspace-integrity warning.** The
  outcome is now corrected at the single boundary every command's envelope passes through,
  rather than trusted from the command that produced it. Two consequences a consumer will
  see:

  - `hydra add` with a failing hook reported `outcome: success` with an empty `warnings`
    array and then exited 1 — the hook failure was absent from the envelope entirely. It now
    reports `outcome: partial` with `error.code: hook_failed`, and the summary still
    truthfully says the worktree was created, because it was.
  - `hydra status` reported `9 worktree(s), all clean` and exit 0 while a registered worktree
    was missing from disk, the absence appearing only in `warnings[]`. It now reports
    `outcome: partial`, **exit 4**, a summary that says so, and a `next[]` pointing at
    `doctor`. Counts describe the worktrees status could inspect, which is not the same set
    as the ones it was asked about.

- **Warnings that describe a fault now carry a hydra error code.** Raw git text reached
  callers verbatim and in the system locale — `fatal: cannot change to '…': Arquivo ou
  diretório inexistente` — which nothing downstream could match on. Such warnings are now
  prefixed with `worktree_unknown:`, `git_failed:` or `bare_missing:`. The git message is
  kept, since it names the real cause; it simply no longer arrives uncoded.

### Added

- `output.Coverage{Claimed, Inspected, Failed}` and `Derive()`, so a verdict is computed
  from what was actually covered: anything failed and nothing survived is `failure`, some
  failed is `partial`, fewer inspected than claimed is `partial`, otherwise `success`. Tested
  against the five bugs that motivated it, including the one where every item failed and the
  outcome still read `partial`.

### Verified, not changed

- `--output text` is honoured in every flag position, across `list`, `status` and
  `repo list`. Two agents contradicted each other on this; the matrix settles it.
- Protection of uncommitted work is sound. `remove` refuses with `worktree_dirty` (exit 5),
  treating untracked files the same as modified ones; `sync` refuses without a policy
  (`needs_input`, exit 7). Work is destroyed only behind an explicit `--force` or
  `--dirty reset`.

[0.3.5]: https://github.com/mssantosdev/hydra/compare/v0.3.4...v0.3.5

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

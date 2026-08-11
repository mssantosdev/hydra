# The workspace model

What a hydra workspace is made of, who writes each part of it, how defaults are declared and
resolved, and what is missing.

Status: **implemented**. Every item under Build order has landed, plus the level work (schema 3) and
the hierarchy. Supersedes the change-pivot plan on the noun.

## The problem

hydra has five nouns — project, group, repo, worktree, topic. The two carrying the most
architectural weight have no body.

A group is `map[string]map[string]Repo` (`config/config.go:22`): a bare map key with nowhere to put
anything, no command surface, and the only envelope treating it as an object is the one that
*deletes* it (`prune.removed_groups`). A topic has no properties, no scope, and no events of its
own. `hydra where` resolves both group and topic from the working directory, and **nothing consumes
either**.

That is why a coherent description of the product was hard to write: the two most important concepts
are implicit. Every item here gives an implicit noun a body.

## State, and who writes each part

Four categories of state, one writer each. Everything below follows from this table.

| category | lives in | written by | examples |
|---|---|---|---|
| **registered** | manifest | hydra — it cannot rediscover alias→remote | repos, groups, remotes |
| **declared** | manifest | humans, explicitly | `branches`, conventions, hooks, `carry` |
| **observed** | git, never stored | nobody | which worktrees exist, dirty, ahead/behind |
| **recorded** | `.hydra/state.yaml` | hydra | topic membership — git cannot know it |

Worktrees are never tracked by hydra; they are discovered from `git worktree list --porcelain`.
Verified: deleting `state.yaml` leaves every worktree listed and loses only the topics.

Consequence: WIP needs no heuristic. It is `observed − declared`. An earlier draft proposed
"topic-less means baseline", which is wrong — an ad-hoc `feat/x` worktree has no topic either.

## Known defects

None outstanding in this model. Every defect this document recorded is closed, and each is pinned
by a test rather than by this list:

| was | closed by | pinned by |
|---|---|---|
| the manifest writer destroyed comments and unknown keys | 0.5.0, hardened in 0.5.2 | `internal/config/writer_test.go` |
| `add` was not convergent | 0.5.0 | `TestAdd_IsConvergent`, `TestAdd_ConvergenceRequiresTheRequestedDirectory` |
| manifest defaults had no read path | 0.5.0 | `internal/config/explain_test.go` |
| `defaults` and `hooks` were read only at the workspace level | 0.5.2 | `internal/config/levels_test.go` |
| hooks had no timeout | 0.4.0 | `internal/hooks` |
| `HYDRA_TOPIC` was absent from the hook environment | 0.4.0 | `hooks.EnvKeys`, asserted in `scripts/e2e.sh` |

A defect belongs here only while no test covers it. Once one does, the test is the record — a list
in a document drifts out of date silently, which is the failure this whole release was about.

## The declared shape of a workspace

```yaml
groups:
  svc:
    repos:
      api:
        remote: git@github.com:org/api.git
        default_branch: master
        branches: [master, stage, prod]   # the declared shape
```

`branches:` is an additive optional field on `Repo`, needing no schema bump of its own. It shipped
alongside the group-as-object work, so the example above shows the schema-3 nesting. Repo level
only: a group-level list would assume repos share branch names (`master` vs `main`).

| operation | command |
|---|---|
| declare at registration | `hydra repo add <url> --as api --branches master,stage,prod` |
| change later | `hydra repo set api --branches master,stage,prod` |
| read | `hydra repo list` |
| apply | `hydra repo restore` |

`repo set` is the only new subcommand. `repo branches <url|alias>` is taken — it lists a *remote's*
branches without cloning — and `set` generalises to `--base-branch` and `--branch-pattern` later
instead of each needing its own verb. No `repo show`: `repo list --output json` already returns
per-repo data, so the declared set is a field.

`repo add --branches` now **persists**. This is a behaviour change to a shipped flag, and an earlier
draft argued against it on exactly that ground. The argument was wrong: `repo add --branches
master,stage` is a user naming a set, while `add api feat/x` is a user doing work. Recording an
explicitly typed instruction is not the same class as silently recording WIP. Same worktrees created,
one additive manifest field.

**Rules.**

- `--all` records the concrete branch list at that moment, never a wildcard. A wildcard would make
  the declared shape change when a colleague pushes a branch.
- Declaring fewer never deletes. `repo set api --branches master` leaves an existing `stage` worktree
  alone; it becomes drift. Removal stays `hydra remove`.
- A declared branch missing from the remote is a warning naming repo and branch, never a failure.
- `branches` **replaces** on override rather than appending — it is set membership, not
  accumulation, and append would make narrowing inexpressible.
- Drift is a `doctor` check with `fixable: false`. Not `status`, which reports work state, not
  conformance.

**Human path.** `repo add` and `repo set` open the existing multi-select at `clone.go:550` —
`huh.NewMultiSelect`, `/` to filter, `Height(15)`, pre-selection assigned before the form is built
because huh binds the pointer. `repo set` pre-checks the declared set. The options come from the
remote's real branches, so a branch that does not exist cannot be declared. Reused, not forked.

An earlier draft proposed locking topic-owned branches out of that picker. Dropped: declaring a
branch creates a worktree, which is cheap, removable, and visible in a manifest diff under review.
There is no rule that a topic-owned branch cannot be declared.

**Agent path.** `hydra config set repo.api.branches master,stage,prod` — scope in the key, flat and
scriptable, plus `config unset`. Humans never browse a settings tree; they set defaults inside the
command that needs them.

## Reading and setting configuration

`config show` reports everything that applies here, resolved, with provenance:

```
theme            terminal         global       ~/.config/hydra/config.yaml
editor           code             global       ~/.config/hydra/config.yaml
base_branch      develop          group svc    .hydra/config.yaml
branch_pattern   {kind}/{slug}    project      .hydra/config.yaml
branches         master, stage    repo api     .hydra/config.yaml
```

Outside a workspace, the global rows only — no flag needed, there is no manifest to read. The source
column is what disambiguates the two files both named `config.yaml`; renaming the manifest is not an
option, it is shipped and documented.

This is a correction, not an addition: `config show` currently under-delivers on its own name. It
also makes `config set` coherent, since `show` and `set` finally address the same key space.

Key namespace: bare keys (`theme`, `editor`) stay global; `project.`, `group.<name>.` and
`repo.<alias>.` prefixes name the level. The prefixes avoid ambiguity when a group and a repo share
a name. `config set` already publishes its key space through `details.one_of`, so agents discover
new keys for free.

## Build order

Solid base first. Two of the top three are bugs, which is what that means.

1. **Manifest writer preserves comments and unknown keys** — prerequisite for everything below.
2. **`add` convergence** — adopt `start`'s `skipped` shape.
3. **`branches:`, `repo set`, `repo restore` honouring the declared set.**
4. **`carry:`** — files a worktree needs that git ignores. Bare form copies from the source worktree
   (`--from`'s worktree when that branch has one, else the repo's default-branch worktree, reported
   in the envelope); `from:` form reads a workspace path. Placement, not a hook: hydra already owns
   layout and already knows the source, where a hook would have to rebuild a path `--as` can
   override. A missing source is a warning, never `hook_failed`. `apply` replays structure, not
   secrets — only `from:` entries survive a fresh machine, and that limit is documented rather than
   discovered.
5. **Hook timeout, `HYDRA_TOPIC`, `HYDRA_SOURCE_WORKTREE`.**
6. **`config show` reads manifest defaults with provenance.**

Then the level work: schema 3, group as an object with `path`, `defaults`, `hooks`, `carry`,
resolution workspace → group → repo, scalars nearest-wins and lists appending with `override: true`
as the explicit escape.

Then hierarchy: `parent:` opt-in with flat as the default, closeability **derived** and
**member-granular** — for each child member `(repo, branch)`, the parent must have a member in the
same repo to merge into; if it does not, that is `no_integration_target`, never a silent pass.
`topic close` is the gate; once-per-operation topic events (`post_topic_start`, `pre_topic_close`,
`post_topic_close`, `pre_topic_remove`) carry the member set.

## Design rules

**hydra must not encode what a group means.** `go-projects`/`java-projects` and `backend`/`web` are
equally valid groupings. Every level carries the same keys; the resolution chain is the only rule;
what belongs where is the user's modelling choice. Never illustrate a config kind at only one level,
and never label a level "for tooling" or "for convention".

**Store what git cannot know; derive everything else.** No cached worktree lists, branch existence,
ahead/behind or dirty status. `upstream_as_of` comes from the bare repo's `FETCH_HEAD`.

**Flat is the default.** Depth is opt-in and must never tax someone who did not ask for it. `--topic X`
never silently means "subtree".

**hydra never merges, rebases, resolves a conflict, or talks to a forge.** It reports whether you
*can* close; you merge.

**Local tool, composable.** The deployment model is one developer, one machine. Sharing is
composition: the manifest is a file, hooks are code, and both compose without hydra growing a server
or a concept of a user. A "once across machines" guarantee is a script's problem, not hydra's — every
hook already runs once per operation.

## Rejected

| | why |
|---|---|
| `depends_on` | pure assertion with one consumer (making `topic close` refuse), about a fact the user typed. `parent:` has a git-observable consequence; this has none. It rots silently when its target is descoped, and the strongest story for it resolves in CI, not on a laptop. `links:` records a note without pretending to enforce it. |
| sub-topics by naming convention | reintroduces the fuzzy destructive handle `topic.go` rejected. That refusal is about *name-inferred* hierarchy and does not block `parent:`. |
| demoting groups | reversed — they partition the workspace so it stays comprehensible at scale. |
| `run_once` on hooks | every hook already runs once per operation; there is no loop. |
| a `baseline` or `defaults` noun | a property, not a sixth noun. |
| declared vs topic-owned exclusivity | invented; no such rule is needed. |
| `hydra group ls\|show`, `groups[]` aggregate, "topic owns its repo set" | no story survived — the same test that cut `depends_on`. |

## Open questions

**Topic membership is the least durable data in hydra, and it is the only concept hydra invented.**
It lives in one gitignored local file. Lose it and the worktrees survive while the structure of the
work is gone, and hydra refuses to re-infer it from branch names. `Member` is `{repo, branch}` with
no paths, so the content is machine-independent and could be committed; the cost is merge conflicts
on a machine-written file, in the same writer that destroys comments. Alternatives: lean on
`list --output json` as the backup (it already carries topics — a full workspace transfer was
verified this session), or state plainly that topics are ephemeral working state.

**Deployment ladder.** Local single-user is what the code is. L0 local, L1 agent-driven and L2
ephemeral/CI work today. L3 (shared box, whole team) works mechanically — same filesystem, so the
state lock is valid — but hydra has no concept of a user, so `status` mixes everyone's work with no
owner field to filter on. L4 (shared network disk) breaks the advisory lock and is where the
change-pivot plan's SQLite argument returns on its own merits. L5 (committed topology) is possible
but wants a conflict-tolerant format.

## Superseded

The change-pivot plan is superseded on the noun. `topic` shipped in v0.4.0 across 42 commands,
`topic_unknown`, SKILL.md and the published guide; renaming to `change` now costs far more than when
that plan was written. None of it landed: no `internal/change`, no SQLite, and `changes` was never
renamed to `dirty_files`. Its SQLite argument (concurrent-writer safety, a future operation journal)
is independent of the naming and should be re-evaluated separately.

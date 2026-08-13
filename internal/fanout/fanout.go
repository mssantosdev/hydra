// Package fanout runs one operation across many worktrees.
//
// It exists because hydra had two executors — sync's and clone's — that differed
// in eleven ways, of which most were incoherences rather than requirements:
// nondeterministic result order, hooks batched after all items instead of per
// item, hook failure aborting the remaining work, and no convergence (clone
// reported git_failed on an already-complete repo). Those are fixed here once
// rather than twice.
//
// The engine deliberately knows nothing about git, hooks, config, or rendering.
// Callers supply the operation, the hook runner and the reporter, which is what
// keeps it testable without a repository on disk.
package fanout

import (
	"context"
	"github.com/mssantosdev/hydra/internal/output"
	"sort"
	"sync"
	"time"
)

// Target is one unit of desired state: this branch, checked out at this path, for
// this repo. sync and start differ only in the ItemOp applied to it.
//
// Identity is flat strings rather than a richer repo type because fanout must not
// import the command package that owns one — and because everything the engine
// itself needs (grouping by bare repo, stable ordering, hook context) is here.
// Callers carrying more per-item state keep it in the ItemOp closure.
type Target struct {
	Group    string
	Repo     string
	Branch   string
	Path     string
	BareRepo string
}

// Key orders targets for humans and agents alike. Channel-drain order is
// accidental and unstable between runs, which makes output undiffable.
func (t Target) Key() string { return t.Group + "/" + t.Repo + "/" + t.Branch }

// Disposition is the outcome of converging one target.
//
// The three values are exhaustive by design: an operation that cannot say which
// of these happened has not actually checked reality.
type Disposition string

const (
	// Created means the operation changed something to reach the desired state.
	Created Disposition = "created"
	// Skipped means reality already matched. This is a SUCCESS, not a failure —
	// re-running a converged operation must be a no-op that exits 0.
	Skipped Disposition = "skipped"
	// Failed means the desired state was not reached.
	Failed Disposition = "failed"
)

// ItemResult is what happened to one target.
type ItemResult struct {
	Target      Target
	Disposition Disposition
	// Reason explains a skip or a failure in one line. It is required for both:
	// "skipped" with no reason is indistinguishable from "did nothing silently".
	Reason string
	Err    error
	// HookWarnings are collected rather than fatal, so one repo's bad hook cannot
	// abort the rest of the fan-out.
	HookWarnings []*output.Diagnostic
	Duration     time.Duration
}

// ItemOp performs the operation for one target.
//
// It MUST be convergent: check reality first and return Skipped rather than
// failing when the target is already correct. This is the contract that fixes
// clone reporting git_failed on a repo whose branches all already existed.
type ItemOp func(ctx context.Context, t Target) ItemResult

// HookFunc runs the per-item hook event. It returns warnings and a fatal error;
// fanout treats the error as a warning on that item and keeps going.
type HookFunc func(ctx context.Context, t Target) ([]*output.Diagnostic, error)

// Reporter observes item lifecycle so a TTY caller can render live progress.
// Implementations MUST be safe for concurrent use: items in different repos run
// in parallel.
type Reporter interface {
	Start(t Target)
	Finish(r ItemResult)
}

// Config tunes one fan-out.
type Config struct {
	// Jobs bounds how many BARE REPOS run concurrently. 0 means one per repo,
	// capped at maxAutoJobs.
	Jobs int

	// SerialPerRepo forces one item at a time within a single bare repository.
	//
	// This is required for CREATION and wrong for pulling, which was measured
	// rather than assumed: 8 concurrent `worktree add` with upstream config leave
	// only 3 successes, the rest failing on `could not lock config file config`
	// and — the dangerous part — leaving a worktree with no upstream at all, a
	// silent partial. Concurrent `pull --ff-only` after a pre-fetch is 4/4.
	// A blanket serialise-everything rule would cost roughly 2x in sync for
	// nothing.
	SerialPerRepo bool

	// Hook fires per item, immediately after that item succeeds. sync's
	// batch-then-hooks ordering and its hook fail-fast are both dropped.
	Hook HookFunc

	// Timeout bounds a single item. Zero means no per-item bound.
	Timeout time.Duration

	Reporter Reporter
	Rollback RollbackScope
}

// maxAutoJobs caps inferred concurrency. Beyond this, git's own locking and disk
// dominate and more goroutines only add contention.
const maxAutoJobs = 8

// RollbackScope lets a caller undo only what THIS call created.
//
// It is caller policy, not engine behaviour: the engine records nothing and
// deletes nothing. clone's rule — undo only on total failure, and only the things
// this invocation brought into existence — is expressible here; sync passes the
// zero value.
type RollbackScope struct {
	// Enabled still only acts when EVERY target failed. A partial success must
	// never be rolled back: that would destroy work that succeeded.
	Enabled            bool
	CreatedThisCall    []string
	BareWasNew         bool
	RegistrationWasNew bool
}

// ShouldRollback reports whether the caller should undo its own side effects.
func (s RollbackScope) ShouldRollback(results []ItemResult) bool {
	if !s.Enabled || len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.Disposition != Failed {
			return false
		}
	}
	return true
}

// Run converges every target and returns one result per target, sorted by
// Target.Key().
//
// Concurrency is per bare repository: repos proceed in parallel up to Jobs, and
// within a repo items are serial or parallel according to SerialPerRepo. A nil or
// empty target list is not an error — it is a converged workspace.
func Run(ctx context.Context, targets []Target, cfg Config, op ItemOp) []ItemResult {
	if len(targets) == 0 {
		return nil
	}

	byRepo := groupByBare(targets)
	jobs := cfg.Jobs
	if jobs <= 0 {
		jobs = min(len(byRepo), maxAutoJobs)
	}

	results := make([]ItemResult, 0, len(targets))
	var mu sync.Mutex
	var wg sync.WaitGroup
	gate := make(chan struct{}, jobs)

	for _, group := range byRepo {
		wg.Add(1)
		go func(group []Target) {
			defer wg.Done()
			gate <- struct{}{}
			defer func() { <-gate }()

			collected := runRepoGroup(ctx, group, cfg, op)
			mu.Lock()
			results = append(results, collected...)
			mu.Unlock()
		}(group)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].Target.Key() < results[j].Target.Key()
	})
	return results
}

// runRepoGroup converges every target of one bare repository.
func runRepoGroup(ctx context.Context, group []Target, cfg Config, op ItemOp) []ItemResult {
	if cfg.SerialPerRepo {
		out := make([]ItemResult, 0, len(group))
		for _, t := range group {
			out = append(out, runItem(ctx, t, cfg, op))
		}
		return out
	}

	out := make([]ItemResult, len(group))
	var wg sync.WaitGroup
	for i, t := range group {
		wg.Add(1)
		go func(idx int, t Target) {
			defer wg.Done()
			out[idx] = runItem(ctx, t, cfg, op)
		}(i, t)
	}
	wg.Wait()
	return out
}

// runItem applies the operation to one target, then its hook.
func runItem(ctx context.Context, t Target, cfg Config, op ItemOp) ItemResult {
	if cfg.Reporter != nil {
		cfg.Reporter.Start(t)
	}

	started := time.Now()
	itemCtx := ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		itemCtx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	result := op(itemCtx, t)
	// The operation reports its own outcome, but identity and timing belong to the
	// engine so no ItemOp can forget to set them.
	result.Target = t
	result.Duration = time.Since(started)

	// Hooks run per item, immediately after THAT item succeeds — not batched at the
	// end. A hook failure degrades to a warning: the git work already landed, and
	// aborting the remaining repos would strand the fan-out half-done for a reason
	// unrelated to them.
	if cfg.Hook != nil && result.Disposition == Created {
		warnings, err := cfg.Hook(itemCtx, t)
		result.HookWarnings = append(result.HookWarnings, warnings...)
		if err != nil {
			result.HookWarnings = append(result.HookWarnings,
				output.Notef(output.CodeHookFailed, "%v", err))
		}
	}

	if cfg.Reporter != nil {
		cfg.Reporter.Finish(result)
	}
	return result
}

// groupByBare buckets targets by bare repository, in first-seen order made stable
// by sorting the targets first.
func groupByBare(targets []Target) [][]Target {
	sorted := make([]Target, len(targets))
	copy(sorted, targets)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Key() < sorted[j].Key() })

	index := make(map[string]int)
	var groups [][]Target
	for _, t := range sorted {
		key := t.BareRepo
		if key == "" {
			// No bare path means nothing to contend over, so the target is its own
			// group and never serialises behind an unrelated one.
			key = "\x00" + t.Key()
		}
		if at, ok := index[key]; ok {
			groups[at] = append(groups[at], t)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, []Target{t})
	}
	return groups
}

// Summarize splits results by disposition and picks the process exit code.
//
// Skipped counts as success: a converged run must exit 0. The exit code is
// computed here so no command invents its own mapping.
func Summarize(results []ItemResult) (created, skipped, failed []ItemResult, exit int) {
	for _, r := range results {
		switch r.Disposition {
		case Created:
			created = append(created, r)
		case Skipped:
			skipped = append(skipped, r)
		case Failed:
			failed = append(failed, r)
		}
	}

	switch {
	case len(failed) == 0:
		exit = 0
	case len(created) == 0 && len(skipped) == 0:
		// Nothing succeeded: this is a plain failure, not a partial one.
		exit = 1
	default:
		exit = 4 // partial_failure
	}
	return created, skipped, failed, exit
}

package fanout

import (
	"context"
	"errors"
	"fmt"
	"github.com/mssantosdev/hydra/internal/output"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func target(group, repo, branch string) Target {
	return Target{
		Group:    group,
		Repo:     repo,
		Branch:   branch,
		Path:     "/tmp/" + repo + "-" + branch,
		BareRepo: "/tmp/.bare/" + repo + ".git",
	}
}

// ok is an ItemOp that always converges by creating.
func ok(_ context.Context, _ Target) ItemResult {
	return ItemResult{Disposition: Created}
}

// Results must come back in a stable order regardless of which goroutine finished
// first. Output order must be deterministic so runs can be diffed by a human or an agent.
func TestRun_OrdersResultsDeterministically(t *testing.T) {
	targets := []Target{
		target("frontend", "web", "main"),
		target("backend", "api", "stage"),
		target("backend", "api", "main"),
		target("backend", "core", "main"),
	}

	want := []string{
		"backend/api/main",
		"backend/api/stage",
		"backend/core/main",
		"frontend/web/main",
	}

	// Repeat: a single pass can pass by luck when scheduling happens to agree.
	for attempt := range 20 {
		results := Run(context.Background(), targets, Config{}, func(_ context.Context, t Target) ItemResult {
			// Make the fast/slow split adversarial to arrival order.
			if t.Repo == "api" {
				time.Sleep(2 * time.Millisecond)
			}
			return ItemResult{Disposition: Created}
		})
		if len(results) != len(want) {
			t.Fatalf("attempt %d: got %d results, want %d", attempt, len(results), len(want))
		}
		for i, key := range want {
			if got := results[i].Target.Key(); got != key {
				t.Fatalf("attempt %d: results[%d] = %q, want %q", attempt, i, got, key)
			}
		}
	}
}

// Creation must serialise per bare repo. Concurrent `worktree add` with upstream
// config leaves worktrees with no upstream — a silent partial, which is why this is
// a hard requirement rather than a tuning knob.
func TestRun_SerialPerRepoNeverOverlapsWithinARepo(t *testing.T) {
	var active, maxActive int32
	var mu sync.Mutex

	targets := []Target{
		target("backend", "api", "a"),
		target("backend", "api", "b"),
		target("backend", "api", "c"),
		target("backend", "api", "d"),
	}

	Run(context.Background(), targets, Config{SerialPerRepo: true}, func(_ context.Context, _ Target) ItemResult {
		now := atomic.AddInt32(&active, 1)
		mu.Lock()
		if now > maxActive {
			maxActive = now
		}
		mu.Unlock()
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return ItemResult{Disposition: Created}
	})

	if maxActive != 1 {
		t.Errorf("max concurrent items within one repo = %d, want 1", maxActive)
	}
}

// Serialising within a repo must NOT serialise across repos, or sync would pay
// roughly 2x for nothing.
func TestRun_SerialPerRepoStillParallelAcrossRepos(t *testing.T) {
	var active, maxActive int32
	var mu sync.Mutex

	var targets []Target
	for i := range 4 {
		targets = append(targets, target("backend", fmt.Sprintf("repo%d", i), "main"))
	}

	Run(context.Background(), targets, Config{SerialPerRepo: true}, func(_ context.Context, _ Target) ItemResult {
		now := atomic.AddInt32(&active, 1)
		mu.Lock()
		if now > maxActive {
			maxActive = now
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return ItemResult{Disposition: Created}
	})

	if maxActive < 2 {
		t.Errorf("max concurrent repos = %d, want more than 1: distinct repos must overlap", maxActive)
	}
}

// Jobs bounds concurrent bare repos.
func TestRun_JobsBoundsConcurrentRepos(t *testing.T) {
	var active, maxActive int32
	var mu sync.Mutex

	var targets []Target
	for i := range 8 {
		targets = append(targets, target("backend", fmt.Sprintf("repo%d", i), "main"))
	}

	Run(context.Background(), targets, Config{Jobs: 2}, func(_ context.Context, _ Target) ItemResult {
		now := atomic.AddInt32(&active, 1)
		mu.Lock()
		if now > maxActive {
			maxActive = now
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return ItemResult{Disposition: Created}
	})

	if maxActive > 2 {
		t.Errorf("max concurrent repos = %d, want at most Jobs=2", maxActive)
	}
}

// Targets sharing no bare repo must not serialise behind each other just because
// BareRepo is empty.
func TestRun_EmptyBareRepoDoesNotSerialiseUnrelatedTargets(t *testing.T) {
	var active, maxActive int32
	var mu sync.Mutex

	targets := []Target{
		{Group: "g", Repo: "a", Branch: "main"},
		{Group: "g", Repo: "b", Branch: "main"},
		{Group: "g", Repo: "c", Branch: "main"},
	}

	Run(context.Background(), targets, Config{SerialPerRepo: true}, func(_ context.Context, _ Target) ItemResult {
		now := atomic.AddInt32(&active, 1)
		mu.Lock()
		if now > maxActive {
			maxActive = now
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		return ItemResult{Disposition: Created}
	})

	if maxActive < 2 {
		t.Errorf("max concurrent = %d: targets with no bare repo share no lock and must overlap", maxActive)
	}
}

// Hooks fire per item, right after that item succeeds — not batched after all targets
// complete.
func TestRun_HookRunsPerSucceedingItem(t *testing.T) {
	var mu sync.Mutex
	var hooked []string

	targets := []Target{
		target("backend", "api", "main"),
		target("backend", "api", "stage"),
		target("frontend", "web", "main"),
	}

	results := Run(context.Background(), targets, Config{
		Hook: func(_ context.Context, t Target) ([]*output.Diagnostic, error) {
			mu.Lock()
			hooked = append(hooked, t.Key())
			mu.Unlock()
			return nil, nil
		},
	}, func(_ context.Context, t Target) ItemResult {
		// The middle target is already correct, so no hook should fire for it.
		if t.Branch == "stage" {
			return ItemResult{Disposition: Skipped, Reason: "already up to date"}
		}
		return ItemResult{Disposition: Created}
	})

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if len(hooked) != 2 {
		t.Fatalf("hook fired %d times (%v), want 2 — skipped items must not fire hooks", len(hooked), hooked)
	}
}

// A failing hook must not abort the fan-out. Remaining targets must still run and record
// hook warnings on the offending item only.
func TestRun_HookFailureDegradesToWarningAndContinues(t *testing.T) {
	targets := []Target{
		target("backend", "api", "main"),
		target("backend", "core", "main"),
		target("frontend", "web", "main"),
	}

	results := Run(context.Background(), targets, Config{
		Hook: func(_ context.Context, t Target) ([]*output.Diagnostic, error) {
			if t.Repo == "api" {
				return []*output.Diagnostic{output.Notef("", "hook said something")}, errors.New("hook exited 1")
			}
			return nil, nil
		},
	}, ok)

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3: a hook failure must not abort the rest", len(results))
	}

	_, _, failed, exit := Summarize(results)
	if len(failed) != 0 {
		t.Errorf("a hook failure must not mark the git work failed, got %d failures", len(failed))
	}
	if exit != 0 {
		t.Errorf("exit = %d, want 0: the operation itself succeeded", exit)
	}

	for _, r := range results {
		if r.Target.Repo != "api" {
			continue
		}
		if len(r.HookWarnings) != 2 {
			t.Errorf("api hook warnings = %v, want the output line and the error", r.HookWarnings)
		}
	}
}

// Skipped is a success. A converged re-run must exit 0 even when every item was skipped.
func TestSummarize_ConvergedRunExitsZero(t *testing.T) {
	results := []ItemResult{
		{Target: target("backend", "api", "main"), Disposition: Skipped, Reason: "already present"},
		{Target: target("backend", "api", "stage"), Disposition: Skipped, Reason: "already present"},
	}

	created, skipped, failed, exit := Summarize(results)
	if len(created) != 0 || len(skipped) != 2 || len(failed) != 0 {
		t.Errorf("created/skipped/failed = %d/%d/%d, want 0/2/0", len(created), len(skipped), len(failed))
	}
	if exit != 0 {
		t.Errorf("exit = %d, want 0: nothing failed", exit)
	}
}

func TestSummarize_ExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		disps []Disposition
		want  int
	}{
		{name: "all created", disps: []Disposition{Created, Created}, want: 0},
		{name: "all skipped", disps: []Disposition{Skipped}, want: 0},
		{name: "mixed created and failed", disps: []Disposition{Created, Failed}, want: 4},
		{name: "skipped and failed is still partial", disps: []Disposition{Skipped, Failed}, want: 4},
		{name: "every item failed", disps: []Disposition{Failed, Failed}, want: 1},
		{name: "nothing at all", disps: nil, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var results []ItemResult
			for i, d := range tc.disps {
				results = append(results, ItemResult{
					Target:      target("g", "r", fmt.Sprintf("b%d", i)),
					Disposition: d,
				})
			}
			if _, _, _, exit := Summarize(results); exit != tc.want {
				t.Errorf("exit = %d, want %d", exit, tc.want)
			}
		})
	}
}

// Rollback is only safe when EVERY target failed. Undoing after a partial success
// would destroy work that landed.
func TestRollbackScope_ShouldRollback(t *testing.T) {
	all := func(disps ...Disposition) []ItemResult {
		var out []ItemResult
		for i, d := range disps {
			out = append(out, ItemResult{Target: target("g", "r", fmt.Sprintf("b%d", i)), Disposition: d})
		}
		return out
	}

	tests := []struct {
		name    string
		scope   RollbackScope
		results []ItemResult
		want    bool
	}{
		{name: "disabled", scope: RollbackScope{}, results: all(Failed), want: false},
		{name: "every target failed", scope: RollbackScope{Enabled: true}, results: all(Failed, Failed), want: true},
		{name: "one succeeded", scope: RollbackScope{Enabled: true}, results: all(Failed, Created), want: false},
		{name: "one skipped counts as success", scope: RollbackScope{Enabled: true}, results: all(Failed, Skipped), want: false},
		{name: "no results", scope: RollbackScope{Enabled: true}, results: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.scope.ShouldRollback(tc.results); got != tc.want {
				t.Errorf("ShouldRollback = %v, want %v", got, tc.want)
			}
		})
	}
}

// The engine owns identity and timing so no ItemOp can forget to set them.
func TestRun_EngineStampsTargetAndDuration(t *testing.T) {
	results := Run(context.Background(), []Target{target("backend", "api", "main")}, Config{},
		func(_ context.Context, _ Target) ItemResult {
			time.Sleep(time.Millisecond)
			// Deliberately returns neither Target nor Duration.
			return ItemResult{Disposition: Created}
		})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Target.Repo != "api" {
		t.Errorf("engine did not stamp Target, got %+v", results[0].Target)
	}
	if results[0].Duration <= 0 {
		t.Error("engine did not stamp Duration")
	}
}

// A per-item timeout must reach the operation, so one hung repo cannot block the
// whole fan-out forever.
func TestRun_TimeoutReachesTheOperation(t *testing.T) {
	results := Run(context.Background(), []Target{target("backend", "api", "main")},
		Config{Timeout: 10 * time.Millisecond},
		func(ctx context.Context, _ Target) ItemResult {
			select {
			case <-ctx.Done():
				return ItemResult{Disposition: Failed, Reason: "timed out", Err: ctx.Err()}
			case <-time.After(2 * time.Second):
				return ItemResult{Disposition: Created}
			}
		})

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Disposition != Failed {
		t.Errorf("disposition = %q, want failed", results[0].Disposition)
	}
	if !errors.Is(results[0].Err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", results[0].Err)
	}
}

type recordingReporter struct {
	mu      sync.Mutex
	started []string
	done    []string
}

func (r *recordingReporter) Start(t Target) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.started = append(r.started, t.Key())
}

func (r *recordingReporter) Finish(res ItemResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done = append(r.done, res.Target.Key())
}

// Every item must be reported exactly once at each end, so a live renderer can
// never leave a row spinning forever.
func TestRun_ReporterSeesEveryItemOnce(t *testing.T) {
	targets := []Target{
		target("backend", "api", "main"),
		target("backend", "api", "stage"),
		target("frontend", "web", "main"),
	}

	rep := &recordingReporter{}
	Run(context.Background(), targets, Config{Reporter: rep}, func(_ context.Context, t Target) ItemResult {
		if t.Repo == "web" {
			return ItemResult{Disposition: Failed, Reason: "boom", Err: errors.New("boom")}
		}
		return ItemResult{Disposition: Created}
	})

	if len(rep.started) != 3 || len(rep.done) != 3 {
		t.Fatalf("reporter saw %d starts and %d finishes, want 3 and 3 — including the failure",
			len(rep.started), len(rep.done))
	}
}

// An empty workspace is converged, not broken.
func TestRun_NoTargetsIsNotAnError(t *testing.T) {
	results := Run(context.Background(), nil, Config{}, ok)
	if results != nil {
		t.Errorf("got %v, want nil", results)
	}
	if _, _, _, exit := Summarize(results); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
}

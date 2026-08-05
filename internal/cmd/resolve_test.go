package cmd

import (
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// twoRepoEnv builds a workspace where both repos have a "main" worktree, which is
// the ordinary case that made first-match resolution dangerous.
func twoRepoEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("frontend", "web", "main")
	env.Chdir()
	// The resolver tests call into the package directly rather than through
	// rootCmd.Execute(), so loadProject never runs. Set what it would have set.
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	return env
}

// The bug this replaces: "main" exists in every repo, and resolution returned the
// first match, so hydra acted on an arbitrary repo and reported success.
func TestResolveOneWorktree_AmbiguousBranchIsRefused(t *testing.T) {
	resetCommandState(t)
	_ = twoRepoEnv(t)

	items, _ := collectWorktrees(cfg, projectRoot)
	_, err := resolveOneWorktree(items, "main")
	if err == nil {
		t.Fatal("a branch name shared by two repos must not resolve silently")
	}

	classified := output.Classify(err)
	if classified.Code != output.CodeWorktreeNameConflict {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeWorktreeNameConflict)
	}
	candidates, ok := classified.Details["candidates"].([]string)
	if !ok || len(candidates) != 2 {
		t.Fatalf("details.candidates must name both worktrees, got %#v", classified.Details["candidates"])
	}

}

// A group-qualified handle is unique by construction, so it must still resolve even
// when the bare branch name is ambiguous. Without this, the ambiguity fix would
// leave no way to name the worktree at all.
func TestResolveOneWorktree_QualifiedHandleDisambiguates(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	items, _ := collectWorktrees(cfg, projectRoot)
	wt, err := resolveOneWorktree(items, "backend/api")
	if err != nil {
		t.Fatalf("a qualified handle must resolve: %v", err)
	}
	if wt.RepoContext.Alias != "api" {
		t.Errorf("resolved %q, want api", wt.RepoContext.Alias)
	}
}

func TestResolveOneWorktree_UnknownHandle(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	items, _ := collectWorktrees(cfg, projectRoot)
	_, err := resolveOneWorktree(items, "nope")
	if code := output.Classify(err).Code; code != output.CodeWorktreeUnknown {
		t.Fatalf("code = %q, want %q", code, output.CodeWorktreeUnknown)
	}
}

// path is consumed as `cd "$(hydra path X)"`, so an ambiguous handle must fail
// rather than send the caller into an arbitrary worktree.
func TestPath_AmbiguousHandleIsRefused(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	resetCommandState(t)
	rootCmd.SetArgs([]string{"path", "main", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("path must refuse an ambiguous handle")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeNameConflict {
		t.Errorf("code = %q, want %q", code, output.CodeWorktreeNameConflict)
	}
}

func TestParseFilters(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		wantErr bool
		check   func(filters) bool
	}{
		{name: "dirty", in: []string{"dirty"}, check: func(f filters) bool { return f.dirty && f.derived() }},
		{name: "behind", in: []string{"behind"}, check: func(f filters) bool { return f.behind && f.derived() }},
		{
			name:  "branch glob is cheap",
			in:    []string{"branch:feat/*"},
			check: func(f filters) bool { return !f.derived() && f.matchesBranch("feat/login") },
		},
		{
			name: "several globs are a union",
			in:   []string{"branch:feat/*", "branch:fix/*"},
			check: func(f filters) bool {
				return f.matchesBranch("fix/x") && f.matchesBranch("feat/y") && !f.matchesBranch("chore/z")
			},
		},
		{name: "unknown value", in: []string{"nope"}, wantErr: true},
		{name: "empty glob", in: []string{"branch:"}, wantErr: true},
		{name: "malformed glob", in: []string{"branch:[a-"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseFilters(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseFilters(%v) must fail", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseFilters(%v): %v", tc.in, err)
			}
			if !tc.check(got) {
				t.Errorf("parseFilters(%v) = %+v, failed its check", tc.in, got)
			}
		})
	}
}

// No filters means no branch narrowing at all — not "matches nothing".
func TestFilters_NoBranchGlobMatchesEverything(t *testing.T) {
	var f filters
	if !f.matchesBranch("anything") {
		t.Error("an empty filter set must not narrow by branch")
	}
}

// A typo in --repos must say the repo is unknown, not return an empty list that
// reads as "nothing to do".
func TestResolveTargets_UnknownRepoIsRefused(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	_, _, _, err := resolveTargets(currentSession(), Selector{Repos: []string{"nope"}}, false)
	if err == nil {
		t.Fatal("an unknown repo must fail")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeRepoUnknown {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeRepoUnknown)
	}
	if known, ok := classified.Details["known"].([]string); !ok || len(known) != 2 {
		t.Errorf("details.known must list both repos, got %#v", classified.Details["known"])
	}
}

func TestResolveTargets_UnknownGroupIsRefused(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	_, _, _, err := resolveTargets(currentSession(), Selector{Group: "nope"}, false)
	if code := output.Classify(err).Code; code != output.CodeRepoUnknown {
		t.Fatalf("code = %q, want %q", code, output.CodeRepoUnknown)
	}
}

func TestResolveTargets_NarrowsByRepoAndGroup(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)
	session := currentSession()

	byRepo, _, _, err := resolveTargets(session, Selector{Repos: []string{"api"}}, false)
	if err != nil {
		t.Fatalf("--repos api: %v", err)
	}
	for _, entry := range byRepo {
		if entry.Context.RepoContext.Alias != "api" {
			t.Errorf("--repos api returned %q", entry.Context.RepoContext.Alias)
		}
	}
	if len(byRepo) == 0 {
		t.Error("--repos api must match something")
	}

	byGroup, _, _, err := resolveTargets(session, Selector{Group: "frontend"}, false)
	if err != nil {
		t.Fatalf("--group frontend: %v", err)
	}
	for _, entry := range byGroup {
		if entry.Context.RepoContext.Group != "frontend" {
			t.Errorf("--group frontend returned group %q", entry.Context.RepoContext.Group)
		}
	}
	if len(byGroup) == 0 {
		t.Error("--group frontend must match something")
	}
}

// A branch glob is a cheap filter, so it must narrow without the caller asking for
// tracking. This is the property that lets the expensive phase be skipped.
func TestResolveTargets_BranchFilterSkipsTracking(t *testing.T) {
	resetCommandState(t)
	env := twoRepoEnv(t)
	env.CreateWorktree("backend", "api", "feat/login", "api-login")

	resolved, _, _, err := resolveTargets(currentSession(), Selector{Filter: []string{"branch:feat/*"}}, false)
	if err != nil {
		t.Fatalf("--filter branch: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected exactly the feat/* worktree, got %d", len(resolved))
	}
	if resolved[0].Context.Branch != "feat/login" {
		t.Errorf("matched %q, want feat/login", resolved[0].Context.Branch)
	}
	// Tracking was not requested and no derived filter was used, so upstream must
	// still be unset — proving the expensive phase was skipped.
	if resolved[0].Item.Upstream != nil {
		t.Error("tracking ran despite no caller or filter needing it")
	}
}

// A derived filter must force the tracking phase even when the caller did not ask,
// since dirty is computed there.
func TestResolveTargets_DerivedFilterForcesTracking(t *testing.T) {
	resetCommandState(t)
	env := twoRepoEnv(t)
	wt := env.CreateWorktree("backend", "api", "feat/dirty", "api-dirty")
	env.MakeWorktreeDirty(wt)

	resolved, _, _, err := resolveTargets(currentSession(), Selector{Filter: []string{"dirty"}}, false)
	if err != nil {
		t.Fatalf("--filter dirty: %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("expected only the dirty worktree, got %d", len(resolved))
	}
	if resolved[0].Context.Branch != "feat/dirty" {
		t.Errorf("matched %q, want feat/dirty", resolved[0].Context.Branch)
	}
	if !resolved[0].Item.Dirty {
		t.Error("a --filter dirty result must be marked dirty")
	}
}

// An absent topic in THIS project must mean "nothing matches here", never an error:
// under --all the topic may live in a sibling project, and failing would abort the
// whole walk.
func TestResolveTargets_AbsentTopicMatchesNothing(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	resolved, _, _, err := resolveTargets(currentSession(), Selector{Topic: "not-here"}, false)
	if err != nil {
		t.Fatalf("an absent topic must not fail inside the resolver: %v", err)
	}
	if len(resolved) != 0 {
		t.Errorf("expected no matches, got %d", len(resolved))
	}
}

// path --topic must print one path or fail. A topic spanning repos is exactly the
// normal case, so this is the guard that keeps `cd "$(hydra path --topic X)"` honest.
func TestPath_TopicSpanningRepositoriesIsRefused(t *testing.T) {
	resetCommandState(t)
	env := twoRepoEnv(t)
	env.CreateWorktree("backend", "api", "feat/login", "api-login")
	env.CreateWorktree("frontend", "web", "feat/login", "web-login")

	store := topic.Open(env.RootDir)
	for _, m := range []topic.Member{{Repo: "api", Branch: "feat/login"}, {Repo: "web", Branch: "feat/login"}} {
		if err := store.Attach("2072958", m); err != nil {
			t.Fatalf("attach %s: %v", m.Repo, err)
		}
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"path", "--topic", "2072958", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a topic spanning two worktrees has no single path")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeWorktreeNameConflict {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeWorktreeNameConflict)
	}
	if candidates, ok := classified.Details["candidates"].([]string); !ok || len(candidates) != 2 {
		t.Errorf("details.candidates must name both, got %#v", classified.Details["candidates"])
	}
}

func TestPath_TopicWithOneWorktreeResolves(t *testing.T) {
	resetCommandState(t)
	env := twoRepoEnv(t)
	created := env.CreateWorktree("backend", "api", "feat/solo", "api-solo")
	if err := topic.Open(env.RootDir).Attach("solo", topic.Member{Repo: "api", Branch: "feat/solo"}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"path", "--topic", "solo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("path --topic solo: %v", err)
	}
	if got := strings.TrimSpace(stdout.String()); got != created {
		t.Errorf("path = %q, want %q", got, created)
	}
}

// Passing both a handle and --topic asks two different questions at once.
func TestPath_HandleAndTopicTogetherIsRefused(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	resetCommandState(t)
	rootCmd.SetArgs([]string{"path", "backend/api", "--topic", "x", "--output", "json"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("a handle and --topic together must be refused")
	}
}

// A selector that matches nothing is a legitimate answer — "nothing is dirty" is true —
// so it stays exit 0. But an empty result is indistinguishable from a typo'd glob, and a
// caller reading success with an empty list concludes the work does not exist rather than
// that its selector was wrong. Saying how many candidates were considered separates them.
func TestResolve_EmptySelectionExplainsItself(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	_, warnings, _, err := resolveTargets(currentSession(),
		Selector{Filter: []string{"branch:no-such-*"}}, false)
	if err != nil {
		t.Fatalf("an empty match must not be an error: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "matched none") {
		t.Fatalf("warnings = %q, want one naming the unmatched selector", warnings)
	}
	if !strings.Contains(warnings[0], "branch:no-such-*") {
		t.Errorf("the warning must name the selector, got %q", warnings[0])
	}
}

// Without a selector there is nothing to explain, so a plain listing stays silent.
func TestResolve_NoSelectorProducesNoWarning(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	resolved, warnings, _, err := resolveTargets(currentSession(), Selector{}, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(resolved) == 0 {
		t.Fatal("fixture should have worktrees")
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %q, want none", warnings)
	}
}

// Repository failures are counted explicitly. They used to be inferred as
// len(repos) - len(warnings), so any advisory warning was miscounted as a repository
// failure — which made a healthy listing report partial_failure.
func TestResolve_AdvisoryWarningsAreNotRepoFailures(t *testing.T) {
	resetCommandState(t)
	twoRepoEnv(t)

	_, warnings, repoFailures, err := resolveTargets(currentSession(),
		Selector{Filter: []string{"branch:no-such-*"}}, false)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected an advisory warning to be present")
	}
	if repoFailures != 0 {
		t.Errorf("repoFailures = %d, want 0: an advisory warning is not a failed repository",
			repoFailures)
	}
}

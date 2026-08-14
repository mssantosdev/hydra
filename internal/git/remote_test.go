package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFetchRemoteBranchesFromLocalOrigin(t *testing.T) {
	upstream := newUpstream(t)
	branches, err := FetchRemoteBranches(fileURL(upstream))
	if err != nil {
		t.Fatalf("FetchRemoteBranches: %v", err)
	}
	names := branchNames(branches)
	if len(names) != 2 {
		t.Fatalf("branch count = %d, want 2: %v", len(names), names)
	}
	for _, want := range []string{"main", "stage"} {
		if !contains(names, want) {
			t.Fatalf("missing branch %q in %v", want, names)
		}
	}
}

func TestGetDefaultBranchPrefersMainOverMaster(t *testing.T) {
	if got := GetDefaultBranch([]RemoteBranch{{Name: "master"}, {Name: "main"}}); got != "main" {
		t.Fatalf("GetDefaultBranch = %q, want main", got)
	}
	if got := GetDefaultBranch([]RemoteBranch{{Name: "develop"}, {Name: "master"}}); got != "master" {
		t.Fatalf("GetDefaultBranch = %q, want master", got)
	}
	if got := GetDefaultBranch([]RemoteBranch{{Name: "release"}}); got != "release" {
		t.Fatalf("GetDefaultBranch = %q, want release", got)
	}
	if got := GetDefaultBranch(nil); got != "main" {
		t.Fatalf("GetDefaultBranch(empty) = %q, want main", got)
	}
}

func TestFilterBranchesReturnsOnlyDefaults(t *testing.T) {
	branches := []RemoteBranch{
		{Name: "main", IsDefault: true},
		{Name: "stage", IsDefault: false},
		{Name: "master", IsDefault: true},
	}
	filtered := FilterBranches(branches, true)
	if len(filtered) != 2 {
		t.Fatalf("filtered count = %d, want 2", len(filtered))
	}
	if len(FilterBranches(branches, false)) != 0 {
		t.Fatal("includeDefaults=false should return nothing")
	}
}

func TestGetRemoteBranchesFromBareFetchesLatest(t *testing.T) {
	upstream := newUpstream(t)
	_, bare := newBareWithRemote(t, upstream)

	runGitTest(t, upstream, "commit", "--allow-empty", "-m", "new on origin")

	branches, err := GetRemoteBranchesFromBare(bare)
	if err != nil {
		t.Fatalf("GetRemoteBranchesFromBare: %v", err)
	}
	if len(branches) < 2 {
		t.Fatalf("expected remote branches, got %v", branchNames(branches))
	}
}

func TestGetRemoteDefaultBranchUnsetReturnsError(t *testing.T) {
	bare := filepath.Join(resolveTempDir(t, t.TempDir()), "bare.git")
	if err := runGit("init", "--bare", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if _, err := GetRemoteDefaultBranch(bare); err == nil {
		t.Fatal("expected error when origin/HEAD is unset")
	}
}

func TestCountAgainstUnknownRefReturnsErrRefUnknown(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	_, _, err := CountAgainst(worktree, "not-a-real-ref")
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
	if !errors.Is(err, ErrRefUnknown) {
		t.Fatalf("error = %v, want ErrRefUnknown", err)
	}

	_, _, err = CountAgainst(worktree, "   ")
	if err == nil || !errors.Is(err, ErrRefUnknown) {
		t.Fatalf("empty ref error = %v, want ErrRefUnknown", err)
	}
}

func TestCountAgainstValidRefReportsAheadBehind(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	runGitTest(t, worktree, "commit", "--allow-empty", "-m", "local only")

	ahead, behind, err := CountAgainst(worktree, "origin/main")
	if err != nil {
		t.Fatalf("CountAgainst: %v", err)
	}
	if ahead != 1 || behind != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 1/0", ahead, behind)
	}
}

func TestListRemoteRefsBrokenRepoIsNotErrRefUnknown(t *testing.T) {
	_, err := ListRemoteBranchesCached(filepath.Join(t.TempDir(), "missing.git"))
	if err == nil {
		t.Fatal("expected error for missing bare repo")
	}
	if errors.Is(err, ErrRefUnknown) {
		t.Fatalf("broken repo should not be reported as unknown ref: %v", err)
	}
}

func TestCheckWorktreeStatusReportsDirtyAndTracking(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	if err := os.WriteFile(filepath.Join(worktree, "dirty.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	status, err := CheckWorktreeStatus(worktree)
	if err != nil {
		t.Fatalf("CheckWorktreeStatus: %v", err)
	}
	if !status.HasChanges || status.ChangeCount == 0 {
		t.Fatalf("expected dirty status, got %+v", status)
	}
	if status.Branch != "main" || status.Upstream != "origin/main" {
		t.Fatalf("tracking = %+v, want main tracking origin/main", status)
	}
	if status.IsClean {
		t.Fatal("dirty worktree must not be clean")
	}
}

func TestStashChangesPopAndHasStash(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	path := filepath.Join(worktree, "tracked.txt")
	if err := os.WriteFile(path, []byte("before"), 0644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	runGitTest(t, worktree, "add", "tracked.txt")
	runGitTest(t, worktree, "commit", "-m", "track")

	if err := os.WriteFile(path, []byte("after"), 0644); err != nil {
		t.Fatalf("modify tracked file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("u"), 0644); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	if err := StashChanges(worktree); err != nil {
		t.Fatalf("StashChanges: %v", err)
	}
	has, err := HasStash(worktree)
	if err != nil || !has {
		t.Fatalf("HasStash = (%v, %v), want (true, nil)", has, err)
	}
	if err := PopStash(worktree); err != nil {
		t.Fatalf("PopStash: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "after" {
		t.Fatalf("restored content = (%q, %v), want after", data, err)
	}
}

func TestResetHardDiscardsChanges(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	path := filepath.Join(worktree, "reset-me.txt")
	if err := os.WriteFile(path, []byte("v1"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitTest(t, worktree, "add", "reset-me.txt")
	runGitTest(t, worktree, "commit", "-m", "track")

	if err := os.WriteFile(path, []byte("dirty"), 0644); err != nil {
		t.Fatalf("dirty file: %v", err)
	}
	if err := ResetHard(worktree); err != nil {
		t.Fatalf("ResetHard: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "v1" {
		t.Fatalf("after reset content = (%q, %v), want v1", data, err)
	}
}

func branchNames(branches []RemoteBranch) []string {
	names := make([]string, len(branches))
	for i, b := range branches {
		names[i] = b.Name
	}
	return names
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func TestFetchRemoteBranchesSkipsMalformedLines(t *testing.T) {
	upstream := newUpstream(t)
	branches, err := FetchRemoteBranches(fileURL(upstream))
	if err != nil {
		t.Fatalf("FetchRemoteBranches: %v", err)
	}
	for _, b := range branches {
		if b.Name == "" || !b.IsRemote {
			t.Fatalf("unexpected branch entry: %+v", b)
		}
	}
}

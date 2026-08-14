package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairBareRemoteConvergesInterruptedClone(t *testing.T) {
	upstream := newUpstream(t)
	root := resolveTempDir(t, t.TempDir())
	bare := filepath.Join(root, "api.git")

	if err := runGit("init", "--bare", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if err := RepairBareRemote(bare, upstream); err != nil {
		t.Fatalf("RepairBareRemote: %v", err)
	}
	if !HasRemote(bare) {
		t.Fatal("repair should configure origin")
	}
	if got := GetConfig(bare, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("fetch refspec = %q", got)
	}
	if !HasOriginHead(bare) {
		t.Fatal("repair should resolve origin/HEAD")
	}
	remotes, err := ListRemoteBranchesCached(bare)
	if err != nil || len(remotes) < 2 {
		t.Fatalf("remote branches after repair = %v, %v", remotes, err)
	}
}

func TestSetOriginHeadAndFetchRefspec(t *testing.T) {
	upstream := newUpstream(t)
	_, bare := newBareWithRemote(t, upstream)

	if err := runGit("--git-dir="+bare, "symbolic-ref", "-d", "refs/remotes/origin/HEAD"); err != nil {
		t.Fatalf("delete origin/HEAD: %v", err)
	}
	if HasOriginHead(bare) {
		t.Fatal("origin/HEAD should be unset")
	}
	if err := SetOriginHead(bare); err != nil {
		t.Fatalf("SetOriginHead: %v", err)
	}
	if branch, err := GetRemoteDefaultBranch(bare); err != nil || branch != "main" {
		t.Fatalf("GetRemoteDefaultBranch = (%q, %v)", branch, err)
	}

	if err := runGit("--git-dir="+bare, "config", "--unset", "remote.origin.fetch"); err != nil {
		t.Fatalf("unset fetch refspec: %v", err)
	}
	if err := SetFetchRefspec(bare); err != nil {
		t.Fatalf("SetFetchRefspec: %v", err)
	}
	if got := GetConfig(bare, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("fetch refspec = %q", got)
	}
}

func TestBranchExistsAndResolveBaseRef(t *testing.T) {
	upstream := newUpstream(t)
	_, bare := newBareWithRemote(t, upstream)

	exists, err := BranchExists(bare, "main")
	if err != nil || !exists {
		t.Fatalf("BranchExists(main) = (%v, %v)", exists, err)
	}
	exists, err = BranchExists(bare, "missing")
	if err != nil || exists {
		t.Fatalf("BranchExists(missing) = (%v, %v)", exists, err)
	}

	ref, err := ResolveBaseRef(bare, "main")
	if err != nil || ref != "origin/main" {
		t.Fatalf("ResolveBaseRef(main) = (%q, %v)", ref, err)
	}
	if _, err := ResolveBaseRef(bare, "missing"); err == nil {
		t.Fatal("expected error for missing branch")
	}
}

func TestAddWorktreeExistingLocalAndTrackingLocalBranch(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)

	localBranch := filepath.Join(root, "seed")
	if err := AddWorktreeNewBranch(bare, localBranch, "local-only", "origin/main"); err != nil {
		t.Fatalf("AddWorktreeNewBranch: %v", err)
	}
	if err := RemoveWorktree(bare, localBranch, false); err != nil {
		t.Fatalf("RemoveWorktree seed: %v", err)
	}

	attach := filepath.Join(root, "attach")
	if err := AddWorktreeExistingLocal(bare, attach, "local-only"); err != nil {
		t.Fatalf("AddWorktreeExistingLocal: %v", err)
	}
	tracking, err := WorktreeTracking(attach)
	if err != nil || tracking.Upstream != "" {
		t.Fatalf("local-only upstream = (%q, %v)", tracking.Upstream, err)
	}
	if err := RemoveWorktree(bare, attach, false); err != nil {
		t.Fatalf("RemoveWorktree attach: %v", err)
	}

	tracked := filepath.Join(root, "tracked")
	if err := AddWorktreeTracking(bare, tracked, "stage"); err != nil {
		t.Fatalf("AddWorktreeTracking new: %v", err)
	}
	if err := RemoveWorktree(bare, tracked, false); err != nil {
		t.Fatalf("RemoveWorktree tracked: %v", err)
	}

	reuse := filepath.Join(root, "reuse")
	if err := AddWorktreeTracking(bare, reuse, "stage"); err != nil {
		t.Fatalf("AddWorktreeTracking existing local branch: %v", err)
	}
	tracking, err = WorktreeTracking(reuse)
	if err != nil || tracking.Upstream != "origin/stage" {
		t.Fatalf("reused branch upstream = (%q, %v)", tracking.Upstream, err)
	}
}

func TestDeleteBranchRefusesUnmergedWithoutForce(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	runGitTest(t, worktree, "checkout", "-b", "orphan")
	runGitTest(t, worktree, "commit", "--allow-empty", "-m", "orphan commit")
	if err := runGit("-C", worktree, "checkout", "main"); err != nil {
		t.Fatalf("checkout main: %v", err)
	}
	if err := RemoveWorktree(bare, worktree, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if err := DeleteBranch(bare, "orphan", false); err == nil {
		t.Fatal("DeleteBranch should refuse unmerged branch without force")
	}
	if RefExists(bare, "refs/heads/orphan") {
		if err := DeleteBranch(bare, "orphan", true); err != nil {
			t.Fatalf("DeleteBranch force: %v", err)
		}
	}
	if RefExists(bare, "refs/heads/orphan") {
		t.Fatal("orphan branch should be deleted after force")
	}
}

func TestHasUncommittedChangesAndIsBranchMerged(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	dirty, count, err := HasUncommittedChanges(worktree)
	if err != nil || dirty || count != 0 {
		t.Fatalf("clean tree = (%v, %d, %v)", dirty, count, err)
	}

	if err := os.WriteFile(filepath.Join(worktree, "new.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	dirty, count, err = HasUncommittedChanges(worktree)
	if err != nil || !dirty || count == 0 {
		t.Fatalf("dirty tree = (%v, %d, %v)", dirty, count, err)
	}

	runGitTest(t, worktree, "checkout", "-b", "merged")
	runGitTest(t, worktree, "commit", "--allow-empty", "-m", "on merged")
	runGitTest(t, worktree, "checkout", "main")
	runGitTest(t, worktree, "merge", "merged")

	if !IsBranchMerged(bare, "merged", "main") {
		t.Fatal("merged branch should be ancestor of main")
	}
	if IsBranchMerged(bare, "stage", "main") {
		t.Fatal("unrelated branch should not be merged into main")
	}
}

func TestGetRemoteURL(t *testing.T) {
	upstream := newUpstream(t)
	_, bare := newBareWithRemote(t, upstream)

	url, err := GetRemoteURL(bare)
	if err != nil {
		t.Fatalf("GetRemoteURL: %v", err)
	}
	if !strings.Contains(url, "upstream") {
		t.Fatalf("origin URL = %q, want path containing upstream", url)
	}
}

func TestClassifyBranchRequiresInput(t *testing.T) {
	if _, err := ClassifyBranch("", "main"); err == nil {
		t.Fatal("expected error for empty bare path")
	}
	if _, err := ClassifyBranch("/tmp/x.git", ""); err == nil {
		t.Fatal("expected error for empty branch")
	}
}

func TestWorktreeTrackingDetachedReturnsEmptyBranch(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := runGit("-C", worktree, "checkout", "--detach"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	tracking, err := WorktreeTracking(worktree)
	if err != nil {
		t.Fatalf("WorktreeTracking: %v", err)
	}
	if tracking.Branch != "" || tracking.Upstream != "" {
		t.Fatalf("detached tracking = %+v, want empty branch/upstream", tracking)
	}
}

func TestSetUpstream(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api-stage")
	if err := AddWorktreeTracking(bare, worktree, "stage"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := runGit("-C", worktree, "branch", "--unset-upstream"); err != nil {
		t.Fatalf("unset upstream: %v", err)
	}
	if err := SetUpstream(worktree, "stage"); err != nil {
		t.Fatalf("SetUpstream: %v", err)
	}
	tracking, err := WorktreeTracking(worktree)
	if err != nil || tracking.Upstream != "origin/stage" {
		t.Fatalf("upstream after SetUpstream = (%q, %v)", tracking.Upstream, err)
	}
}

func TestRepairBareRemoteUpdatesExistingOriginURL(t *testing.T) {
	upstream := newUpstream(t)
	root := resolveTempDir(t, t.TempDir())
	other := filepath.Join(root, "second-origin")
	if err := os.MkdirAll(other, 0755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	runGitTest(t, other, "init", "-b", "main")
	runGitTest(t, other, "commit", "--allow-empty", "-m", "init")

	bare := filepath.Join(root, "api.git")
	if err := runGit("init", "--bare", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	if err := runGit("--git-dir="+bare, "remote", "add", "origin", upstream); err != nil {
		t.Fatalf("add origin: %v", err)
	}
	if err := RepairBareRemote(bare, other); err != nil {
		t.Fatalf("RepairBareRemote: %v", err)
	}
	if got := GetConfig(bare, "remote.origin.url"); got != other {
		t.Fatalf("origin URL = %q, want %q after repair", got, other)
	}
}

func TestResolveBaseRefPrefersLocalBranch(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	local := filepath.Join(root, "local-only")
	if err := AddWorktreeNewBranch(bare, local, "local-only", "origin/main"); err != nil {
		t.Fatalf("AddWorktreeNewBranch: %v", err)
	}
	ref, err := ResolveBaseRef(bare, "local-only")
	if err != nil || ref != "local-only" {
		t.Fatalf("ResolveBaseRef(local-only) = (%q, %v)", ref, err)
	}
}

func TestHasStashFalseWhenEmpty(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	has, err := HasStash(worktree)
	if err != nil || has {
		t.Fatalf("HasStash on clean worktree = (%v, %v), want (false, nil)", has, err)
	}
}

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchNameFromRef(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "local branch with slash", ref: "refs/heads/marcus/feat-onboarding", want: "marcus/feat-onboarding"},
		{name: "local branch simple", ref: "refs/heads/stage", want: "stage"},
		{name: "remote branch with slash", ref: "refs/remotes/origin/feature/test", want: "feature/test"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := branchNameFromRef(tt.ref); got != tt.want {
				t.Fatalf("branchNameFromRef(%q) = %q, want %q", tt.ref, got, tt.want)
			}
		})
	}
}

// newUpstream builds a real repository to act as origin, with an initial commit
// on `main` plus a `stage` branch.
func newUpstream(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	upstream := filepath.Join(dir, "upstream")
	if err := os.MkdirAll(upstream, 0755); err != nil {
		t.Fatalf("mkdir upstream: %v", err)
	}

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", upstream}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	run("init", "-b", "main")
	run("commit", "--allow-empty", "-m", "init")
	run("branch", "stage", "main")
	return upstream
}

// TestInitBareWithRemoteConfiguresTracking is the regression guard for the
// original bug: `git clone --bare` wrote no fetch refspec and no
// refs/remotes/origin/*, so no worktree could ever get an upstream.
func TestInitBareWithRemoteConfiguresTracking(t *testing.T) {
	upstream := newUpstream(t)
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	bare := filepath.Join(root, ".bare", "api.git")

	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}

	// refs/heads/* must start EMPTY: --bare would have copied upstream heads
	// straight into it, which is what shadowed origin/<branch> forever.
	locals, err := ListLocalBranches(bare)
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	if len(locals) != 0 {
		t.Fatalf("expected no local branches after InitBareWithRemote, got %v", locals)
	}

	if got := GetConfig(bare, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("remote.origin.fetch = %q, want the standard refspec", got)
	}
	if !HasOriginHead(bare) {
		t.Fatal("refs/remotes/origin/HEAD is not set")
	}
	if branch, err := GetRemoteDefaultBranch(bare); err != nil || branch != "main" {
		t.Fatalf("GetRemoteDefaultBranch() = (%q, %v), want (main, nil)", branch, err)
	}

	remotes, err := ListRemoteBranchesCached(bare)
	if err != nil {
		t.Fatalf("ListRemoteBranchesCached: %v", err)
	}
	names := make([]string, 0, len(remotes))
	for _, r := range remotes {
		names = append(names, r.Name)
	}
	if len(names) != 2 {
		t.Fatalf("expected main and stage on origin, got %v", names)
	}

	// A worktree for a remote branch must come out WITH upstream configured.
	worktree := filepath.Join(root, "backend", "api-stage")
	if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
		t.Fatalf("mkdir group: %v", err)
	}
	if err := AddWorktreeTracking(bare, worktree, "stage"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	tracking, err := WorktreeTracking(worktree)
	if err != nil {
		t.Fatalf("WorktreeTracking: %v", err)
	}
	if tracking.Branch != "stage" {
		t.Fatalf("branch = %q, want stage", tracking.Branch)
	}
	if tracking.Upstream != "origin/stage" {
		t.Fatalf("upstream = %q, want origin/stage", tracking.Upstream)
	}
	if tracking.Ahead != 0 || tracking.Behind != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 0/0", tracking.Ahead, tracking.Behind)
	}

	// The worktree is a real directory OUTSIDE the git dir.
	if info, err := os.Lstat(worktree); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("worktree must be a real directory: err=%v", err)
	}
	if strings.HasPrefix(worktree, bare) {
		t.Fatal("worktree path must not be inside the bare repository")
	}
}

func TestClassifyBranch(t *testing.T) {
	upstream := newUpstream(t)
	bare := filepath.Join(t.TempDir(), "api.git")
	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}

	if kind, err := ClassifyBranch(bare, "main"); err != nil || kind != BranchRemote {
		t.Fatalf("ClassifyBranch(main) = (%v, %v), want (remote, nil)", kind, err)
	}
	if kind, err := ClassifyBranch(bare, "nope"); err != nil || kind != BranchNone {
		t.Fatalf("ClassifyBranch(nope) = (%v, %v), want (none, nil)", kind, err)
	}

	worktree := filepath.Join(t.TempDir(), "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if kind, err := ClassifyBranch(bare, "main"); err != nil || kind != BranchBoth {
		t.Fatalf("ClassifyBranch(main) after checkout = (%v, %v), want (both, nil)", kind, err)
	}

	// A brand-new branch has no upstream, and that is reported as local-only
	// rather than as an error.
	local := filepath.Join(t.TempDir(), "api-feat")
	if err := AddWorktreeNewBranch(bare, local, "feat/x", "origin/main"); err != nil {
		t.Fatalf("AddWorktreeNewBranch: %v", err)
	}
	if kind, err := ClassifyBranch(bare, "feat/x"); err != nil || kind != BranchLocal {
		t.Fatalf("ClassifyBranch(feat/x) = (%v, %v), want (local, nil)", kind, err)
	}
	tracking, err := WorktreeTracking(local)
	if err != nil {
		t.Fatalf("WorktreeTracking: %v", err)
	}
	if tracking.Upstream != "" {
		t.Fatalf("upstream = %q, want empty for a new local branch", tracking.Upstream)
	}
}

// TestWorktreeTrackingCountsBehind proves sync is no longer inert: a real commit
// on origin must show up as Behind == 1.
func TestWorktreeTrackingCountsBehind(t *testing.T) {
	upstream := newUpstream(t)
	root := t.TempDir()
	bare := filepath.Join(root, "api.git")
	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	cmd := exec.Command("git", "-C", upstream, "commit", "--allow-empty", "-m", "ahead")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("commit upstream: %v\n%s", err, out)
	}

	if err := FetchBareRepo(bare); err != nil {
		t.Fatalf("FetchBareRepo: %v", err)
	}

	tracking, err := WorktreeTracking(worktree)
	if err != nil {
		t.Fatalf("WorktreeTracking: %v", err)
	}
	if tracking.Behind != 1 || tracking.Ahead != 0 {
		t.Fatalf("ahead/behind = %d/%d, want 0/1", tracking.Ahead, tracking.Behind)
	}

	if err := PullWorktree(worktree); err != nil {
		t.Fatalf("PullWorktree: %v", err)
	}
	tracking, err = WorktreeTracking(worktree)
	if err != nil {
		t.Fatalf("WorktreeTracking after pull: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("behind = %d after pull, want 0", tracking.Behind)
	}
}

func TestListWorktreesReportsDetached(t *testing.T) {
	upstream := newUpstream(t)
	root := t.TempDir()
	bare := filepath.Join(root, "api.git")
	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := runGit("-C", worktree, "checkout", "--detach"); err != nil {
		t.Fatalf("detach: %v", err)
	}

	worktrees, err := ListWorktrees(bare)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}

	var found bool
	for _, wt := range worktrees {
		if wt.IsBare {
			continue
		}
		found = true
		if !wt.Detached {
			t.Errorf("worktree %s should report Detached", wt.Path)
		}
		if wt.Branch != "" {
			t.Errorf("detached worktree branch = %q, want empty", wt.Branch)
		}
		if wt.Head == "" {
			t.Errorf("worktree %s has no HEAD sha", wt.Path)
		}
	}
	if !found {
		t.Fatal("no non-bare worktree reported")
	}
}

func TestDeleteBranchActuallyDeletes(t *testing.T) {
	upstream := newUpstream(t)
	root := t.TempDir()
	bare := filepath.Join(root, "api.git")
	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}
	worktree := filepath.Join(root, "api-stage")
	if err := AddWorktreeTracking(bare, worktree, "stage"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := RemoveWorktree(bare, worktree, false); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}

	if !RefExists(bare, "refs/heads/stage") {
		t.Fatal("branch stage should still exist before deletion")
	}
	if err := DeleteBranch(bare, "stage", false); err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if RefExists(bare, "refs/heads/stage") {
		t.Fatal("branch stage must be gone after DeleteBranch")
	}
}

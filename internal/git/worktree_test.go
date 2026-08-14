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

// A streaming git failure must carry git's own diagnosis. Attaching stderr to the
// terminal is not reporting it: a caller reading JSON would otherwise get only
// "exit status 128" while the human watching the terminal saw the reason.
func TestStreamingFailureCarriesGitsDiagnosis(t *testing.T) {
	err := FetchBareRepo(filepath.Join(t.TempDir(), "absent.git"))
	if err == nil {
		t.Fatal("fetch of an absent bare repo should fail")
	}
	if strings.Contains(err.Error(), "exit status") {
		t.Errorf("error reports only an exit status, losing git's reason: %v", err)
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error does not carry git's diagnosis: %v", err)
	}
}

func TestStderrTailKeepsTheDiagnosisNotTheProgress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// git writes progress AFTER the fatal on some paths; the fatal is the
			// answer either way.
			name: "fatal wins over later progress",
			in:   "Receiving objects:  50%\rfatal: could not read Username\rReceiving objects: 100%\n",
			want: "fatal: could not read Username",
		},
		{
			name: "falls back to the last non-empty line",
			in:   "warning: something\n\n  remote hung up  \n\n",
			want: "remote hung up",
		},
		{
			name: "empty stays empty so the caller keeps the exit error",
			in:   "\r\n  \r\n",
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tail := &stderrTail{limit: 4096}
			if _, err := tail.Write([]byte(tt.in)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if got := tail.diagnosis(); got != tt.want {
				t.Errorf("diagnosis() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Progress output is unbounded; the buffer must not be.
func TestStderrTailIsBounded(t *testing.T) {
	tail := &stderrTail{limit: 64}
	for i := 0; i < 200; i++ {
		if _, err := tail.Write([]byte("0123456789")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if len(tail.buf) > 64 {
		t.Errorf("buffer grew to %d bytes, limit is 64", len(tail.buf))
	}
}

// git translates its diagnostics and hydra puts that text in a machine-readable
// envelope, so the locale must not come from the operator's environment.
func TestGitCommandPinsTheLocale(t *testing.T) {
	t.Setenv("LANG", "pt_BR.UTF-8")
	t.Setenv("LC_ALL", "pt_BR.UTF-8")
	cmd := gitCmd("--version")
	var lcAll, lang string
	for _, kv := range cmd.Env {
		switch {
		case strings.HasPrefix(kv, "LC_ALL="):
			lcAll = kv
		case strings.HasPrefix(kv, "LANG="):
			lang = kv
		}
	}
	if lcAll != "LC_ALL=C" {
		t.Errorf("LC_ALL not pinned, last value is %q", lcAll)
	}
	if lang != "LANG=C" {
		t.Errorf("LANG not pinned, last value is %q", lang)
	}
}

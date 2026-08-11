package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestClone_LocalRemoteSiblingLayout(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("api-origin", "main", "develop")
	env.Chdir()

	rootCmd.SetArgs([]string{
		"repo", "add", remote,
		"--as", "api",
		"--group", "tools",
		"--branches", "main",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("clone failed: %v", err)
	}

	barePath := env.GetBarePath("api")
	if !env.DirExists(barePath) {
		t.Fatalf("expected bare repo at %s", barePath)
	}

	worktreePath := env.GetWorktreePath("tools", "api")
	info, err := os.Stat(worktreePath)
	if err != nil {
		t.Fatalf("expected worktree at %s: %v", worktreePath, err)
	}
	if !info.IsDir() {
		t.Fatalf("worktree must be a real directory")
	}
	if env.IsSymlink(worktreePath) {
		t.Fatal("worktree must not be a symlink")
	}
	if strings.Contains(worktreePath, ".bare") {
		t.Fatal("worktree must not live under .bare")
	}
	if upstream := env.Upstream(worktreePath); upstream != "origin/main" {
		t.Fatalf("expected origin/main upstream, got %q", upstream)
	}
}

// TestClone_ResumesInterruptedClone converges when a bare repo exists on disk but is not
// registered: re-running repo add completes fetch, refspec, origin/HEAD, and registration.
func TestClone_ResumesInterruptedClone(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.Chdir()

	// Simulate the interruption: a bare repo exists with neither the fetch refspec
	// nor origin/HEAD, and the config knows nothing about it.
	barePath := env.GetBarePath("api")
	if err := os.MkdirAll(filepath.Dir(barePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := exec.Command("git", "init", "--bare", barePath).Run(); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}
	if git.GetConfig(barePath, "remote.origin.fetch") != "" {
		t.Fatal("fixture should start with no fetch refspec")
	}
	if _, ok := env.LoadConfig().FindRepo("api"); ok {
		t.Fatal("fixture should start unregistered")
	}

	rootCmd.SetArgs([]string{
		"repo", "add", remote,
		"--as", "api", "--group", "backend", "--branches", "main",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("clone must resume an interrupted clone, got: %v", err)
	}

	// The half-built repo is now complete rather than orphaned or re-cloned.
	if got := git.GetConfig(barePath, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Errorf("fetch refspec = %q, want the standard refspec", got)
	}
	if !git.HasOriginHead(barePath) {
		t.Error("origin/HEAD must be set after resuming")
	}
	ref, ok := env.LoadConfig().FindRepo("api")
	if !ok {
		t.Fatal("api must be registered after resuming")
	}
	if ref.Group != "backend" || ref.Repo.Remote != remote || ref.Repo.DefaultBranch != "main" {
		t.Errorf("registration = %+v, want group backend, the remote, default_branch main", ref)
	}
	worktree := env.GetWorktreePath("backend", "api")
	if !env.DirExists(worktree) {
		t.Fatalf("expected worktree at %s", worktree)
	}
	if upstream := env.Upstream(worktree); upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", upstream)
	}
}

// TestClone_RefusesConflictingRemote ensures repo add does not repoint a registered alias
// at a different remote URL.
func TestClone_RefusesConflictingRemote(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	first := env.CreateRemoteRepo("api-origin", "main")
	second := env.CreateRemoteRepo("other-origin", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"repo", "add", first, "--as", "api", "--group", "backend", "--branches", "main"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first clone: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"repo", "add", second, "--as", "api", "--group", "backend", "--branches", "main"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("cloning a different remote over a registered alias must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeExists {
		t.Errorf("error code = %q, want %q", code, output.CodeWorktreeExists)
	}
	if ref, _ := env.LoadConfig().FindRepo("api"); ref.Repo.Remote != first {
		t.Errorf("remote was repointed to %q; it must stay %q", ref.Repo.Remote, first)
	}
}

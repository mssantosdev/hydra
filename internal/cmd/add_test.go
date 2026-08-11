package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// TestAdd_TracksRemoteBranch ensures add sets upstream tracking when the branch already
// exists on origin.
func TestAdd_TracksRemoteBranch(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	worktree := env.GetWorktreePath("backend", "api-stage")
	if !env.DirExists(worktree) {
		t.Fatalf("expected worktree at %s", worktree)
	}
	if env.IsSymlink(worktree) {
		t.Error("worktree must be a real directory, never a symlink")
	}
	if upstream := env.Upstream(worktree); upstream != "origin/stage" {
		t.Errorf("upstream = %q, want origin/stage", upstream)
	}
}

// TestAdd_NewBranchIsLocalOnly proves a brand-new branch reports no upstream
// rather than inheriting its base branch's, which would make ahead/behind lie.
func TestAdd_NewBranchIsLocalOnly(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "feat/login"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	worktree := env.GetWorktreePath("backend", "api-feat-login")
	if !env.DirExists(worktree) {
		t.Fatalf("expected worktree at %s (slug folds / to -)", worktree)
	}
	if upstream := env.Upstream(worktree); upstream != "" {
		t.Errorf("upstream = %q, want empty for a branch that was never pushed", upstream)
	}
}

// TestAdd_AsOverridesDirectoryName covers the real motivation for --as: a long
// branch name that is not mechanically reducible to a short directory name.
func TestAdd_AsOverridesDirectoryName(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("web", "gileadeweb", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "gileadeweb", "marcus/feat-2072958-excel-xlsx", "--as", "gileadeweb-excel-xlsx"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add --as: %v", err)
	}

	if !env.DirExists(env.GetWorktreePath("web", "gileadeweb-excel-xlsx")) {
		t.Error("--as must decide the directory name")
	}
	if env.DirExists(env.GetWorktreePath("web", "gileadeweb-marcus-feat-2072958-excel-xlsx")) {
		t.Error("the derived name must not also be created")
	}
}

// TestAdd_NameConflictIsRefused proves hydra never auto-suffixes a surprise
// directory name: a clear failure beats a directory the user did not ask for.
func TestAdd_NameConflictIsRefused(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "other")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "stage", "--as", "shared"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first add: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"add", "api", "other", "--as", "shared"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a colliding directory name must be refused")
	}
	e := output.Classify(err)
	if e.Code != output.CodeWorktreeNameConflict {
		t.Fatalf("error code = %q, want %q", e.Code, output.CodeWorktreeNameConflict)
	}
	if e.Details["existing_branch"] != "stage" {
		t.Errorf("details should name the occupying branch, got %v", e.Details)
	}
}

// TestAdd_IsConvergent pins invariant 3 for add: repeating add for an existing worktree
// exits 0 as a no-op. Sibling start/apply/clone tests cover their commands; add must
// match that convergent behaviour for idempotent provisioning scripts.
func TestAdd_IsConvergent(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first add: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"add", "api", "stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("second add must be a no-op at exit 0, got %v (code %q)",
			err, output.Classify(err).Code)
	}
}

// TestAdd_ConvergenceRequiresTheRequestedDirectory pins the boundary of invariant 3: `--as`
// names a directory, so a branch already checked out under a DIFFERENT name does not satisfy
// the request and must not report `skipped`.
//
// The test above cannot cover this, because without `--as` the converged path and the requested
// path are always the same string.
func TestAdd_ConvergenceRequiresTheRequestedDirectory(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first add: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"add", "api", "stage", "--as", "devcopy"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("add --as devcopy reported success for a directory it did not create")
	}
	if got := output.Classify(err).Code; got != output.CodeWorktreeExists {
		t.Fatalf("code: got %q, want %q", got, output.CodeWorktreeExists)
	}
	if _, statErr := os.Stat(filepath.Join(env.RootDir, "backend", "devcopy")); statErr == nil {
		t.Fatal("devcopy exists, so this no longer covers the intended case")
	}
}

// TestAdd_UnknownBaseIsBranchUnknown covers the resolution chain's failure mode:
// an unknown branch is created, but an unknown BASE cannot be invented.
func TestAdd_UnknownBaseIsBranchUnknown(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "feat/x", "--from", "does-not-exist"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an unknown --from base must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeBranchUnknown {
		t.Errorf("error code = %q, want %q", code, output.CodeBranchUnknown)
	}
}

func TestAdd_UnknownRepoIsRepoUnknown(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "nosuchrepo", "main"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an unknown alias must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeRepoUnknown {
		t.Errorf("error code = %q, want %q", code, output.CodeRepoUnknown)
	}
}

// TestRemove_DirtyWorktreeIsBlocked proves destructive work is gated, with the
// distinct exit code that lets a script tell "dirty" from "not found".
func TestRemove_DirtyWorktreeIsBlocked(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	worktree := env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.MakeWorktreeDirty(worktree)

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "stage", "--yes"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("removing a dirty worktree without --force must fail")
	}
	e := output.Classify(err)
	if e.Code != output.CodeWorktreeDirty {
		t.Fatalf("error code = %q, want %q", e.Code, output.CodeWorktreeDirty)
	}
	if e.Exit != 5 {
		t.Errorf("exit = %d, want 5", e.Exit)
	}
	if !env.DirExists(worktree) {
		t.Error("the worktree must survive a refused removal")
	}
}

// TestRemove_DeleteBranchIsAtomic ensures `remove --delete-branch` does not delete the
// worktree when branch deletion is refused: both halves succeed or neither does.
// TestRemove_DirtyWorktreeIsBlocked covers a different gate (dirty worktree, no branch delete).
func TestRemove_DeleteBranchIsAtomic(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	worktree := env.CreateWorktree("backend", "api", "feat/unmerged", "api-feat-unmerged")
	env.CreateCommit(worktree, "unmerged-work")
	bare := env.GetBarePath("api")

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feat/unmerged", "--yes", "--delete-branch"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("deleting an unmerged branch without --force must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeGitFailed {
		t.Errorf("error code = %q, want %q", code, output.CodeGitFailed)
	}

	// Neither half may have happened.
	if !env.DirExists(worktree) {
		t.Error("the worktree must survive a refused branch deletion")
	}
	if !git.RefExists(bare, "refs/heads/feat/unmerged") {
		t.Error("the branch must survive a refused deletion")
	}

	// --force does both.
	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feat/unmerged", "--yes", "--delete-branch", "--force"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("forced remove: %v", err)
	}
	if env.DirExists(worktree) {
		t.Error("worktree must be gone after a forced remove")
	}
	if git.RefExists(bare, "refs/heads/feat/unmerged") {
		t.Error("branch must be gone after a forced remove")
	}
}

// TestRemove_DeleteMergedBranchNeedsNoForce proves the pre-check does not
// over-refuse: a branch already merged into its base deletes cleanly.
func TestRemove_DeleteMergedBranchNeedsNoForce(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	// A brand-new branch with no commits of its own is merged into main by definition.
	worktree := env.CreateWorktree("backend", "api", "feat/empty", "api-feat-empty")
	bare := env.GetBarePath("api")

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feat/empty", "--yes", "--delete-branch"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("removing a merged branch must not need --force, got: %v", err)
	}
	if env.DirExists(worktree) {
		t.Error("worktree should be gone")
	}
	if git.RefExists(bare, "refs/heads/feat/empty") {
		t.Error("merged branch should be deleted")
	}
}

// TestRemove_PushedButUnmergedBranchIsRefused is the guard against the most
// dangerous possible shortcut here: judging deletability against
// origin/<same-branch>. That only proves the branch was pushed, so a live feature
// branch fully present on the remote would be hard-deleted as "safe". The safety
// target must be the repo's DEFAULT branch.
func TestRemove_PushedButUnmergedBranchIsRefused(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	// `feature` exists on origin and is identical to its remote tip, but carries a
	// commit that main does not have.
	_, remote, _ := env.SetupRepo("backend", "api", "main", "feature")
	env.Chdir()

	// Advance origin/feature past origin/main, then refresh the bare repo's view so
	// the local branch really is pushed AND unmerged.
	env.CommitToRemote(remote, "feature", "real feature work")
	bare := env.GetBarePath("api")
	if err := git.FetchBareRepo(bare); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	worktree := env.CreateWorktree("backend", "api", "feature", "api-feature")

	if git.IsBranchMerged(bare, "feature", "refs/remotes/origin/main") {
		t.Fatal("fixture must leave feature unmerged into main")
	}

	if !git.RefExists(bare, "refs/remotes/origin/feature") {
		t.Fatal("fixture must have the branch on origin")
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "feature", "--yes", "--delete-branch"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a pushed but unmerged branch must NOT be treated as deletable")
	}
	e := output.Classify(err)
	if e.Code != output.CodeGitFailed {
		t.Fatalf("error code = %q, want %q", e.Code, output.CodeGitFailed)
	}
	if target, _ := e.Details["merge_target"].(string); target != "refs/remotes/origin/main" {
		t.Errorf("merge target = %q, want refs/remotes/origin/main (never origin/<same-branch>)", target)
	}
	if !git.RefExists(bare, "refs/heads/feature") {
		t.Fatal("the unmerged branch must still exist")
	}
	if !env.DirExists(worktree) {
		t.Error("the worktree must survive a refused removal")
	}
}

// TestRemove_RefusesDefaultBranch: deleting the mainline is never what the user
// meant, and it would strand every other worktree's base.
func TestRemove_RefusesDefaultBranch(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	resetCommandState(t)
	rootCmd.SetArgs([]string{"remove", "api", "main", "--yes", "--delete-branch"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("deleting the default branch must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeGitFailed {
		t.Errorf("error code = %q, want %q", code, output.CodeGitFailed)
	}
	if !git.RefExists(env.GetBarePath("api"), "refs/heads/main") {
		t.Error("the default branch must survive")
	}
}

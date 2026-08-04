package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// Re-cloning a complete repository must be a no-op that exits 0.
//
// Before the engine, every branch that already had a worktree counted as a
// failure, so a fully-cloned repo produced git_failed "no worktree could be
// created for api" — precisely because everything was already correct. An agent
// re-running a clone to make sure it landed got a hard error naming no real
// problem.
func TestClone_ReRunOnCompleteRepoIsConvergent(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.Chdir()

	args := []string{"repo", "add", remote, "--as", "api", "--group", "backend",
		"--branches", "main,stage", "--output", "json"}

	resetCommandState(t)
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first clone: %v", err)
	}

	firstMain := env.GetWorktreePath("backend", "api")
	if !env.DirExists(firstMain) {
		t.Fatalf("precondition: %s must exist after the first clone", firstMain)
	}

	resetCommandState(t)
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("re-cloning a complete repository must succeed, got: %v", err)
	}

	// The existing worktrees must survive: a convergent no-op deletes nothing.
	if !env.DirExists(firstMain) {
		t.Error("the existing worktree was destroyed by a re-clone")
	}
	if !env.DirExists(env.GetBarePath("api")) {
		t.Error("the bare repository was destroyed by a re-clone")
	}
}

// A directory already used by a DIFFERENT branch stays a real conflict — the
// convergence rule must not swallow it.
func TestClone_DirectoryTakenByAnotherBranchStillConflicts(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	// Put "stage" in the directory that cloning "other" would derive, by using --as.
	resetCommandState(t)
	rootCmd.SetArgs([]string{"add", "api", "stage", "--as", "api-taken", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"add", "api", "main", "--as", "api-taken", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a directory held by another branch must not be reused")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeNameConflict {
		t.Errorf("code = %q, want %q", code, output.CodeWorktreeNameConflict)
	}
}

// A converged clone must still report what is on disk. Reporting nothing would be
// indistinguishable from having done nothing at all.
func TestClone_ConvergedRunStillReportsWorktrees(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.Chdir()

	args := []string{"repo", "add", remote, "--as", "api", "--group", "backend",
		"--branches", "main,stage", "--output", "json"}

	resetCommandState(t)
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first clone: %v", err)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(args)
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("second clone: %v", err)
	}

	// decodeJSONData already unwraps the envelope's "data", so this struct matches
	// the payload, not the envelope.
	var payload struct {
		Worktrees []worktreeJSON `json:"worktrees"`
	}
	decodeJSONData(t, stdout, &payload)
	if len(payload.Worktrees) != 2 {
		t.Errorf("converged clone reported %d worktrees, want 2", len(payload.Worktrees))
	}
}

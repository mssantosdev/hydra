package cmd

import (
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// repo set is the only way to change a repository's declared shape without hand-editing
// .hydra/config.yaml — a file hydra also writes, so the two were competing over one
// document.

func TestRepoSet_DeclaresBranches(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "prod")
	env.Chdir()

	rootCmd.SetArgs([]string{"repo", "set", "api", "--branches", "main,stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo set: %v", err)
	}

	cfg, err := config.Load(config.ManifestPath(env.RootDir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ref, _ := cfg.FindRepo("api")
	if got := strings.Join(ref.Repo.Branches, ","); got != "main,stage" {
		t.Errorf("declared = %q, want main,stage", got)
	}
}

// Declaring fewer branches must never delete a worktree. A declaration is what a restore
// builds; deleting work is a different decision with a different command, and one that asks
// about unmerged commits.
func TestRepoSet_NarrowingDoesNotDeleteWorktrees(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()
	present := env.CreateWorktree("backend", "api", "stage", "api-stage")

	resetCommandState(t)
	rootCmd.SetArgs([]string{"repo", "set", "api", "--branches", "main"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo set: %v", err)
	}

	if !env.DirExists(present) {
		t.Error("narrowing the declaration deleted an existing worktree")
	}
}

// A name that does not exist on origin is refused rather than written, so the failure lands
// on whoever typed it and not on whoever restores the workspace next.
func TestRepoSet_UnknownBranchIsRefused(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"repo", "set", "api", "--branches", "main,nope"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a branch absent from origin must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeBranchUnknown {
		t.Fatalf("code = %q, want %q", code, output.CodeBranchUnknown)
	}

	// And nothing was written.
	cfg, _ := config.Load(config.ManifestPath(env.RootDir))
	ref, _ := cfg.FindRepo("api")
	if len(ref.Repo.Branches) != 0 {
		t.Errorf("a refused declaration was still recorded: %v", ref.Repo.Branches)
	}
}

func TestRepoSet_UnknownRepoListsKnown(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	rootCmd.SetArgs([]string{"repo", "set", "nope", "--branches", "main"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an unregistered alias must be refused")
	}
	e := output.Classify(err)
	if e.Code != output.CodeRepoUnknown {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeRepoUnknown)
	}
	if e.Details["known"] == nil {
		t.Error("details.known must list real aliases so a caller can self-correct")
	}
}

// Invariant 5: a missing value is needs_input naming it, never a prompt when the output is
// machine-readable. Without --branches on a non-TTY, `repo set` must not fall through to the
// clone path's "nothing selected means the default branch", which would silently narrow an
// existing declaration to one entry.
func TestRepoSet_NonInteractiveWithoutBranchesNeedsInput(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	if err := config.Update(env.RootDir, func(live *config.Config) error {
		ref, _ := live.FindRepo("api")
		ref.Repo.Branches = []string{"main", "stage"}
		live.SetRepo(ref.Group, ref.Alias, ref.Repo)
		return nil
	}); err != nil {
		t.Fatalf("seed declaration: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"repo", "set", "api"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("no --branches and no terminal must be needs_input, not a silent narrowing")
	}
	e := output.Classify(err)
	if e.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeNeedsInput)
	}
	if e.Details["missing"] == nil {
		t.Error("details.missing must name --branches")
	}

	// The prior declaration is intact.
	cfg, _ := config.Load(config.ManifestPath(env.RootDir))
	ref, _ := cfg.FindRepo("api")
	if got := strings.Join(ref.Repo.Branches, ","); got != "main,stage" {
		t.Errorf("declaration changed on a refused call: %q", got)
	}
}

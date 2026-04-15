package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestStatus_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	// Don't create config
	env.Chdir()

	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when no config")
	}

	testutil.Contains(t, err.Error(), "no .hydra.yaml")
}

func TestStatus_EmptyProject(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	rootCmd.SetArgs([]string{"status"})

	// Should complete without error, showing zero counts
	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Status empty project failed: %v", err)
	}
}

func TestStatus_WithCleanWorktrees(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create clean worktrees
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.CreateWorktree(bareRepo, "develop")
	env.AddToConfig("backend", "api", "api")

	env.Chdir()

	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Status with clean worktrees failed: %v", err)
	}
}

func TestStatus_Counts(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create clean worktree
	cleanRepo := env.CreateBareRepo("clean-api")
	env.CreateWorktree(cleanRepo, "main")
	env.AddToConfig("backend", "clean-api", "clean-api")

	// Create dirty worktree
	dirtyRepo := env.CreateBareRepo("dirty-api")
	dirtyWt := env.CreateWorktree(dirtyRepo, "main")
	env.AddToConfig("backend", "dirty-api", "dirty-api")
	env.MakeWorktreeDirty(dirtyWt)

	env.Chdir()

	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Status counts failed: %v", err)
	}
}

func TestStatus_NavigationPaths(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create worktrees with different suffixes
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.CreateWorktree(bareRepo, "stage")
	env.AddToConfig("backend", "api", "api")

	env.Chdir()

	rootCmd.SetArgs([]string{"status"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Status navigation paths failed: %v", err)
	}
}

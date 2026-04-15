package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestList_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	// Don't create config
	env.Chdir()

	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when no config")
	}

	testutil.Contains(t, err.Error(), "no .hydra.yaml")
}

func TestList_EmptyProject(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	rootCmd.SetArgs([]string{"list"})

	// Should complete without error but show "no worktrees found"
	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("List empty project failed: %v", err)
	}
}

func TestList_WithWorktrees(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create a bare repo with worktrees
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.CreateWorktree(bareRepo, "develop")
	env.AddToConfig("backend", "api", "api")

	env.Chdir()

	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("List with worktrees failed: %v", err)
	}
}

func TestList_WithDirtyWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create a bare repo with worktree
	bareRepo := env.CreateBareRepo("web")
	mainWt := env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("frontend", "web", "web")

	// Make worktree dirty
	env.MakeWorktreeDirty(mainWt)

	env.Chdir()

	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("List with dirty worktree failed: %v", err)
	}
}

func TestList_MultipleGroups(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create backend group
	backendRepo := env.CreateBareRepo("api")
	env.CreateWorktree(backendRepo, "main")
	env.AddToConfig("backend", "api", "api")

	// Create frontend group
	frontendRepo := env.CreateBareRepo("web")
	env.CreateWorktree(frontendRepo, "main")
	env.AddToConfig("frontend", "web", "web")

	// Create infra group
	infraRepo := env.CreateBareRepo("terraform")
	env.CreateWorktree(infraRepo, "main")
	env.AddToConfig("infra", "terraform", "terraform")

	env.Chdir()

	rootCmd.SetArgs([]string{"list"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("List multiple groups failed: %v", err)
	}
}

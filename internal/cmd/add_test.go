package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestAdd_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	// Don't create config
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "main"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when no config")
	}

	testutil.Contains(t, err.Error(), "no .hydra.yaml")
}

func TestAdd_UnknownAlias(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "unknown-alias", "main"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error for unknown alias")
	}

	testutil.Contains(t, err.Error(), "unknown alias")
}

func TestAdd_NoBareRepo(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.AddToConfig("backend", "api", "api")
	env.Chdir()

	rootCmd.SetArgs([]string{"add", "api", "main"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when bare repo not found")
	}

	testutil.Contains(t, err.Error(), "bare repository not found")
}

func TestAdd_CreateNewWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create bare repo with main worktree
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("backend", "api", "api")

	env.Chdir()

	// Create a new worktree for feature branch
	rootCmd.SetArgs([]string{"add", "api", "feature/new-feature"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Checkout new worktree failed: %v", err)
	}

	// Verify worktree was created
	worktreePath := env.GetWorktreePath("api", "feature-new-feature")
	if !env.DirExists(worktreePath) {
		t.Error("New worktree should exist")
	}
}

func TestAdd_ExistingWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create bare repo with existing worktrees
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.CreateWorktree(bareRepo, "develop")
	env.AddToConfig("backend", "api", "api")

	env.Chdir()

	// Checkout existing worktree
	rootCmd.SetArgs([]string{"add", "api", "develop"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Checkout existing worktree failed: %v", err)
	}
}

func TestAdd_CreatesSymlink(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Setup
	bareRepo := env.CreateBareRepo("web")
	env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("frontend", "web", "web")

	// Create group directory
	groupDir := filepath.Join(env.RootDir, "frontend")
	os.MkdirAll(groupDir, 0755)

	env.Chdir()

	// Checkout with branch
	rootCmd.SetArgs([]string{"add", "web", "feature-test"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Checkout with symlink failed: %v", err)
	}

	// Verify symlink exists
	symlinkPath := filepath.Join(groupDir, "web-feature-test")
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Error("Symlink should exist")
	}
}

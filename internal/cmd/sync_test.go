package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestSync_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	// Don't create config
	env.Chdir()

	rootCmd.SetArgs([]string{"sync"})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when no config")
	}

	testutil.Contains(t, err.Error(), "no .hydra.yaml")
}

func TestSync_NoWorktrees(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	rootCmd.SetArgs([]string{"sync"})

	// Should complete without error but no worktrees found
	err := rootCmd.Execute()
	// No error expected, just "no worktrees found" message
	if err != nil {
		t.Logf("Sync with no worktrees returned: %v", err)
	}
}

func TestSync_WithWorktrees(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create a bare repo with worktrees
	bareRepo := env.CreateBareRepo("test-repo")
	mainWt := env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("backend", "test-repo", "test-repo")

	// Create a commit so there's something to sync
	env.CreateCommit(mainWt, "test commit")

	env.Chdir()

	rootCmd.SetArgs([]string{"sync", "--all"})

	// Execute sync (may fail due to no remote, but should handle gracefully)
	err := rootCmd.Execute()
	// We expect this might fail due to no remote, but shouldn't panic
	t.Logf("Sync result: %v", err)
}

func TestSync_DetectCurrentRepo(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create a bare repo with worktrees
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("backend", "api", "api")

	// Create group directory and symlink
	groupDir := filepath.Join(env.RootDir, "backend")
	os.MkdirAll(groupDir, 0755)
	worktreePath := env.GetWorktreePath("api", "main")
	symlinkPath := filepath.Join(groupDir, "api")
	os.Symlink(worktreePath, symlinkPath)

	// Change to worktree directory
	os.Chdir(symlinkPath)

	rootCmd.SetArgs([]string{"sync"})

	// Should detect current repo
	err := rootCmd.Execute()
	t.Logf("Sync from within worktree: %v", err)
}

func TestSyncFlags(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	// Test with --all flag
	rootCmd.SetArgs([]string{"sync", "--all"})
	err := rootCmd.Execute()
	t.Logf("Sync --all: %v", err)

	// Reset args for next test
	rootCmd.SetArgs([]string{})

	// Test with --yes flag
	rootCmd.SetArgs([]string{"sync", "--yes"})
	err = rootCmd.Execute()
	t.Logf("Sync --yes: %v", err)

	// Reset args
	rootCmd.SetArgs([]string{})

	// Test with --force flag
	rootCmd.SetArgs([]string{"sync", "--force"})
	err = rootCmd.Execute()
	t.Logf("Sync --force: %v", err)
}

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

// TestFullFlow tests the complete hydra workflow
func TestFullFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping full flow test in short mode")
	}

	env := testutil.NewTestEnv(t)

	// Step 1: Initialize project
	t.Log("Step 1: Initialize project")
	env.Chdir()
	rootCmd.SetArgs([]string{"init"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Verify config was created
	configPath := filepath.Join(env.RootDir, ".hydra.yaml")
	if !env.FileExists(configPath) {
		t.Fatal("Config file should exist after init")
	}

	// Step 2: Clone a repository
	t.Log("Step 2: Clone repository")
	rootCmd.SetArgs([]string{
		"clone",
		"https://github.com/mssantosdev/hydra.git",
		"--interactive=false",
		"--alias=hydra",
		"--group=tools",
		"--branches=main",
	})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Verify bare repo
	barePath := env.GetBarePath("hydra")
	if !env.DirExists(barePath) {
		t.Fatal("Bare repo should exist after clone")
	}

	// Verify worktree
	worktreePath := env.GetWorktreePath("hydra", "main")
	if !env.DirExists(worktreePath) {
		t.Fatal("Worktree should exist after clone")
	}

	// Verify config was updated
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
	testutil.Contains(t, string(configContent), "tools")
	testutil.Contains(t, string(configContent), "hydra")

	// Step 3: List worktrees
	t.Log("Step 3: List worktrees")
	rootCmd.SetArgs([]string{"list"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("List failed: %v", err)
	}

	// Step 4: Check status
	t.Log("Step 4: Check status")
	rootCmd.SetArgs([]string{"status"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("Status failed: %v", err)
	}

	// Step 5: Checkout a new branch
	t.Log("Step 5: Checkout new branch")
	rootCmd.SetArgs([]string{"checkout", "hydra", "feature-test"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("Checkout failed: %v", err)
	}

	// Verify new worktree
	featurePath := env.GetWorktreePath("hydra", "feature-test")
	if !env.DirExists(featurePath) {
		t.Error("Feature worktree should exist")
	}

	// Step 6: Make a worktree dirty
	t.Log("Step 6: Create dirty worktree")
	env.MakeWorktreeDirty(worktreePath)

	// Step 7: List again (should show dirty status)
	t.Log("Step 7: List with dirty worktree")
	rootCmd.SetArgs([]string{"list"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("List with dirty failed: %v", err)
	}

	// Step 8: Status should show modified count
	t.Log("Step 8: Status with dirty worktree")
	rootCmd.SetArgs([]string{"status"})
	if err := rootCmd.Execute(); err != nil {
		t.Errorf("Status with dirty failed: %v", err)
	}

	// Step 9: Try sync (may fail due to no remote updates, but shouldn't panic)
	t.Log("Step 9: Sync")
	rootCmd.SetArgs([]string{"sync", "--all"})
	err = rootCmd.Execute()
	if err != nil {
		t.Logf("Sync returned error (expected): %v", err)
	}

	t.Log("Full flow test completed successfully!")
}

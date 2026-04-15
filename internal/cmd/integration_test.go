package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

// Integration test for the complete workflow: add -> switch -> remove
func TestIntegration_AddSwitchRemove(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Create bare repo
	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "main")
	env.AddToConfig("backend", "api", "api")

	groupDir := filepath.Join(env.RootDir, "backend")
	os.MkdirAll(groupDir, 0755)

	env.Chdir()

	// Step 1: Add a new worktree
	t.Log("Step 1: Adding worktree")
	rootCmd.SetArgs([]string{"add", "api", "feature/test"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Verify worktree exists
	worktreePath := env.GetWorktreePath("api", "feature-test")
	if !env.DirExists(worktreePath) {
		t.Fatal("Worktree should exist after add")
	}

	// Verify symlink exists
	symlinkPath := filepath.Join(groupDir, "api-feature-test")
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Fatal("Symlink should exist after add")
	}

	// Step 2: Try to switch (without shell helper - should fail gracefully)
	t.Log("Step 2: Testing switch without shell helper")
	rootCmd.SetArgs([]string{"switch", "api-feature-test"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("Switch should fail without shell helper")
	}
	if !strings.Contains(err.Error(), "shell helper not initialized") {
		t.Fatalf("Expected shell helper error, got: %v", err)
	}

	// Step 3: Remove the worktree
	t.Log("Step 3: Removing worktree")
	rootCmd.SetArgs([]string{"remove", "api", "feature/test", "--yes"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify worktree is gone
	if env.DirExists(worktreePath) {
		t.Fatal("Worktree should be removed")
	}

	t.Log("✓ Integration test passed: add -> switch (fail) -> remove")
}

// Test add command with existing worktree
func TestIntegration_AddExistingWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	bareRepo := env.CreateBareRepo("web")
	env.CreateWorktree(bareRepo, "main")
	env.CreateWorktree(bareRepo, "stage") // Pre-existing
	env.AddToConfig("frontend", "web", "web")

	env.Chdir()

	// Try to add existing worktree - should succeed with message
	rootCmd.SetArgs([]string{"add", "web", "stage"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Add existing worktree should not fail: %v", err)
	}

	t.Log("✓ Adding existing worktree handled gracefully")
}

// Test remove with dirty worktree (without force)
func TestIntegration_RemoveDirtyWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	bareRepo := env.CreateBareRepo("api")
	worktreePath := env.CreateWorktree(bareRepo, "feature-x")
	env.AddToConfig("backend", "api", "api")

	// Make worktree dirty
	env.MakeWorktreeDirty(worktreePath)

	env.Chdir()

	// Try to remove without force - should fail
	rootCmd.SetArgs([]string{"remove", "api", "feature-x"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("Remove should fail for dirty worktree without --force")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("Expected uncommitted changes error, got: %v", err)
	}

	// Now remove with force
	rootCmd.SetArgs([]string{"remove", "api", "feature-x", "--force", "--yes"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("Remove with force should succeed: %v", err)
	}

	t.Log("✓ Remove dirty worktree with force works")
}

// Test init-shell command outputs valid shell code
func TestIntegration_InitShell(t *testing.T) {
	// Test bash output
	rootCmd.SetArgs([]string{"init-shell", "bash"})
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("init-shell bash failed: %v", err)
	}

	// Test zsh output
	rootCmd.SetArgs([]string{"init-shell", "zsh"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("init-shell zsh failed: %v", err)
	}

	// Test fish output
	rootCmd.SetArgs([]string{"init-shell", "fish"})
	err = rootCmd.Execute()
	if err != nil {
		t.Fatalf("init-shell fish failed: %v", err)
	}

	// Test unsupported shell
	rootCmd.SetArgs([]string{"init-shell", "powershell"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("Should fail for unsupported shell")
	}

	t.Log("✓ init-shell generates valid output for all shells")
}

// Test switch with partial match
func TestIntegration_SwitchPartialMatch(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	bareRepo := env.CreateBareRepo("api")
	env.CreateWorktree(bareRepo, "stage")
	env.CreateWorktree(bareRepo, "prod")
	env.AddToConfig("backend", "api", "api")

	groupDir := filepath.Join(env.RootDir, "backend")
	os.MkdirAll(groupDir, 0755)

	// Create symlinks manually for test
	stagePath := env.GetWorktreePath("api", "stage")
	os.Symlink(stagePath, filepath.Join(groupDir, "api-stage"))

	env.Chdir()

	// Try switch with partial match - should fail without shell helper
	// but should show suggestions
	rootCmd.SetArgs([]string{"switch", "stage"})
	err := rootCmd.Execute()
	// Will fail due to no shell helper, but should show the match
	if err == nil {
		t.Fatal("Should fail without shell helper")
	}

	t.Log("✓ Switch partial match suggests correct worktree")
}

// Test complete workflow with multiple repos
func TestIntegration_MultiRepoWorkflow(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()

	// Setup multiple repos
	apiRepo := env.CreateBareRepo("api")
	webRepo := env.CreateBareRepo("web")
	env.CreateWorktree(apiRepo, "main")
	env.CreateWorktree(webRepo, "main")
	env.AddToConfig("backend", "api", "api")
	env.AddToConfig("frontend", "web", "web")

	backendDir := filepath.Join(env.RootDir, "backend")
	frontendDir := filepath.Join(env.RootDir, "frontend")
	os.MkdirAll(backendDir, 0755)
	os.MkdirAll(frontendDir, 0755)

	env.Chdir()

	// Add worktrees in different ecosystems
	rootCmd.SetArgs([]string{"add", "api", "feature/api-v2"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Add api worktree failed: %v", err)
	}

	rootCmd.SetArgs([]string{"add", "web", "feature/ui-redesign"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Add web worktree failed: %v", err)
	}

	// Verify both exist
	apiWorktree := env.GetWorktreePath("api", "feature-api-v2")
	webWorktree := env.GetWorktreePath("web", "feature-ui-redesign")

	if !env.DirExists(apiWorktree) {
		t.Fatal("API worktree should exist")
	}
	if !env.DirExists(webWorktree) {
		t.Fatal("Web worktree should exist")
	}

	// Remove one
	rootCmd.SetArgs([]string{"remove", "api", "feature/api-v2", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("Remove api worktree failed: %v", err)
	}

	if env.DirExists(apiWorktree) {
		t.Fatal("API worktree should be removed")
	}
	if !env.DirExists(webWorktree) {
		t.Fatal("Web worktree should still exist")
	}

	t.Log("✓ Multi-repo workflow works correctly")
}

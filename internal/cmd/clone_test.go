package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestClone_DryRun(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	// Execute clone with dry-run
	rootCmd.SetArgs([]string{
		"clone",
		"https://github.com/mssantosdev/hydra.git",
		"--dry-run",
		"--interactive=false",
		"--alias=test-repo",
		"--group=backend",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Clone dry-run failed: %v", err)
	}

	// Verify no actual clone happened
	barePath := env.GetBarePath("test-repo")
	if env.DirExists(barePath) {
		t.Error("Dry-run should not create bare repo")
	}
}

func TestClone_RequiresConfigOrInteractive(t *testing.T) {
	env := testutil.NewTestEnv(t)
	// Don't init config
	env.Chdir()

	// Without config and without interactive, should fail
	rootCmd.SetArgs([]string{
		"clone",
		"https://github.com/mssantosdev/hydra.git",
		"--interactive=false",
	})

	err := rootCmd.Execute()
	if err == nil {
		t.Error("Expected error when no config and non-interactive")
	}

	testutil.Contains(t, err.Error(), "no .hydra.yaml found")
}

func TestClone_ExtractRepoName(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://github.com/user/repo.git", "repo"},
		{"https://github.com/user/repo", "repo"},
		{"git@github.com:user/repo.git", "repo"},
		{"github.com/user/repo", "repo"},
	}

	for _, tt := range tests {
		result := extractRepoName(tt.url)
		if result != tt.expected {
			t.Errorf("extractRepoName(%q) = %q, want %q", tt.url, result, tt.expected)
		}
	}
}

func TestClone_RealRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real clone test in short mode")
	}

	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	// Execute real clone (non-interactive)
	rootCmd.SetArgs([]string{
		"clone",
		"https://github.com/mssantosdev/hydra.git",
		"--interactive=false",
		"--alias=hydra-test",
		"--group=tools",
		"--branches=main",
	})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	// Verify bare repo was created
	barePath := env.GetBarePath("hydra-test")
	if !env.DirExists(barePath) {
		t.Error("Bare repo should exist")
	}

	// Verify worktree was created
	worktreePath := env.GetWorktreePath("hydra-test", "main")
	if !env.DirExists(worktreePath) {
		t.Error("Worktree should exist")
	}

	// Verify config was updated
	configPath := filepath.Join(env.RootDir, ".hydra.yaml")
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	configStr := string(content)
	testutil.Contains(t, configStr, "tools")
	testutil.Contains(t, configStr, "hydra-test")

	// Verify symlink exists
	symlinkPath := filepath.Join(env.RootDir, "tools", "hydra-test")
	if _, err := os.Lstat(symlinkPath); err != nil {
		t.Error("Symlink should exist")
	}
}

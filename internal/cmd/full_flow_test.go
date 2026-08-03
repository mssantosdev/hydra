package cmd

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestFullFlow_InitAndClone(t *testing.T) {
	resetCommandState(t)
	if testing.Short() {
		t.Skip("short mode")
	}
	env := testutil.NewTestEnv(t)
	env.Chdir()

	rootCmd.SetArgs([]string{"init"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}
	configPath := filepath.Join(env.RootDir, ".hydra.yaml")
	if !env.FileExists(configPath) {
		t.Fatal("missing config after init")
	}

	remote := env.CreateRemoteRepo("flow-origin", "main")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"clone", remote, "--alias", "api", "--group", "tools", "--branches", "main"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	if !env.DirExists(env.GetBarePath("api")) {
		t.Fatal("missing bare repo")
	}
	if !env.DirExists(env.GetWorktreePath("tools", "api")) {
		t.Fatal("missing worktree")
	}
}

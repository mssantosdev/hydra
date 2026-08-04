package cmd

import (
	"bytes"
	"github.com/mssantosdev/hydra/internal/config"
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
	configPath := config.ManifestPath(env.RootDir)
	if !env.FileExists(configPath) {
		t.Fatal("missing config after init")
	}

	remote := env.CreateRemoteRepo("flow-origin", "main")
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"repo", "add", remote, "--as", "api", "--group", "tools", "--branches", "main"})
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

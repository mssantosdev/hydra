package cmd

import (
	"bytes"
	"github.com/mssantosdev/hydra/internal/config"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
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

// A second init in the same directory is a usage mistake, not a tool defect. It reported
// `internal` because the check that knew what happened returned a bare
// fmt.Errorf and the caller relabelled every failure as internal. `internal` is the one
// code an agent is told to treat as "hydra is broken", so misusing it costs a retry loop
// and a bug report. Both creation paths must name themselves.
func TestInitTwiceReportsProjectExists(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.Chdir()

	rootCmd.SetArgs([]string{"init"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("first init: %v", err)
	}

	resetCommandState(t)
	env.Chdir()
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"init"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("second init succeeded; it must refuse an existing workspace")
	}

	got := output.Classify(err)
	if got.Code != output.CodeProjectExists {
		t.Errorf("code = %q, want %q (message: %s)", got.Code, output.CodeProjectExists, got.Message)
	}
	if got.Exit != output.ExitFor(output.CodeProjectExists) {
		t.Errorf("exit = %d, want %d", got.Exit, output.ExitFor(output.CodeProjectExists))
	}
}

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestProjectLsOutsideWorkspace(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	outside := filepath.Join(env.RootDir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("chdir outside: %v", err)
	}

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	cleanup := withJSONOutput(t)
	defer cleanup()
	rootCmd.SetArgs([]string{"project", "ls"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("project ls outside workspace: %v", err)
	}
}

func TestProjectAddListPruneAndRm(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	cleanup := withJSONOutput(t)
	defer cleanup()

	rootCmd.SetArgs([]string{"project", "add", "demo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("project add: %v", err)
	}

	reg, err := registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if root, ok := reg.Resolve("demo"); !ok || root != env.RootDir {
		t.Fatalf("registry resolve demo = %q ok=%v want %q", root, ok, env.RootDir)
	}

	stdout.Reset()
	rootCmd.SetArgs([]string{"project", "ls"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("project ls: %v", err)
	}
	var payload projectLsPayload
	decodeJSONData(t, &stdout, &payload)
	if len(payload.Projects) != 1 || !payload.Projects[0].Exists {
		t.Fatalf("projects = %+v", payload.Projects)
	}

	if err := os.Remove(filepath.Join(env.RootDir, ".hydra.yaml")); err != nil {
		t.Fatalf("remove config: %v", err)
	}

	stdout.Reset()
	rootCmd.SetArgs([]string{"project", "ls", "--prune"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("project ls --prune: %v", err)
	}
	decodeJSONData(t, &stdout, &payload)
	if len(payload.Projects) != 0 || len(payload.Pruned) != 1 {
		t.Fatalf("after prune projects=%+v pruned=%v", payload.Projects, payload.Pruned)
	}

	env.InitConfig()
	rootCmd.SetArgs([]string{"project", "add", "demo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("re-add project: %v", err)
	}

	stdout.Reset()
	rootCmd.SetArgs([]string{"project", "rm", "demo"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("project rm: %v", err)
	}
	reg, err = registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if _, ok := reg.Resolve("demo"); ok {
		t.Fatal("demo should be removed from registry")
	}
}

func TestProjectRmUnknown(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.Chdir()

	rootCmd.SetArgs([]string{"project", "rm", "missing"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown project")
	}
	he := output.Classify(err)
	if he.Code != output.CodeProjectUnknown || he.Exit != 2 {
		t.Fatalf("got code=%s exit=%d", he.Code, he.Exit)
	}
}

func TestProjectAddWithoutConfigDoesNotRegister(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	empty := filepath.Join(env.RootDir, "empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chdir(empty); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	rootCmd.SetArgs([]string{"project", "add", "ghost"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected not_in_project error")
	}
	he := output.Classify(err)
	if he.Code != output.CodeNotInProject {
		t.Fatalf("code = %s", he.Code)
	}

	reg, err := registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if _, ok := reg.Resolve("ghost"); ok {
		t.Fatal("ghost should not be registered")
	}
}

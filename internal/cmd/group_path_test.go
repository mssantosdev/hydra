package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// Group `path:` relocates worktrees; doctor orphan scan, prune, and remove empty-group
// cleanup must all resolve the declared directory — not filepath.Join(root, group name).

func withGroupPath(t *testing.T, group, path string) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo(group, "api", "main", "stage")

	if err := config.Update(env.RootDir, func(live *config.Config) error {
		entry := live.Groups[group]
		entry.Path = path
		live.Groups[group] = entry
		return nil
	}); err != nil {
		t.Fatalf("declare path: %v", err)
	}
	env.Chdir()
	resetCommandState(t)
	return env
}

func TestGroupPath_PlacesWorktreesUnderIt(t *testing.T) {
	resetCommandState(t)
	env := withGroupPath(t, "backend", "services")

	rootCmd.SetArgs([]string{"add", "api", "stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	if _, err := os.Stat(filepath.Join(env.RootDir, "services", "api-stage")); err != nil {
		t.Errorf("worktree was not placed under the declared path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.RootDir, "backend", "api-stage")); err == nil {
		t.Error("worktree was placed under the group NAME, ignoring path:")
	}
}

// doctor's orphan scan reads the group directory. Pointed at the wrong one it finds nothing and
// reports a clean workspace, which is worse than an error.
func TestGroupPath_DoctorScansTheDeclaredDirectory(t *testing.T) {
	resetCommandState(t)
	env := withGroupPath(t, "backend", "services")

	// A directory that LOOKS like a git worktree — the check requires a .git entry — but is not
	// registered, placed inside the declared group path. doctor must see it there.
	orphan := filepath.Join(env.RootDir, "services", "api-orphan")
	if err := os.MkdirAll(orphan, 0o750); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orphan, ".git"), []byte("gitdir: /nowhere\n"), 0o600); err != nil {
		t.Fatalf("write .git: %v", err)
	}

	buf, done := withDoctorJSON(t)
	defer done()
	rootCmd.SetArgs([]string{"doctor", "--output", "json"})
	_ = rootCmd.Execute()

	if !strings.Contains(buf.String(), "api-orphan") {
		t.Errorf("doctor did not scan the declared group path; output was:\n%s", buf.String())
	}
}

// prune must see the declared group directory; pointing at the group name finds nothing to prune.
func TestGroupPath_PruneSeesTheDeclaredDirectory(t *testing.T) {
	resetCommandState(t)
	env := withGroupPath(t, "backend", "services")

	// An empty group directory at the declared path. `backend/` is deliberately left absent, so a
	// prune that resolved the group NAME would read nothing and report nothing removed.
	declared := filepath.Join(env.RootDir, "services")
	if err := os.RemoveAll(declared); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := os.MkdirAll(declared, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"prune", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if _, err := os.Stat(declared); err == nil {
		t.Error("prune did not remove the empty group directory at its declared path")
	}
}

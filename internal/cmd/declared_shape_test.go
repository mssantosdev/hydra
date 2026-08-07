package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// A manifest used to record repositories and their default branch only, so a workspace
// rebuilt from it had one worktree per repo and the caller was pointed at a captured
// `hydra list --output json` for the rest — which also carried that machine's topic
// membership. `branches:` makes the manifest enough on its own.

func TestRepoAdd_RecordsDeclaredBranches(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "prod")
	env.Chdir()

	// SetupRepo already registered it; re-registering with an explicit branch set is the
	// path a user takes, and it must land in the manifest rather than being forgotten.
	resetCommandState(t)
	rootCmd.SetArgs([]string{"repo", "list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo list: %v", err)
	}

	cfg, err := config.Load(config.ManifestPath(env.RootDir))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	ref, ok := cfg.FindRepo("api")
	if !ok {
		t.Fatal("api is not registered")
	}
	// SetupRepo does not go through repo add, so the declaration is absent here. What this
	// pins is that absence is representable — a manifest written before `branches:` existed
	// must keep working, with restore falling back to the default branch.
	if len(ref.Repo.Branches) != 0 {
		t.Errorf("branches = %v, want empty for a repo registered without a declaration", ref.Repo.Branches)
	}
}

// A declaration in the manifest is what `repo restore` builds, and it must not be inferred
// from what happens to be on disk.
func TestRepoRestore_CreatesDeclaredBranches(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage", "prod")
	env.Chdir()

	// Declare the shape, then delete the workspace's worktrees and bare repo to make
	// restore do real work — the manifest is all that survives.
	if err := config.Update(env.RootDir, func(live *config.Config) error {
		ref, ok := live.FindRepo("api")
		if !ok {
			t.Fatal("api is not registered")
		}
		ref.Repo.Branches = []string{"main", "stage"}
		live.SetRepo(ref.Group, ref.Alias, ref.Repo)
		return nil
	}); err != nil {
		t.Fatalf("declare branches: %v", err)
	}

	cfg, err := config.Load(config.ManifestPath(env.RootDir))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	ref, _ := cfg.FindRepo("api")
	if got := strings.Join(ref.Repo.Branches, ","); got != "main,stage" {
		t.Fatalf("declaration did not round-trip through the manifest: %q", got)
	}

	resetCommandState(t)
	rootCmd.SetArgs([]string{"repo", "restore", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo restore: %v", err)
	}
}

// The summary and next[] both used to claim the manifest could only produce default
// branches. That is now false when a declaration exists, and still true when it does not —
// so the message has to depend on the manifest, not be hardcoded either way.
func TestRestoreSummary_DistinguishesDeclaredFromDefaultOnly(t *testing.T) {
	declared := restoreSummary(restoreJSON{Cloned: 1, Declared: 3})
	if !strings.Contains(declared, "3 declared worktree(s)") {
		t.Errorf("a declared manifest should say so, got %q", declared)
	}
	if strings.Contains(declared, "default-branch worktrees only") {
		t.Errorf("a declared manifest must not claim default-branch only, got %q", declared)
	}

	legacy := restoreSummary(restoreJSON{Cloned: 1, Declared: 0})
	if !strings.Contains(legacy, "default-branch worktrees only") {
		t.Errorf("a manifest with no declaration still restores default branches only, got %q", legacy)
	}

	// And the next[] hint is only useful when the manifest cannot describe the shape.
	if next := restoreNext([]declaredRepo{{Alias: "api", Branches: []string{"main"}}}); next != nil {
		t.Errorf("a declared manifest needs no `apply -` hint, got %v", next)
	}
	if next := restoreNext([]declaredRepo{{Alias: "api"}}); len(next) == 0 {
		t.Error("a manifest with no declaration should still point at `apply -`")
	}
}

func TestDeclaredWorktrees_CountsOnlyExplicitDeclarations(t *testing.T) {
	// A repo with no declaration contributes zero, not one: restore has always created its
	// default branch, and counting that would make every legacy manifest claim a shape.
	got := declaredWorktrees([]declaredRepo{
		{Alias: "api", Branches: []string{"main", "stage", "prod"}},
		{Alias: "web"},
	})
	if got != 3 {
		t.Errorf("declaredWorktrees = %d, want 3", got)
	}
}

// repo list is the read path for the declaration, so it has to come from the manifest and
// not from whatever worktrees happen to exist.
func TestRepoList_ReportsDeclaredBranches(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	if err := config.Update(env.RootDir, func(live *config.Config) error {
		ref, _ := live.FindRepo("api")
		ref.Repo.Branches = []string{"main", "stage", "prod"}
		live.SetRepo(ref.Group, ref.Alias, ref.Repo)
		return nil
	}); err != nil {
		t.Fatalf("declare: %v", err)
	}

	// A buffer, and the previous writer restored: leaving rootCmd.SetOut pointed at a
	// closed file poisons every later test in the package, which is exactly what happened
	// the first time this was written with a temp file.
	var buf bytes.Buffer
	old := rootCmd.OutOrStdout()
	t.Cleanup(func() { rootCmd.SetOut(old) })
	resetCommandState(t)
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"repo", "list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("repo list: %v", err)
	}
	raw := buf.Bytes()

	var env2 struct {
		Data struct {
			Repos []struct {
				Alias    string   `json:"alias"`
				Branches []string `json:"branches"`
			} `json:"repos"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env2); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if len(env2.Data.Repos) == 0 {
		t.Fatal("repo list reported no repositories")
	}
	if got := strings.Join(env2.Data.Repos[0].Branches, ","); got != "main,stage,prod" {
		t.Errorf("branches = %q, want main,stage,prod", got)
	}
}

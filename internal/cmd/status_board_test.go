package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

// newBoardLoader claims the board reads git through the same resolver as list and status, so the
// two cannot drift. That is only a claim while nothing compares them.
func TestBoardLoaderMatchesList(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}

	// What list reports.
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list: %v", err)
	}
	var listed struct {
		Worktrees []struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
			Path   string `json:"path"`
		} `json:"worktrees"`
	}
	decodeJSONData(t, stdout, &listed)

	// What the board loads, through the same globals a real invocation leaves behind.
	resetCommandState(t)
	resetCommandIO()
	if err := loadProject(); err != nil {
		t.Fatalf("loadProject: %v", err)
	}
	targets, _, err := projectTargets(false)
	if err != nil {
		t.Fatalf("projectTargets: %v", err)
	}
	rows, _, err := newBoardLoader(targets, currentSelector())()
	if err != nil {
		t.Fatalf("board loader: %v", err)
	}

	if len(rows) != len(listed.Worktrees) {
		t.Fatalf("board loaded %d rows, list reported %d", len(rows), len(listed.Worktrees))
	}
	byPath := map[string]string{}
	for _, r := range rows {
		byPath[r.Path] = r.Repo + "@" + r.Branch
	}
	for _, wt := range listed.Worktrees {
		got, ok := byPath[wt.Path]
		if !ok {
			t.Errorf("list reported %s but the board has no row for it", wt.Path)
			continue
		}
		if want := wt.Repo + "@" + wt.Branch; got != want {
			t.Errorf("board row for %s is %q, list says %q", wt.Path, got, want)
		}
	}
}

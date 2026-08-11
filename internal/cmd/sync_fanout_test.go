package cmd

import (
	"encoding/json"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// syncEnvelope is the shape sync emits; only the parts this file asserts on.
type syncEnvelope struct {
	Data struct {
		Worktrees []struct {
			Repo   string `json:"repo"`
			Branch string `json:"branch"`
			Status string `json:"status"`
			Pulled bool   `json:"pulled"`
		} `json:"worktrees"`
		Summary struct {
			Total  int `json:"total"`
			Pulled int `json:"pulled"`
			Failed int `json:"failed"`
		} `json:"summary"`
	} `json:"data"`
	Warnings []string `json:"warnings"`
}

// TestSync_HookFailureWarnsAndKeepsPulling: a failing post_sync hook is a warning after
// a successful pull; remaining worktrees still pull and stay in the envelope.
func TestSync_HookFailureWarnsAndKeepsPulling(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, _ := env.SetupRepo("backend", "api", "main", "stage")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostSync: []config.Hook{{Run: "exit 1"}},
	})
	env.Chdir()

	// Two worktrees that are genuinely behind, so both must pull.
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CommitToRemote(remote, "main", "upstream-main")
	env.CommitToRemote(remote, "stage", "upstream-stage")

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("a failing post_sync hook must not fail the command: %v", err)
	}

	var env2 syncEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env2); err != nil {
		t.Fatalf("decode sync envelope: %v\n%s", err, stdout.String())
	}

	if env2.Data.Summary.Pulled < 2 {
		t.Errorf("pulled = %d, want both worktrees: a hook failure must not stop the fan-out",
			env2.Data.Summary.Pulled)
	}
	if env2.Data.Summary.Failed != 0 {
		t.Errorf("failed = %d, want 0: the pulls succeeded", env2.Data.Summary.Failed)
	}
	if len(env2.Warnings) == 0 {
		t.Error("a failing hook must surface as a warning, not vanish")
	}
}

// Results must be ordered by (group, repo, branch), not by which pull finished
// first. An agent diffing two runs needs a stable order.
func TestSync_ResultOrderIsDeterministic(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, _ := env.SetupRepo("backend", "api", "main", "stage", "dev")
	env.Chdir()

	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CreateWorktree("backend", "api", "dev", "api-dev")
	for _, branch := range []string{"main", "stage", "dev"} {
		env.CommitToRemote(remote, branch, "upstream-"+branch)
	}

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	var decoded syncEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode sync envelope: %v\n%s", err, stdout.String())
	}
	if len(decoded.Data.Worktrees) < 3 {
		t.Fatalf("expected at least 3 worktrees, got %d", len(decoded.Data.Worktrees))
	}

	// dev < main < stage alphabetically; every worktree shares group and repo here.
	var branches []string
	for _, wt := range decoded.Data.Worktrees {
		branches = append(branches, wt.Branch)
	}
	for i := 1; i < len(branches); i++ {
		if branches[i-1] > branches[i] {
			t.Fatalf("worktrees are not sorted: %v", branches)
		}
	}
}

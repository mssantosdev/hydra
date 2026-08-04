package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func resetCommandIO() (*bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	return &stdout, &stderr
}

func runCommandJSON(t *testing.T, args ...string) (map[string]any, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout, stderr := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid JSON output: %v\nstdout: %s", err, stdout.String())
	}
	return envelope, stdout, stderr
}

func worktreesFromEnvelope(t *testing.T, envelope map[string]any) []map[string]any {
	t.Helper()
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %#v", envelope["data"])
	}
	raw, ok := data["worktrees"].([]any)
	if !ok {
		t.Fatalf("expected worktrees array, got %#v", data["worktrees"])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		wt, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected worktree object, got %#v", item)
		}
		out = append(out, wt)
	}
	return out
}

func warningsFromEnvelope(t *testing.T, envelope map[string]any) []string {
	t.Helper()
	raw, ok := envelope["warnings"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		s, _ := item.(string)
		out = append(out, s)
	}
	return out
}

func TestList_HasLsAlias(t *testing.T) {
	for _, alias := range listCmd.Aliases {
		if alias == "ls" {
			return
		}
	}
	t.Fatal("expected list command to expose ls alias")
}

func TestList_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	resetCommandState(t)
	resetCommandIO()
	env.Chdir()
	rootCmd.SetArgs([]string{"list"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no config")
	}
	testutil.Contains(t, err.Error(), "no .hydra/config.yaml")
}

func TestList_EmptyProject(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "list")
	data := envelope["data"].(map[string]any)
	if total, _ := data["total"].(float64); int(total) != 0 {
		t.Fatalf("expected total 0, got %v", total)
	}
}

func TestList_SetupRepoLayoutAndJSON(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, _, _ = env.SetupRepo("backend", "api", "main", "feature-auth")
	featurePath := env.CreateWorktree("backend", "api", "feature-auth", "api-feature-auth")
	expectedPath, err := filepath.EvalSymlinks(featurePath)
	if err != nil {
		t.Fatalf("eval symlinks: %v", err)
	}
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "list")
	if got, _ := envelope["schema"].(float64); int(got) != output.Schema {
		t.Fatalf("expected schema %d, got %v", output.Schema, envelope["schema"])
	}
	worktrees := worktreesFromEnvelope(t, envelope)
	if len(worktrees) != 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(worktrees))
	}
	var feature map[string]any
	for _, wt := range worktrees {
		if wt["name"] == "api-feature-auth" {
			feature = wt
			break
		}
	}
	if feature == nil {
		t.Fatal("expected api-feature-auth worktree")
	}
	if got, _ := feature["path"].(string); got != expectedPath {
		t.Fatalf("expected path %q, got %q", expectedPath, got)
	}
}

func TestList_LsAlias(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "ls")
	if got, _ := envelope["command"].(string); got != "ls" {
		t.Fatalf("expected command ls, got %q", got)
	}
}

func TestList_DirtyWorktree(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, _, mainPath := env.SetupRepo("backend", "api", "main")
	env.MakeWorktreeDirty(mainPath)
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "list")
	for _, wt := range worktreesFromEnvelope(t, envelope) {
		if wt["name"] == "api" {
			if dirty, _ := wt["dirty"].(bool); !dirty {
				t.Fatal("expected dirty worktree")
			}
			return
		}
	}
	t.Fatal("api worktree not found")
}

func TestList_MissingBareWarns(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	barePath2, remote2 := env.CreateBareRepo("web", "main")
	env.AddToConfig("frontend", "web", remote2, "main")
	_ = os.RemoveAll(barePath2)
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "list")
	found := false
	for _, w := range warningsFromEnvelope(t, envelope) {
		if strings.Contains(w, "bare repository missing") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected bare missing warning in envelope")
	}
	if len(worktreesFromEnvelope(t, envelope)) != 1 {
		t.Fatalf("expected surviving worktree from healthy repo")
	}
}

func TestList_AllReposFailedPartialFailure(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	barePath, _, _ := env.SetupRepo("backend", "api", "main")
	_ = os.RemoveAll(barePath)
	resetCommandState(t)
	env.Chdir()
	rootCmd.SetArgs([]string{"list", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected partial_failure")
	}
	e := output.Classify(err)
	if e.Code != output.CodePartialFailure {
		t.Fatalf("expected %s, got %s", output.CodePartialFailure, e.Code)
	}
}

func TestList_LocalOnlyUpstreamNull(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.CreateWorktree("backend", "api", "local-only-branch", "api-local-only")
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "list")
	for _, wt := range worktreesFromEnvelope(t, envelope) {
		if wt["name"] == "api-local-only" {
			if wt["upstream"] != nil {
				t.Fatalf("expected upstream null, got %#v", wt["upstream"])
			}
			return
		}
	}
	t.Fatal("api-local-only worktree not found")
}

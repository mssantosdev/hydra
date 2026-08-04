package cmd

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestStatus_NoConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)
	resetCommandState(t)
	resetCommandIO()
	env.Chdir()
	rootCmd.SetArgs([]string{"status"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	testutil.Contains(t, err.Error(), "no .hydra/config.yaml")
}

func TestStatus_EmptyProject(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "status")
	summary := envelope["data"].(map[string]any)["summary"].(map[string]any)
	if total, _ := summary["total"].(float64); int(total) != 0 {
		t.Fatalf("expected total 0, got %v", total)
	}
}

func TestStatus_CountsCleanAndDirty(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, _, mainPath := env.SetupRepo("backend", "api", "main", "develop")
	env.CreateWorktree("backend", "api", "develop", "api-develop")
	env.MakeWorktreeDirty(mainPath)
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "status")
	summary := envelope["data"].(map[string]any)["summary"].(map[string]any)
	if dirty, _ := summary["dirty"].(float64); int(dirty) != 1 {
		t.Fatalf("expected dirty 1, got %v", dirty)
	}
	if clean, _ := summary["clean"].(float64); int(clean) != 1 {
		t.Fatalf("expected clean 1, got %v", clean)
	}
}

func TestStatus_HasAllFlag(t *testing.T) {
	if statusCmd.Flags().Lookup("all") == nil {
		t.Fatal("expected --all flag")
	}
}

func TestStatus_AllReposFailedPartialFailure(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	barePath, _, _ := env.SetupRepo("backend", "api", "main")
	_ = os.RemoveAll(barePath)
	resetCommandState(t)
	env.Chdir()
	rootCmd.SetArgs([]string{"status", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected partial_failure")
	}
	if output.Classify(err).Code != output.CodePartialFailure {
		t.Fatalf("expected partial_failure")
	}
}

func TestStatus_JSONShape(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	resetCommandState(t)
	env.Chdir()
	envelope, _, _ := runCommandJSON(t, "status")
	if got, _ := envelope["schema"].(float64); int(got) != output.Schema {
		t.Fatalf("expected schema %d", output.Schema)
	}
	data := envelope["data"].(map[string]any)
	for _, key := range []string{"project", "root", "summary", "worktrees"} {
		if _, ok := data[key]; !ok {
			t.Fatalf("missing data.%s", key)
		}
	}
	_, _ = json.Marshal(data)
}

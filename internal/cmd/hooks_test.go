package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func writeHooksConfig(t *testing.T, env *testutil.TestEnv, remote string, hooks config.Hooks) {
	t.Helper()
	cfg := config.DefaultConfig("test")
	cfg.SetRepo("backend", "api", config.Repo{Remote: remote, DefaultBranch: "main"})
	cfg.Hooks = hooks
	env.SaveConfig(cfg)
}

func TestHooksRunPostAddInjectsEnvAndCwd(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, wt := env.SetupRepo("backend", "api", "main")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostAdd: []config.Hook{{Run: `echo "$HYDRA_BRANCH" > hook-branch.txt`}},
	})

	env.ChdirTo(filepath.Join("backend", "api"))
	projectRoot = env.RootDir
	projectConfigPath = filepath.Join(env.RootDir, ".hydra.yaml")
	cfg = env.LoadConfig()

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)
	cleanup := withJSONOutput(t)
	defer cleanup()

	rootCmd.SetArgs([]string{"hooks", "run", "post_add", "--worktree", "api"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hooks run: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(wt, "hook-branch.txt"))
	if err != nil {
		t.Fatalf("read hook output: %v", err)
	}
	if strings.TrimSpace(string(data)) != "main" {
		t.Fatalf("HYDRA_BRANCH = %q", data)
	}
}

func TestHooksRunRequiredFailure(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, _ := env.SetupRepo("backend", "api", "main")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostAdd: []config.Hook{{Run: "exit 7"}},
	})

	env.ChdirTo(filepath.Join("backend", "api"))
	projectRoot = env.RootDir
	projectConfigPath = filepath.Join(env.RootDir, ".hydra.yaml")
	cfg = env.LoadConfig()

	rootCmd.SetArgs([]string{"hooks", "run", "post_add", "--worktree", "api"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected hook_failed")
	}
	he := output.Classify(err)
	if he.Code != output.CodeHookFailed || he.Exit != 1 {
		t.Fatalf("code=%s exit=%d", he.Code, he.Exit)
	}
}

func TestHooksRunOptionalFailureWarns(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, _ := env.SetupRepo("backend", "api", "main")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostAdd: []config.Hook{{Run: "exit 9", Optional: true}},
	})

	env.ChdirTo(filepath.Join("backend", "api"))
	projectRoot = env.RootDir
	projectConfigPath = filepath.Join(env.RootDir, ".hydra.yaml")
	cfg = env.LoadConfig()

	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	cleanup := withJSONOutput(t)
	defer cleanup()

	rootCmd.SetArgs([]string{"hooks", "run", "post_add", "--worktree", "api"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("optional hook should not fail: %v", err)
	}
	var payload hooksRunPayload
	decodeJSONData(t, &stdout, &payload)
	if len(payload.Result.Warnings) == 0 {
		t.Fatalf("expected warnings in payload: %+v", payload)
	}
}

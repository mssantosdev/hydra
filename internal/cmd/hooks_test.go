package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/hooks"
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
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	// These fixtures carry hooks, so the trust gate applies: a real user must approve a
	// workspace before hydra will execute its manifest. Say so here rather than weakening
	// the gate for tests.
	trustCurrentWorkspace(t)

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
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	// These fixtures carry hooks, so the trust gate applies: a real user must approve a
	// workspace before hydra will execute its manifest. Say so here rather than weakening
	// the gate for tests.
	trustCurrentWorkspace(t)

	outputFlag = "text"
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = w.Close()
	}()
	rootCmd.SetArgs([]string{"hooks", "run", "post_add", "--worktree", "api"})
	_, runErr := Execute()
	_ = w.Close()
	var stderr bytes.Buffer
	if _, err := io.Copy(&stderr, r); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	err = runErr
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	outcome, code := output.EmittedVerdict()
	if outcome != output.OutcomeFailure || code != output.CodeHookFailed {
		t.Fatalf("verdict = %s/%s, want failure/hook_failed", outcome, code)
	}
	if output.ExitFor(code) != 1 {
		t.Fatalf("exit = %d, want 1", output.ExitFor(code))
	}

	got := stderr.String()
	if strings.HasPrefix(got, "Error:") {
		t.Fatalf("stderr must not use main's Error: prefix:\n%s", got)
	}
	if !strings.HasPrefix(got, "error: hook failed at hooks.post_add[0]\n") {
		t.Fatalf("stderr = %q, want error: hook failed at hooks.post_add[0]", got)
	}
	if !strings.Contains(got, "  exit: 7\n") {
		t.Fatalf("stderr missing exit line:\n%s", got)
	}
	if !strings.Contains(got, `  hint: fix the hook, then run "hydra hooks run post_add --worktree api"`) {
		t.Fatalf("stderr missing retry hint:\n%s", got)
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
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	// These fixtures carry hooks, so the trust gate applies: a real user must approve a
	// workspace before hydra will execute its manifest. Say so here rather than weakening
	// the gate for tests.
	trustCurrentWorkspace(t)

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

// The ENVIRONMENT block of `hydra hooks --help` and the `env` list in `hooks ls --output json`
// describe one fact. This pins them to the same source so the human-facing copy cannot fall
// behind a new variable.
func TestHooksHelpListsEveryInjectedVariable(t *testing.T) {
	help := hooksLongHelp()
	for _, key := range hooks.EnvKeys() {
		if !strings.Contains(help, key) {
			t.Errorf("hooks --help does not mention %s", key)
		}
	}
	found := regexp.MustCompile(`HYDRA_[A-Z_]+`).FindAllString(help, -1)
	unique := map[string]bool{}
	for _, f := range found {
		unique[f] = true
	}
	// HYDRA_* also appears in the prose above the block, so compare the set, not the count.
	for name := range unique {
		if !slices.Contains(hooks.EnvKeys(), name) {
			t.Errorf("hooks --help advertises %s, which the hook runner does not inject", name)
		}
	}
}

// post_topic_start fires ONCE PER TOPIC: for the invocation that started it, and not again when a
// second repository joins, nor on a converged re-run.
//
// Its guard must read a populated count. `payload.Created` is filled after the guard runs, which
// made the event configured, counted by `hooks ls`, documented, and unreachable. Once reachable, a
// per-invocation predicate is the opposite error: an integration that opens a work item on start
// would open a second one when the topic grows.
func TestStart_PostTopicStartFiresOncePerTopic(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.SetupRepo("frontend", "web", "main", "stage")
	// A third repository, so the last case can start a topic that really creates something: a
	// branch whose worktree already exists creates nothing, and then the event is right to stay
	// quiet for a reason that has nothing to do with the predicate under test.
	env.SetupRepo("infra", "tf", "main", "stage")
	env.Chdir()

	// The hooks are added to the manifest SetupRepo wrote rather than replacing it:
	// writeHooksConfig builds a single-repo config and this test needs two.
	marker := filepath.Join(env.RootDir, "started.txt")
	manifest := config.ManifestPath(env.RootDir)
	loaded, err := config.Load(manifest)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	loaded.Hooks = config.Hooks{
		PostTopicStart: []config.Hook{{Run: `printf '%s\n' "$HYDRA_TOPIC" >> ` + marker}},
	}
	if err := loaded.Save(manifest); err != nil {
		t.Fatalf("save manifest: %v", err)
	}

	// This manifest now carries a hook, so the trust gate applies. Load the config into the
	// command globals and approve it, exactly as a user must before hydra will run it.
	projectRoot = env.RootDir
	projectConfigPath = manifest
	cfg = loaded
	trustCurrentWorkspace(t)

	fires := func() int {
		data, err := os.ReadFile(marker)
		if err != nil {
			return 0
		}
		return len(strings.Fields(string(data)))
	}
	run := func(label string, args ...string) {
		resetCommandState(t)
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("%s: %v (code %q)", label, err, output.Classify(err).Code)
		}
	}

	run("start the topic", "start", "stage", "--repos", "api", "--topic", "7001")
	if got := fires(); got != 1 {
		t.Fatalf("starting a topic fired the event %d time(s), want 1", got)
	}

	run("extend the topic", "start", "stage", "--repos", "web", "--topic", "7001")
	if got := fires(); got != 1 {
		t.Errorf("a second repository joining fired it again (%d total): post_topic_start is once "+
			"per TOPIC, so an integration must not create a second work item", got)
	}

	run("converged re-run", "start", "stage", "--repos", "api", "--topic", "7001")
	if got := fires(); got != 1 {
		t.Errorf("a re-run that created nothing fired it (%d total)", got)
	}

	run("a different topic", "start", "stage", "--repos", "tf", "--topic", "7002")
	if got := fires(); got != 2 {
		t.Errorf("a genuinely new topic did not announce itself (%d fires, want 2)", got)
	}
}

func TestHooksLsShowsHookNames(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, _ := env.SetupRepo("backend", "api", "main")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostAdd: []config.Hook{{Name: "install dependencies", Run: "true"}},
	})

	env.ChdirTo(filepath.Join("backend", "api"))
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	// These fixtures carry hooks, so the trust gate applies: a real user must approve a
	// workspace before hydra will execute its manifest. Say so here rather than weakening
	// the gate for tests.
	trustCurrentWorkspace(t)

	var stdout bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stdout)

	rootCmd.SetArgs([]string{"hooks", "ls"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("hooks ls: %v", err)
	}
	if !strings.Contains(stdout.String(), "install dependencies") {
		t.Fatalf("hooks ls output = %q, want hook name", stdout.String())
	}
	if strings.Contains(stdout.String(), "true") {
		t.Fatalf("hooks ls must not echo hook command: %q", stdout.String())
	}
}

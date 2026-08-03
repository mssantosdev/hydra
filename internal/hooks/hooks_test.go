package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

func testContext(worktree string) Context {
	return Context{
		Event:        "post_add",
		Project:      "demo",
		ProjectRoot:  filepath.Dir(filepath.Dir(worktree)),
		Group:        "backend",
		Repo:         "api",
		Branch:       "feat/login",
		WorktreePath: worktree,
		BarePath:     "/ws/.bare/api.git",
	}
}

// A hook must see the documented environment and run in the documented directory,
// because that is the whole interface a user writes hooks against.
func TestRunInjectsEnvironmentAndCwd(t *testing.T) {
	worktree := t.TempDir()
	var log strings.Builder

	hook := config.Hook{Run: `printf '%s|%s|%s|%s|%s\n' "$HYDRA_EVENT" "$HYDRA_PROJECT" "$HYDRA_GROUP" "$HYDRA_REPO" "$HYDRA_BRANCH" > env.txt && pwd > cwd.txt`}

	result, err := Run([]config.Hook{hook}, testContext(worktree), worktree, &log)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Ran != 1 {
		t.Errorf("Ran = %d, want 1", result.Ran)
	}

	env, err := os.ReadFile(filepath.Join(worktree, "env.txt"))
	if err != nil {
		t.Fatalf("hook did not run in the worktree: %v", err)
	}
	if got := strings.TrimSpace(string(env)); got != "post_add|demo|backend|api|feat/login" {
		t.Errorf("injected env = %q, want post_add|demo|backend|api|feat/login", got)
	}

	cwd, err := os.ReadFile(filepath.Join(worktree, "cwd.txt"))
	if err != nil {
		t.Fatalf("read cwd.txt: %v", err)
	}
	wantCwd, _ := filepath.EvalSymlinks(worktree)
	gotCwd, _ := filepath.EvalSymlinks(strings.TrimSpace(string(cwd)))
	if gotCwd != wantCwd {
		t.Errorf("hook cwd = %q, want %q", gotCwd, wantCwd)
	}
}

func TestRunUsesShellSoOperatorsWork(t *testing.T) {
	worktree := t.TempDir()
	var log strings.Builder

	hook := config.Hook{Run: `echo one > a.txt && echo two >> a.txt`}
	if _, err := Run([]config.Hook{hook}, testContext(worktree), worktree, &log); err != nil {
		t.Fatalf("Run: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(worktree, "a.txt"))
	if err != nil {
		t.Fatalf("read a.txt: %v", err)
	}
	if got := strings.Fields(string(content)); len(got) != 2 {
		t.Errorf("shell operators did not run: %q", content)
	}
}

// A required hook failure stops the chain and reports hook_failed. It must NOT be
// downgraded, and later hooks must not run.
func TestRequiredHookFailureStopsChain(t *testing.T) {
	worktree := t.TempDir()
	var log strings.Builder

	hooks := []config.Hook{
		{Run: "exit 3"},
		{Run: "touch should-not-exist.txt"},
	}

	result, err := Run(hooks, testContext(worktree), worktree, &log)
	if err == nil {
		t.Fatal("a required hook failure must return an error")
	}

	var e *output.Error
	if !errors.As(err, &e) {
		t.Fatalf("error = %T, want *output.Error", err)
	}
	if e.Code != output.CodeHookFailed {
		t.Errorf("code = %q, want %q", e.Code, output.CodeHookFailed)
	}
	if e.Exit != 1 {
		t.Errorf("exit = %d, want 1", e.Exit)
	}
	if e.Details["event"] != "post_add" || e.Details["index"] != 1 {
		t.Errorf("details = %v, want event post_add at index 1", e.Details)
	}
	if result.Ran != 1 {
		t.Errorf("Ran = %d, want 1: the chain must stop at the failure", result.Ran)
	}
	if _, statErr := os.Stat(filepath.Join(worktree, "should-not-exist.txt")); statErr == nil {
		t.Error("a hook after a required failure must not run")
	}
}

// An optional hook failure is a warning, and the chain continues. This is what
// lets `bun install` failing not block a correctly-created worktree.
func TestOptionalHookFailureWarnsAndContinues(t *testing.T) {
	worktree := t.TempDir()
	var log strings.Builder

	hooks := []config.Hook{
		{Run: "exit 7", Optional: true},
		{Run: "touch ran.txt"},
	}

	result, err := Run(hooks, testContext(worktree), worktree, &log)
	if err != nil {
		t.Fatalf("an optional hook failure must not fail the run: %v", err)
	}
	if len(result.Warnings) != 1 {
		t.Fatalf("Warnings = %v, want exactly one", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "optional") {
		t.Errorf("warning should name the hook as optional: %q", result.Warnings[0])
	}
	if result.Ran != 2 {
		t.Errorf("Ran = %d, want 2: the chain must continue", result.Ran)
	}
	if _, statErr := os.Stat(filepath.Join(worktree, "ran.txt")); statErr != nil {
		t.Error("the hook after an optional failure must run")
	}
	if !strings.Contains(log.String(), "warning:") {
		t.Errorf("the warning must be streamed to the writer, got %q", log.String())
	}
}

func TestRunEmptyChainIsANoop(t *testing.T) {
	var log strings.Builder

	result, err := Run(nil, testContext(t.TempDir()), t.TempDir(), &log)
	if err != nil {
		t.Fatalf("Run(nil): %v", err)
	}
	if result.Ran != 0 || len(result.Warnings) != 0 {
		t.Errorf("result = %+v, want zero", result)
	}
	if log.String() != "" {
		t.Errorf("an empty chain must produce no output, got %q", log.String())
	}
}

func TestEnvListsEveryDocumentedVariable(t *testing.T) {
	env := testContext("/ws/backend/api").Env()

	want := []string{
		"HYDRA_EVENT", "HYDRA_PROJECT", "HYDRA_PROJECT_ROOT", "HYDRA_GROUP",
		"HYDRA_REPO", "HYDRA_BRANCH", "HYDRA_WORKTREE_PATH", "HYDRA_BARE_PATH",
	}
	if len(env) != len(want) {
		t.Fatalf("Env() = %v, want %d variables", env, len(want))
	}
	for i, key := range want {
		if !strings.HasPrefix(env[i], key+"=") {
			t.Errorf("Env()[%d] = %q, want it to set %s", i, env[i], key)
		}
	}
}

package cmd

import (
	"runtime"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

func runEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("run tests use POSIX utilities")
	}
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("backend", "web", "main")
	env.Chdir()
	return env
}

func runPayload(t *testing.T, args ...string) runJSON {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"run"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("run %v: %v", args, err)
	}
	var payload runJSON
	decodeJSONData(t, stdout, &payload)
	return payload
}

// A bare handle addresses exactly one worktree, the same way it does everywhere else.
func TestRun_HandleTargetsOneWorktree(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	payload := runPayload(t, "backend/api", "--output", "json", "--", "true")
	if payload.Total != 1 {
		t.Fatalf("total = %d, want 1", payload.Total)
	}
	if payload.Results[0].Repo != "api" {
		t.Errorf("ran in %q, want api", payload.Results[0].Repo)
	}
}

// An ambiguous handle is an error, never a silent first match.
func TestRun_AmbiguousHandleIsRefused(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "main", "--output", "json", "--", "true"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a branch name shared by two repos must not resolve")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeNameConflict {
		t.Errorf("code = %q, want %q", code, output.CodeWorktreeNameConflict)
	}
}

// A selector runs across every match.
func TestRun_SelectorTargetsEveryMatch(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	payload := runPayload(t, "--group", "backend", "--output", "json", "--", "true")
	if payload.Total != 2 {
		t.Fatalf("total = %d, want both backend repos", payload.Total)
	}
}

// The command is an argv array executed WITHOUT a shell, so a metacharacter is a
// literal argument and cannot become a second command.
func TestRun_NoShellInterpretation(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	marker := env.RootDir + "/should-not-exist"

	// If this were passed through a shell, the `touch` after `;` would run.
	payload := runPayload(t, "backend/api", "--output", "json", "--",
		"echo", "hello; touch "+marker)

	if payload.Failed != 0 {
		t.Fatalf("echo should succeed, got %+v", payload.Results)
	}
	if env.FileExists(marker) {
		t.Fatal("the argument was interpreted by a shell; hydra must never wrap it in sh -c")
	}
}

// A shell is available when asked for explicitly, which is the documented escape.
func TestRun_ExplicitShellWorks(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	marker := env.RootDir + "/made-by-shell"

	payload := runPayload(t, "backend/api", "--output", "json", "--",
		"sh", "-c", "touch "+marker)

	if payload.Failed != 0 {
		t.Fatalf("explicit sh -c must work, got %+v", payload.Results)
	}
	if !env.FileExists(marker) {
		t.Error("sh -c did not run")
	}
}

// The documented environment reaches the command.
func TestRun_InjectsHydraEnvironment(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	out := env.RootDir + "/env-out"

	payload := runPayload(t, "backend/api", "--output", "json", "--",
		"sh", "-c", `printf '%s|%s|%s|%s' "$HYDRA_REPO" "$HYDRA_GROUP" "$HYDRA_BRANCH" "$HYDRA_TOPIC" > `+out)
	if payload.Failed != 0 {
		t.Fatalf("run failed: %+v", payload.Results)
	}

	got := env.ReadFile(t, out)
	if want := "api|backend|main|2072958"; got != want {
		t.Errorf("environment = %q, want %q", got, want)
	}
}

// HYDRA_PATH is the worktree, and it is also the process's working directory.
func TestRun_RunsInsideTheWorktree(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	out := env.RootDir + "/pwd-out"

	payload := runPayload(t, "backend/api", "--output", "json", "--",
		"sh", "-c", `printf '%s' "$PWD" > `+out)
	if payload.Failed != 0 {
		t.Fatalf("run failed: %+v", payload.Results)
	}

	want := env.GetWorktreePath("backend", "api")
	if got := env.ReadFile(t, out); got != want {
		t.Errorf("cwd = %q, want the worktree %q", got, want)
	}
}

// A non-zero exit in some worktrees is partial_failure, and the failures are named.
func TestRun_PartialFailureNamesTheFailures(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "--group", "backend", "--output", "json", "--",
		"sh", "-c", `test "$HYDRA_REPO" = api`})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a failing command must be reported")
	}

	classified := output.Classify(err)
	if classified.Code != output.CodePartialFailure {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodePartialFailure)
	}
	failed, ok := classified.Details["failed"].([]map[string]any)
	if !ok || len(failed) != 1 {
		t.Fatalf("details.failed must name the one failure, got %#v", classified.Details["failed"])
	}
	if failed[0]["repo"] != "web" {
		t.Errorf("failed repo = %v, want web", failed[0]["repo"])
	}
}

// Failing everywhere is a plain failure, not a partial one.
func TestRun_TotalFailureIsNotPartial(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "--group", "backend", "--output", "json", "--", "false"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a command failing everywhere must be reported")
	}
	if code := output.Classify(err).Code; code != output.CodeGitFailed {
		t.Errorf("code = %q, want %q", code, output.CodeGitFailed)
	}
}

// No command is needs_input, not a usage dump: an agent needs the code and the flag.
func TestRun_WithoutACommandAsksForOne(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "--group", "backend", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("run without a command must fail")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
	}
	if missing, _ := classified.Details["missing"].([]string); len(missing) != 1 {
		t.Errorf("details.missing = %#v, want the command placeholder", classified.Details["missing"])
	}
}

// A per-invocation timeout is enforced, and reported as a timeout rather than as an
// ordinary non-zero exit.
func TestRun_TimeoutIsEnforcedAndReported(t *testing.T) {
	resetCommandState(t)
	runEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"run", "backend/api", "--timeout", "100ms",
		"--output", "json", "--", "sleep", "5"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a command exceeding the timeout must fail")
	}

	var payload runJSON
	decodeJSONData(t, stdout, &payload)
	if payload.TimedOut != 1 {
		t.Errorf("timed_out = %d, want 1", payload.TimedOut)
	}
	if payload.Results[0].ExitCode != -1 {
		t.Errorf("exit_code = %d, want -1 for a killed process", payload.Results[0].ExitCode)
	}
}

// A selector matching nothing is an error rather than a silent success, so a typo does
// not read as "ran everywhere, all fine".
func TestRun_EmptySelectionIsAnError(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	if err := topic.Open(env.RootDir).Attach("real", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "--filter", "branch:nothing-matches-this",
		"--output", "json", "--", "true"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("a selector matching nothing must not report success")
	}
}

// Membership is reported per result, so a caller knows which topic each run belonged
// to without a second call.
func TestRun_ReportsTopicPerResult(t *testing.T) {
	resetCommandState(t)
	env := runEnv(t)
	if err := topic.Open(env.RootDir).Attach("2072958", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	payload := runPayload(t, "--topic", "2072958", "--output", "json", "--", "true")
	if payload.Total != 1 {
		t.Fatalf("total = %d, want 1", payload.Total)
	}
	if payload.Results[0].Topic != "2072958" {
		t.Errorf("topic = %q, want 2072958", payload.Results[0].Topic)
	}
}

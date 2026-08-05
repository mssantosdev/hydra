package cmd

import (
	"os"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func whereData(t *testing.T) whereJSON {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"where", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("where: %v", err)
	}
	var payload whereJSON
	decodeJSONData(t, stdout, &payload)
	return payload
}

// Inside a worktree, where answers with the identity every other command takes as input.
func TestWhere_ReportsTheWorktreeItIsIn(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.ChdirTo("backend/api")

	got := whereData(t)
	if !got.InProject {
		t.Fatal("in_project must be true inside a workspace")
	}
	if !got.IsWorktree {
		t.Fatal("is_worktree must be true inside a worktree")
	}
	if got.Repo != "api" || got.Group != "backend" {
		t.Errorf("got %s/%s, want backend/api", got.Group, got.Repo)
	}
	if got.Worktree == "" || got.Branch == "" {
		t.Errorf("worktree and branch must be reported, got %+v", got)
	}
}

// At the workspace root the answer is "in the project, not in a worktree" — the two are
// different positions and a caller has to be able to tell them apart.
func TestWhere_DistinguishesRootFromWorktree(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	got := whereData(t)
	if !got.InProject {
		t.Error("in_project must be true at the workspace root")
	}
	if got.IsWorktree {
		t.Error("the workspace root is not a worktree")
	}
}

// Outside any workspace this must SUCCEED with in_project=false. That is the answer a
// caller dropped into an unknown directory needs; failing would make "no workspace"
// indistinguishable from "broken workspace".
func TestWhere_OutsideAWorkspaceIsAnAnswerNotAnError(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	// Somewhere with no workspace above it.
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got := whereData(t)
	if got.InProject {
		t.Error("in_project must be false outside a workspace")
	}
	if got.Cwd == "" {
		t.Error("cwd must always be reported")
	}
}

// A mistyped subcommand is the first thing a zero-context caller hits. It must be
// machine-readable: cobra buried its suggestion in English prose and classified the whole
// thing as `internal`, which is neither actionable nor accurate.
func TestExecute_UnknownCommandIsActionable(t *testing.T) {
	resetCommandState(t)

	err := classifyUnknownCommand(errUnknownCommand())
	if err == nil {
		t.Fatal("expected an error")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeUnknownCommand {
		t.Errorf("code = %q, want %q", classified.Code, output.CodeUnknownCommand)
	}
	if _, ok := classified.Details["available"]; !ok {
		t.Error("details.available must list the real commands")
	}
	if got, ok := classified.Details["did_you_mean"]; !ok {
		t.Error("details.did_you_mean must carry cobra's suggestion as data")
	} else if names, _ := got.([]string); len(names) == 0 || names[0] != "list" {
		t.Errorf("did_you_mean = %v, want [list]", got)
	}
	if len(classified.Next) == 0 {
		t.Fatal("the recovery invocation must ride the error itself")
	}
	argv := classified.Next[0].Argv
	if len(argv) < 2 || argv[1] != "commands" {
		t.Errorf("next argv = %v, want it to point at hydra commands", argv)
	}
}

// errUnknownCommand reproduces cobra's message shape, so the parsing is tested against the
// real text rather than an invented one.
func errUnknownCommand() error {
	return &stubError{"unknown command \"lst\" for \"hydra\"\n\nDid you mean this?\n\tlist\n"}
}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }

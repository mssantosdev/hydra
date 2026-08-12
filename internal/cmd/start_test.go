package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
)

// startEnv builds a workspace with three repositories, so "which repos" is a real
// question rather than one with a single possible answer.
func startEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("backend", "web", "main")
	env.SetupRepo("backend", "billing", "main")
	env.Chdir()
	return env
}

func runStartCmd(t *testing.T, args ...string) error {
	t.Helper()
	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs(append([]string{"start"}, args...))
	return rootCmd.Execute()
}

func startPayload(t *testing.T, args ...string) startJSON {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"start"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("start %v: %v", args, err)
	}
	var payload startJSON
	decodeJSONData(t, stdout, &payload)
	return payload
}

// The defaults table's happy path: an explicit branch across two repos, recorded.
func TestStart_ExplicitBranchAcrossRepos(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)

	payload := startPayload(t, "marcus/feat-login", "--repos", "api,web",
		"--topic", "2072958", "--output", "json")

	if payload.Branch != "marcus/feat-login" {
		t.Errorf("branch = %q, want the positional value", payload.Branch)
	}
	if payload.BranchSource != "positional" {
		t.Errorf("branch_source = %q, want positional", payload.BranchSource)
	}
	if len(payload.Created) != 2 {
		t.Fatalf("created %d worktrees, want 2", len(payload.Created))
	}

	got, ok, err := topic.Open(env.RootDir).Get("2072958")
	if err != nil || !ok {
		t.Fatalf("the topic must be recorded (ok=%v err=%v)", ok, err)
	}
	if len(got.Members) != 2 {
		t.Errorf("members = %+v, want 2", got.Members)
	}
}

// Extending a topic to another repository needs NO flags beyond the selector: the
// branch comes from the members, which is why level 3 outranks the pattern.
func TestStart_ExtendsTopicUsingTheUnanimousBranch(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	if err := runStartCmd(t, "marcus/feat-login", "--repos", "api", "--topic", "2072958", "--output", "json"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	payload := startPayload(t, "--topic", "2072958", "--repos", "billing", "--output", "json")

	if payload.Branch != "marcus/feat-login" {
		t.Errorf("branch = %q, want the members' branch", payload.Branch)
	}
	if payload.BranchSource != "topic_members" {
		t.Errorf("branch_source = %q, want topic_members", payload.BranchSource)
	}
	if len(payload.Created) != 1 || payload.Created[0].Repo != "billing" {
		t.Errorf("created = %+v, want just billing", payload.Created)
	}

	got, _, _ := topic.Open(env.RootDir).Get("2072958")
	if len(got.Members) != 2 {
		t.Errorf("members = %+v, want api and billing", got.Members)
	}
}

// With no selector, a topic's existing members supply the repositories too, so
// "hydra start --topic X" alone re-converges the whole topic.
func TestStart_NoSelectorUsesTheTopicsMembers(t *testing.T) {
	resetCommandState(t)
	startEnv(t)
	if err := runStartCmd(t, "marcus/feat-login", "--repos", "api,web", "--topic", "2072958", "--output", "json"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	payload := startPayload(t, "--topic", "2072958", "--output", "json")

	// Everything is already correct, so a converged run skips rather than fails.
	if len(payload.Skipped) != 2 {
		t.Errorf("skipped = %d, want both members", len(payload.Skipped))
	}
	if len(payload.Failed) != 0 {
		t.Errorf("failed = %+v, want none", payload.Failed)
	}
}

// Re-running an identical start is a no-op that exits 0. This is the same
// convergence property clone needed, and the reason it lives in the engine.
func TestStart_IsConvergent(t *testing.T) {
	resetCommandState(t)
	startEnv(t)
	args := []string{"marcus/feat-login", "--repos", "api,web", "--output", "json"}

	if err := runStartCmd(t, args...); err != nil {
		t.Fatalf("first start: %v", err)
	}
	payload := startPayload(t, args...)

	if len(payload.Created) != 0 {
		t.Errorf("created = %+v, want nothing on a converged run", payload.Created)
	}
	if len(payload.Skipped) != 2 {
		t.Errorf("skipped = %d, want 2", len(payload.Skipped))
	}
}

// start must NEVER silently mean "every repository": a worktree in every repo of a
// workspace is not a plausible accident to allow.
func TestStart_WithoutASelectorAsksWhichRepos(t *testing.T) {
	resetCommandState(t)
	startEnv(t)

	for _, args := range [][]string{
		{"--output", "json"},
		{"marcus/feat-login", "--output", "json"},
		{"--topic", "brand-new", "--output", "json"},
	} {
		t.Run(args[0], func(t *testing.T) {
			err := runStartCmd(t, args...)
			if err == nil {
				t.Fatal("start without a selector must ask")
			}
			classified := output.Classify(err)
			if classified.Code != output.CodeNeedsInput {
				t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
			}
			// one_of, not missing: any of the three flags satisfies this.
			oneOf, ok := classified.Details["one_of"].([]string)
			if !ok || len(oneOf) != 3 {
				t.Fatalf("details.one_of = %#v, want the three selector flags", classified.Details["one_of"])
			}
		})
	}
}

// No branch, no members, no pattern is needs_input naming --branch. An earlier design
// fell back to the topic id verbatim, which would create a branch named "2072958".
func TestStart_NoBranchAndNoPatternAsksForBranch(t *testing.T) {
	resetCommandState(t)
	startEnv(t)

	err := runStartCmd(t, "--repos", "api", "--topic", "2072958", "--output", "json")
	if err == nil {
		t.Fatal("start with nothing to derive a branch from must ask")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
	}
	missing, ok := classified.Details["missing"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "--branch" {
		t.Fatalf("details.missing = %#v, want [--branch]", classified.Details["missing"])
	}

	// Nothing may have been created, and in particular no branch named after the id.
	if _, ok, _ := topic.Open(projectRoot).Get("2072958"); ok {
		t.Error("a refused start must not record a topic")
	}
}

// Members on different branches: hydra refuses rather than choosing a third name.
func TestStart_DisagreeingMembersAskForBranch(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	store := topic.Open(env.RootDir)
	if err := runStartCmd(t, "marcus/feat-login", "--repos", "api", "--topic", "2072958", "--output", "json"); err != nil {
		t.Fatalf("seed api: %v", err)
	}
	// A second member on a DIFFERENT branch, which is legitimate.
	if err := runStartCmd(t, "hotfix/login-npe", "--repos", "web", "--topic", "2072958", "--output", "json"); err != nil {
		t.Fatalf("seed web: %v", err)
	}
	if got, _, _ := store.Get("2072958"); len(got.Members) != 2 {
		t.Fatalf("precondition: want 2 members, got %+v", got.Members)
	}

	err := runStartCmd(t, "--topic", "2072958", "--repos", "billing", "--output", "json")
	if err == nil {
		t.Fatal("disagreeing members leave no branch to extend")
	}
	if code := output.Classify(err).Code; code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", code, output.CodeNeedsInput)
	}
}

// withPattern configures defaults.branch_pattern on the workspace.
func withPattern(t *testing.T, env *testutil.TestEnv, pattern string) {
	t.Helper()
	c := env.LoadConfig()
	c.Defaults.BranchPattern = pattern
	env.SaveConfig(c)
}

func TestStart_GeneratesFromPattern(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	withPattern(t, env, "{user}/{kind}-{slug}")

	payload := startPayload(t, "--topic", "2072958", "--repos", "api",
		"--slug", "Login Page", "--kind", "feat", "--user", "marcus", "--output", "json")

	if payload.Branch != "marcus/feat-login-page" {
		t.Errorf("branch = %q, want marcus/feat-login-page (slug slugified)", payload.Branch)
	}
	if payload.BranchSource != "branch_pattern" {
		t.Errorf("branch_source = %q, want branch_pattern", payload.BranchSource)
	}
}

// THE documented surprise: a positional argument is a literal branch name even when
// a pattern is configured and the value looks like a topic id.
func TestStart_PositionalBeatsThePattern(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	withPattern(t, env, "{user}/{kind}-{slug}")

	payload := startPayload(t, "2072958", "--repos", "api",
		"--slug", "login", "--kind", "feat", "--user", "marcus", "--output", "json")

	if payload.Branch != "2072958" {
		t.Errorf("branch = %q, want the literal positional value", payload.Branch)
	}
	if payload.BranchSource != "positional" {
		t.Errorf("branch_source = %q, want positional", payload.BranchSource)
	}
}

// A pattern that needs a value nobody supplied names the exact flag.
func TestStart_PatternMissingValueNamesItsFlag(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	withPattern(t, env, "{user}/{kind}-{slug}")

	err := runStartCmd(t, "--topic", "2072958", "--repos", "api",
		"--kind", "feat", "--user", "marcus", "--output", "json")
	if err == nil {
		t.Fatal("a pattern missing {slug} must ask for it")
	}
	missing, ok := output.Classify(err).Details["missing"].([]string)
	if !ok || len(missing) != 1 || missing[0] != "--slug" {
		t.Fatalf("details.missing = %#v, want [--slug]", output.Classify(err).Details["missing"])
	}
}

// A repo-level branch_pattern overrides the project default.
func TestStart_RepoPatternOverridesTheDefault(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	c := env.LoadConfig()
	c.Defaults.BranchPattern = "{user}/{slug}"
	ref, ok := c.FindRepo("api")
	if !ok {
		t.Fatal("precondition: api must be registered")
	}
	repo := ref.Repo
	repo.BranchPattern = "custom/{slug}"
	c.SetRepo("backend", "api", repo)
	env.SaveConfig(c)

	payload := startPayload(t, "--topic", "2072958", "--repos", "api",
		"--slug", "login", "--user", "marcus", "--output", "json")

	if payload.Branch != "custom/login" {
		t.Errorf("branch = %q, want the repo-level pattern to win", payload.Branch)
	}
}

// Without --topic, start behaves exactly like add: the worktrees are unassigned, and
// unassigned is a permanent first-class state.
func TestStart_WithoutTopicLeavesWorktreesUnassigned(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)

	payload := startPayload(t, "feat/spike", "--repos", "api", "--output", "json")
	if payload.Topic != nil {
		t.Errorf("topic = %v, want null", *payload.Topic)
	}

	topics, err := topic.Open(env.RootDir).List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(topics) != 0 {
		t.Errorf("start without --topic must record nothing, got %+v", topics)
	}
}

// --no-assign creates the worktrees without recording membership, for the case where
// the topic id is only meant to drive the branch pattern.
func TestStart_NoAssignSkipsRecording(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	withPattern(t, env, "{topic}-{slug}")

	payload := startPayload(t, "--topic", "2072958", "--repos", "api",
		"--slug", "login", "--no-assign", "--output", "json")

	if payload.Branch != "2072958-login" {
		t.Errorf("branch = %q, want the pattern to still use {topic}", payload.Branch)
	}
	if topics, _ := topic.Open(env.RootDir).List(); len(topics) != 0 {
		t.Errorf("--no-assign must record nothing, got %+v", topics)
	}
}

func TestStart_DryRunChangesNothing(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)

	payload := startPayload(t, "marcus/feat-login", "--repos", "api,web",
		"--topic", "2072958", "--dry-run", "--output", "json")

	if !payload.DryRun {
		t.Error("dry_run must be reported")
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-marcus-feat-login")) {
		t.Error("--dry-run created a worktree")
	}
	if topics, _ := topic.Open(env.RootDir).List(); len(topics) != 0 {
		t.Errorf("--dry-run recorded a topic: %+v", topics)
	}
}

// An unknown repo in --repos is refused with the known set, not silently skipped.
func TestStart_UnknownRepoIsRefused(t *testing.T) {
	resetCommandState(t)
	startEnv(t)

	err := runStartCmd(t, "feat/x", "--repos", "nope", "--output", "json")
	if err == nil {
		t.Fatal("an unknown repo must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeRepoUnknown {
		t.Errorf("code = %q, want %q", code, output.CodeRepoUnknown)
	}
}

// --group targets a whole group without naming each repo.
func TestStart_GroupSelector(t *testing.T) {
	resetCommandState(t)
	env := startEnv(t)
	// A repo in a second group must NOT be targeted.
	env.SetupRepo("frontend", "ui", "main")
	resetCommandState(t)
	c := env.LoadConfig()
	cfg = c

	payload := startPayload(t, "feat/grouped", "--group", "backend", "--output", "json")
	for _, target := range payload.Created {
		if target.Group != "backend" {
			t.Errorf("created %s/%s, want only backend", target.Group, target.Repo)
		}
	}
	if len(payload.Created) != 3 {
		t.Errorf("created %d, want the three backend repos", len(payload.Created))
	}
}

// The envelope explains WHICH name was chosen and why, so a caller never re-derives
// the precedence chain to understand the result.
func TestStart_EnvelopeExplainsTheBranchSource(t *testing.T) {
	resetCommandState(t)
	startEnv(t)

	payload := startPayload(t, "feat/x", "--repos", "api", "--output", "json")
	if payload.BranchSource == "" {
		t.Error("branch_source must always be reported")
	}
}

// A next suggestion appears only when there is a topic to inspect, and hydra never
// acts on it.
func TestStart_SuggestsStatusForATopic(t *testing.T) {
	resetCommandState(t)
	startEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"start", "feat/x", "--repos", "api", "--topic", "2072958", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("start: %v", err)
	}

	var envelope struct {
		Next []output.Next `json:"next"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Next) != 1 {
		t.Fatalf("next = %+v, want one suggestion", envelope.Next)
	}
	argv := envelope.Next[0].Argv
	if len(argv) < 2 || argv[0] != "hydra" || argv[1] != "status" {
		t.Errorf("argv = %v, want a hydra status invocation", argv)
	}
}

// startEnv's config helper is used by the repo-override test; keep the config import
// honest by asserting the type it produces.
var _ = config.Defaults{}

// --dry-run predicts, so it must run the same convergence check the real path does. Reporting
// "would_create" for every target promised to create worktrees that already existed, which made a
// preflight before a real run always look like work and never like a no-op.
func TestStart_DryRunReportsSkippedForAWorktreeThatExists(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	rootCmd.SetArgs([]string{"start", "stage", "--repos", "api"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("create the worktree: %v", err)
	}

	resetCommandState(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"start", "stage", "--repos", "api", "--dry-run", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	var envelope struct {
		Data struct {
			Created []struct{ Disposition string } `json:"created"`
			Skipped []struct{ Disposition string } `json:"skipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v\n%s", err, out.String())
	}
	if len(envelope.Data.Created) != 0 {
		t.Errorf("dry run promised to create a worktree that exists: %+v", envelope.Data.Created)
	}
	if len(envelope.Data.Skipped) != 1 || envelope.Data.Skipped[0].Disposition != "skipped" {
		t.Errorf("skipped = %+v, want one entry reported skipped", envelope.Data.Skipped)
	}
}

// A predicted failure must exit non-zero, so a preflight cannot report success for a run that will
// not succeed.
func TestStart_DryRunFailsWhenTheTargetDirectoryIsOccupied(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	blocked := filepath.Join(env.RootDir, "backend", "api-feat-blocked")
	if err := os.MkdirAll(blocked, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "f"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	rootCmd.SetArgs([]string{"start", "feat/blocked", "--repos", "api", "--dry-run"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("dry run reported success for a target a real run cannot create")
	}
}

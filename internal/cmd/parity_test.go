package cmd

import (
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// Every prompt needs a flag equivalent, and a prompt that cannot be shown must return
// needs_input naming the missing argument — never a code that misdescribes the
// problem, and never silence.
//
// worktree_unknown was wrong for these: nothing is unknown, nothing was named.
func TestParity_NonInteractivePromptsAskForInput(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		missing string
	}{
		{name: "switch with no worktree", args: []string{"switch"}, missing: "<worktree>"},
		{name: "remove with no worktree", args: []string{"remove"}, missing: "<worktree>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetCommandState(t)
			env := testutil.NewTestEnv(t)
			env.InitConfig()
			env.SetupRepo("backend", "api", "main")
			env.Chdir()

			resetCommandState(t)
			resetCommandIO()
			rootCmd.SetArgs(append(tc.args, "--output", "json"))
			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("%v must not succeed without an argument", tc.args)
			}

			classified := output.Classify(err)
			if classified.Code != output.CodeNeedsInput {
				t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
			}
			missing, ok := classified.Details["missing"].([]string)
			if !ok || len(missing) != 1 || missing[0] != tc.missing {
				t.Errorf("details.missing = %#v, want [%s]", classified.Details["missing"], tc.missing)
			}
			// The valid values travel with the error so a caller can recover from it
			// alone rather than making a second call.
			if _, ok := classified.Details["available"]; !ok {
				t.Error("details.available must list the valid worktrees")
			}
		})
	}
}

// clone without a URL is a missing value, not an internal error.
func TestParity_CloneWithoutURLAsksForIt(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"repo", "add", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("clone without a URL must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeNeedsInput {
		t.Errorf("code = %q, want %q", code, output.CodeNeedsInput)
	}
}

// TestSyncDirty_WithoutAPolicyAsksForOne: non-interactive sync with a dirty worktree and
// no --dirty returns needs_input naming --dirty instead of skipping without explanation.
func TestSyncDirty_WithoutAPolicyAsksForOne(t *testing.T) {
	resetCommandState(t)
	env := syncDirtyEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("a dirty worktree with no policy must ask for one")
	}

	classified := output.Classify(err)
	if classified.Code != output.CodeNeedsInput {
		t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
	}
	missing, _ := classified.Details["missing"].([]string)
	if len(missing) != 1 || missing[0] != "--dirty" {
		t.Errorf("details.missing = %#v, want [--dirty]", classified.Details["missing"])
	}
	oneOf, _ := classified.Details["one_of"].([]string)
	if len(oneOf) != 3 {
		t.Errorf("details.one_of = %#v, want the three policies", classified.Details["one_of"])
	}
	// The offending worktree is named, not just counted.
	worktrees, _ := classified.Details["worktrees"].([]string)
	if len(worktrees) != 1 || !strings.Contains(worktrees[0], "api") {
		t.Errorf("details.worktrees = %#v, want the dirty worktree", classified.Details["worktrees"])
	}
	_ = env
}

// syncDirtyEnv builds a workspace with one worktree that is both behind and dirty.
func syncDirtyEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, remote, wt := env.SetupRepo("backend", "api", "main")
	env.Chdir()
	env.CommitToRemote(remote, "main", "upstream work")
	env.MakeWorktreeDirty(wt)
	return env
}

func TestSyncDirty_SkipLeavesTheWorktreeAlone(t *testing.T) {
	resetCommandState(t)
	env := syncDirtyEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--dirty", "skip", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--dirty skip: %v", err)
	}

	var payload syncJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Summary.Pulled != 0 {
		t.Errorf("pulled = %d, want 0 with --dirty skip", payload.Summary.Pulled)
	}
	_ = env
}

func TestSyncDirty_StashPullsAndRestores(t *testing.T) {
	resetCommandState(t)
	env := syncDirtyEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--dirty", "stash", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--dirty stash: %v", err)
	}

	var payload syncJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Summary.Pulled != 1 {
		t.Fatalf("pulled = %d, want 1", payload.Summary.Pulled)
	}
	// The uncommitted work must come back; a stash that does not restore is data loss.
	if !env.FileExists(env.GetWorktreePath("backend", "api") + "/dirty-file.txt") {
		t.Error("the stashed change was not restored")
	}
}

func TestSyncDirty_InvalidPolicyIsRefused(t *testing.T) {
	resetCommandState(t)
	syncDirtyEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--dirty", "nope", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an invalid --dirty value must be refused")
	}
	classified := output.Classify(err)
	valid, ok := classified.Details["valid"].([]string)
	if !ok || len(valid) != 3 {
		t.Errorf("details.valid = %#v, want the three policies", classified.Details["valid"])
	}
}

// --force keeps working as the shorthand for --dirty stash.
func TestSyncDirty_ForceIsStashShorthand(t *testing.T) {
	resetCommandState(t)
	env := syncDirtyEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"sync", "--yes", "--force", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--force: %v", err)
	}

	var payload syncJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Summary.Pulled != 1 {
		t.Errorf("pulled = %d, want 1", payload.Summary.Pulled)
	}
	if !env.FileExists(env.GetWorktreePath("backend", "api") + "/dirty-file.txt") {
		t.Error("--force must restore the stash, like --dirty stash")
	}
}

// config was READ-ONLY without a TTY: the only way to change a setting was a prompt,
// so an agent could see the configuration but never write it.
func TestConfigSet_WritesWithoutAPrompt(t *testing.T) {
	resetCommandState(t)
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"config", "set", "editor", "code --wait", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config set editor: %v", err)
	}

	var payload configPayload
	decodeJSONData(t, stdout, &payload)
	if payload.Editor != "code --wait" {
		t.Errorf("editor = %q, want the value passed", payload.Editor)
	}
	if !payload.Changed {
		t.Error("changed must be true for a write")
	}

	// It must persist, not merely be reported.
	loaded, err := global.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Defaults.Editor != "code --wait" {
		t.Errorf("persisted editor = %q, want the value passed", loaded.Defaults.Editor)
	}
}

func TestConfigSet_RejectsAnUnknownThemeWithTheValidSet(t *testing.T) {
	resetCommandState(t)
	t.Setenv("HYDRA_CONFIG_DIR", t.TempDir())

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"config", "set", "theme", "no-such-theme", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an unknown theme must be refused")
	}
	valid, ok := output.Classify(err).Details["valid"].([]string)
	if !ok || len(valid) == 0 {
		t.Errorf("details.valid must list the theme names, got %#v", output.Classify(err).Details["valid"])
	}
}

func TestConfigSet_MissingKeyOrValueAsksForIt(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		missing string
	}{
		{name: "no key", args: []string{"config", "set"}, missing: "<key>"},
		{name: "no value", args: []string{"config", "set", "theme"}, missing: "<value>"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resetCommandState(t)
			t.Setenv("HYDRA_CONFIG_DIR", t.TempDir())

			resetCommandState(t)
			resetCommandIO()
			rootCmd.SetArgs(append(tc.args, "--output", "json"))
			err := rootCmd.Execute()
			if err == nil {
				t.Fatalf("%v must ask for the missing argument", tc.args)
			}
			classified := output.Classify(err)
			if classified.Code != output.CodeNeedsInput {
				t.Fatalf("code = %q, want %q", classified.Code, output.CodeNeedsInput)
			}
			missing, _ := classified.Details["missing"].([]string)
			if len(missing) != 1 || missing[0] != tc.missing {
				t.Errorf("details.missing = %#v, want [%s]", classified.Details["missing"], tc.missing)
			}
		})
	}
}

// config show is the explicit read, and must not report a change.
func TestConfigShow_ReportsWithoutChanging(t *testing.T) {
	resetCommandState(t)
	t.Setenv("HYDRA_CONFIG_DIR", t.TempDir())

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"config", "show", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config show: %v", err)
	}

	var payload configPayload
	decodeJSONData(t, stdout, &payload)
	if payload.Changed {
		t.Error("a read must not report changed")
	}
	if payload.Theme == "" || payload.ConfigPath == "" {
		t.Errorf("show must report the theme and path, got %+v", payload)
	}
}

var _ = config.Defaults{}

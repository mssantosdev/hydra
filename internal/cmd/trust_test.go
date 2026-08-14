package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/trust"
)

// trustEnv builds a workspace whose manifest carries a hook, which is what makes the gate
// apply at all. The hook writes a sentinel file so a test can assert whether it RAN, rather
// than trusting the envelope's word for it.
func trustEnv(t *testing.T) (*testutil.TestEnv, string) {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	sentinel := filepath.Join(t.TempDir(), "hook-ran")
	cfg := env.LoadConfig()
	cfg.Hooks.PostAdd = []config.Hook{{Name: "sentinel", Run: "touch " + sentinel}}
	env.SaveConfig(cfg)
	return env, sentinel
}

// trustCurrentWorkspace approves the loaded workspace, for tests whose subject is a hook
// firing rather than the gate. It is deliberately NOT folded into the shared fixture: a test
// that needs execution says so, so the gate stays exercised by default and a regression that
// silently disabled it would fail the tests above rather than passing everywhere.
func trustCurrentWorkspace(t *testing.T) {
	t.Helper()
	if cfg == nil || projectRoot == "" {
		t.Fatal("trustCurrentWorkspace needs a loaded project")
	}
	if _, err := trust.Approve(global.GetConfigDir(), projectRoot, cfg, ""); err != nil {
		t.Fatalf("approve workspace: %v", err)
	}
}

func trustEnvelope(t *testing.T, stdout *bytes.Buffer) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.String())
	}
	return envelope
}

// The whole point: pulling a branch whose manifest adds a hook must not execute it.
func TestUntrustedManifestDoesNotExecuteItsHook(t *testing.T) {
	resetCommandState(t)
	_, sentinel := trustEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("add on an untrusted workspace must refuse")
	}
	if code := output.Classify(err).Code; code != output.CodeManifestUntrusted {
		t.Fatalf("code = %q, want %q", code, output.CodeManifestUntrusted)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("the hook RAN on an untrusted workspace; the gate does not gate")
	}
	_ = stdout
}

// Inspecting a workspace is how you decide whether to trust it, so read-only commands must
// keep working while it is untrusted.
func TestUntrustedWorkspaceStillAllowsInspection(t *testing.T) {
	for _, args := range [][]string{
		{"list", "--output", "json"},
		{"status", "--output", "json"},
		{"where", "--output", "json"},
		{"hooks", "ls", "--output", "json"},
		{"trust", "--show", "--output", "json"},
	} {
		t.Run(strings.Join(args[:len(args)-2], " "), func(t *testing.T) {
			resetCommandState(t)
			trustEnv(t)

			resetCommandState(t)
			resetCommandIO()
			rootCmd.SetArgs(args)
			if err := rootCmd.Execute(); err != nil {
				t.Errorf("%v must work on an untrusted workspace: %v", args, err)
			}
		})
	}
}

func TestTrustThenTheHookRuns(t *testing.T) {
	resetCommandState(t)
	_, sentinel := trustEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"trust", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("trust: %v", err)
	}

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add after trust: %v", err)
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Error("the hook did not run after the workspace was trusted")
	}
}

// A manifest with nothing executable has nothing to approve, so it must never meet the gate.
// This is what keeps the feature invisible to workspaces that never configure a hook.
func TestWorkspaceWithNoExecutableSurfaceNeverNeedsTrust(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("a hookless workspace must not be gated: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(global.GetConfigDir(), "trust.yaml")); statErr == nil {
		t.Error("a trust store was written for a workspace with nothing to trust")
	}
}

// Editing a hook must re-block, and the refusal must name the manifest path WITHOUT echoing
// the hook's text — a hook line is exactly where a credential ends up.
func TestEditingAHookReBlocksAndNeverEchoesIt(t *testing.T) {
	resetCommandState(t)
	env, _ := trustEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"trust", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("trust: %v", err)
	}

	cfg := env.LoadConfig()
	cfg.Hooks.PostAdd = []config.Hook{{Name: "sentinel", Run: "./post-to-forge --token SECRET123"}}
	env.SaveConfig(cfg)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("an edited hook must re-block")
	}
	e := output.Classify(err)
	if e.Code != output.CodeManifestUntrusted {
		t.Fatalf("code = %q, want %q", e.Code, output.CodeManifestUntrusted)
	}
	if got := e.Details["reason"]; got != "changed" {
		t.Errorf("reason = %v, want \"changed\"", got)
	}
	changed, _ := e.Details["changed"].([]string)
	if len(changed) != 1 || changed[0] != "hooks.post_add[0]" {
		t.Errorf("changed = %v, want the manifest path of the edited hook", changed)
	}
	if strings.Contains(stdout.String(), "SECRET123") {
		t.Error("the refusal echoed the hook's arguments into the envelope")
	}
}

// The refusal must hand back the recovery, because guidance a caller has to know to ask for is
// not an affordance.
func TestUntrustedRefusalCarriesItsRecovery(t *testing.T) {
	resetCommandState(t)
	trustEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected a refusal")
	}
	envelope := trustEnvelope(t, stdout)
	next, _ := envelope["next"].([]any)
	if len(next) == 0 {
		t.Fatalf("no next[] on the refusal: %s", stdout.String())
	}
	var argvs []string
	for _, n := range next {
		m, _ := n.(map[string]any)
		parts, _ := m["argv"].([]any)
		var words []string
		for _, p := range parts {
			words = append(words, p.(string))
		}
		argvs = append(argvs, strings.Join(words, " "))
	}
	joined := strings.Join(argvs, " | ")
	for _, want := range []string{"hydra trust", "hooks ls"} {
		if !strings.Contains(joined, want) {
			t.Errorf("next[] does not offer %q: %v", want, argvs)
		}
	}
}

func TestTrustRevokeMakesItUntrustedAgain(t *testing.T) {
	resetCommandState(t)
	_, sentinel := trustEnv(t)

	for _, args := range [][]string{
		{"trust", "--output", "json"},
		{"trust", "--revoke", "--output", "json"},
	} {
		resetCommandState(t)
		resetCommandIO()
		rootCmd.SetArgs(args)
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("a revoked workspace must be gated again")
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("the hook ran after trust was revoked")
	}
}

func TestTrustShowAndRevokeAreMutuallyExclusive(t *testing.T) {
	resetCommandState(t)
	trustEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"trust", "--show", "--revoke", "--output", "json"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("--show with --revoke must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeUsage {
		t.Errorf("code = %q, want %q", code, output.CodeUsage)
	}
}

// --no-hooks suppresses execution, so there is nothing to approve and it must not be gated.
func TestNoHooksNeedsNoTrust(t *testing.T) {
	resetCommandState(t)
	_, sentinel := trustEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--no-hooks", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--no-hooks runs nothing from the manifest, so it must not be gated: %v", err)
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("--no-hooks ran the hook")
	}
}

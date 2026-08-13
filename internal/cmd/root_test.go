package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestRootVersionFlag(t *testing.T) {
	resetCommandState(t)
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "1.2.3", "abc123", "2026-04-17T00:00:00Z"
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.Version = versionInfo()

	rootCmd.SetArgs([]string{"--version"})
	_ = rootCmd.Execute()

	if !strings.Contains(out.String(), "v1.2.3") || !strings.Contains(out.String(), "abc123") {
		t.Fatalf("expected version output, got %q", out.String())
	}
}

func TestRootHelpShowsVersion(t *testing.T) {
	resetCommandState(t)
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "dev", "", ""
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.Version = versionInfo()

	rootCmd.SetArgs([]string{"--help"})
	_ = rootCmd.Execute()

	testutil.Contains(t, out.String(), "Version:")
	testutil.Contains(t, out.String(), "dev")
}

func TestRootDefaultOutputShowsVersion(t *testing.T) {
	resetCommandState(t)
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "dev", "", ""
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.Version = versionInfo()

	rootCmd.SetArgs([]string{})
	_ = rootCmd.Execute()

	testutil.Contains(t, out.String(), "Version:")
	testutil.Contains(t, out.String(), "dev")
}

// "no hydra project loaded" with no details cannot be acted on: a caller cannot tell
// a wrong working directory from a workspace that was never created.
func TestNotInProjectSaysWhereItLooked(t *testing.T) {
	resetCommandState(t)

	err := errNotInProject()
	if err.Code != output.CodeNotInProject {
		t.Fatalf("code = %q, want %q", err.Code, output.CodeNotInProject)
	}
	for _, key := range []string{"searched_from", "looked_for"} {
		if _, ok := err.Details[key]; !ok {
			t.Errorf("details is missing %q: %v", key, err.Details)
		}
	}
	if len(err.Next) == 0 {
		t.Fatal("no recovery hint; a caller that must know to ask has no affordance")
	}
	// `hydra init` REFUSES when a manifest already exists, so advising it for a
	// malformed or unreachable workspace sends the caller to a command that cannot work.
	for _, n := range err.Next {
		if len(n.Argv) > 1 && n.Argv[1] == "init" {
			t.Errorf("hint advises %v, which refuses when a manifest exists", n.Argv)
		}
	}
}

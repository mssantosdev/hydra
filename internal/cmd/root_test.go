package cmd

import (
	"os"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestRootVersionFlag(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "1.2.3", "abc123", "2026-04-17T00:00:00Z"
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.Version = versionInfo()

	out := testutil.CaptureOutput(func() {
		rootCmd.SetArgs([]string{"--version"})
		_ = rootCmd.Execute()
	})

	if !strings.Contains(out, "v1.2.3") || !strings.Contains(out, "abc123") {
		t.Fatalf("expected version output, got %q", out)
	}
}

func TestRootHelpShowsVersion(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "dev", "", ""
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.Version = versionInfo()

	out := testutil.CaptureOutput(func() {
		rootCmd.SetArgs([]string{"--help"})
		_ = rootCmd.Execute()
	})

	testutil.Contains(t, out, "Version:")
	testutil.Contains(t, out, "dev")
}

func TestRootDefaultOutputShowsVersion(t *testing.T) {
	oldVersion, oldCommit, oldBuiltAt := version, commit, builtAt
	oldOut, oldErr := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr()
	version, commit, builtAt = "dev", "", ""
	t.Cleanup(func() {
		version, commit, builtAt = oldVersion, oldCommit, oldBuiltAt
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
	})
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
	rootCmd.Version = versionInfo()

	out := testutil.CaptureOutput(func() {
		rootCmd.SetArgs([]string{})
		_ = rootCmd.Execute()
	})

	testutil.Contains(t, out, "Version:")
	testutil.Contains(t, out, "dev")
}

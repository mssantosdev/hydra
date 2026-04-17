package cmd

import (
	"bytes"
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

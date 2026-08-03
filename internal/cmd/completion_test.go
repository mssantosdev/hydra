package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/spf13/cobra"
)

func TestCompletionCommandPrintsScripts(t *testing.T) {
	resetCommandState(t)
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "complete"},
		{shell: "zsh", want: "compdef"},
		{shell: "fish", want: "complete -c hydra"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			rootCmd.SetArgs([]string{"completion", tt.shell})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", tt.shell, err)
			}

			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("completion %s output missing %q", tt.shell, tt.want)
			}
		})
	}
}

func TestCompleteRepoAliasesReturnsConfiguredAliases(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("frontend", "web", "main")
	env.Chdir()

	aliases, directive := completeRepoAliases(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	want := map[string]bool{"api": true, "web": true}
	if len(aliases) != len(want) {
		t.Fatalf("expected %d aliases, got %v", len(want), aliases)
	}
	for _, alias := range aliases {
		if !want[alias] {
			t.Fatalf("unexpected alias %q in %v", alias, aliases)
		}
	}
}

func TestCompleteRepoAliasesWithoutConfigReturnsEmpty(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.Chdir()

	aliases, directive := completeRepoAliases(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}
	if len(aliases) != 0 {
		t.Fatalf("expected no suggestions without config, got %v", aliases)
	}
}

func TestCompleteWorktreeNamesReturnsDirectoryBasenames(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	_, _, _ = env.SetupRepo("backend", "api", "main")
	env.CreateWorktree("backend", "api", "feature/login", "api-feature-login")
	env.Chdir()

	names, directive := completeWorktreeNames(nil, nil, "")
	if directive != cobra.ShellCompDirectiveNoFileComp {
		t.Fatalf("unexpected directive: %v", directive)
	}

	want := map[string]bool{"api": true, "api-feature-login": true}
	if len(names) != len(want) {
		t.Fatalf("expected %d worktree names, got %v", len(want), names)
	}
	for _, name := range names {
		if !want[name] {
			t.Fatalf("unexpected worktree name %q in %v", name, names)
		}
	}
}

func TestGlossaryNonInteractiveJSON(t *testing.T) {
	resetCommandState(t)
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"glossary", "--output", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("glossary --output json failed: %v", err)
	}

	var envelope struct {
		Data struct {
			Terms []struct {
				Term       string `json:"term"`
				Definition string `json:"definition"`
			} `json:"terms"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse glossary JSON: %v\noutput: %s", err, out.String())
	}
	if len(envelope.Data.Terms) == 0 {
		t.Fatal("expected glossary terms in JSON output")
	}
}

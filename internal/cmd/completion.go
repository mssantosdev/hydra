package cmd

import (
	"bytes"
	"os"
	"path/filepath"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion <bash|zsh|fish>",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Hydra.

DESCRIPTION
  Writes a shell completion script to stdout. Redirect the output into your
  shell's completion directory or source it from your shell rc.

  Supported shells: bash, zsh, fish.

EXAMPLES
  # Generate and inspect bash completions
  $ hydra completion bash

  # Install fish completions manually
  $ hydra completion fish > ~/.config/fish/completions/hydra.fish

NOTES
  Prefer ` + "`hydra init-shell --with-completion`" + ` to install helper and
  completion together. Use this command when you only need the completion script.

EXIT CODES
  0  Success (completion script written to stdout)
  1  General error (unsupported shell)`,
	Args: cobra.ExactArgs(1),
	RunE: runCompletion,
}

func init() {
	rootCmd.AddCommand(completionCmd)
	removeCmd.ValidArgsFunction = completeRepoAliases
	syncCmd.ValidArgsFunction = completeRepoAliases
}

func runCompletion(cmd *cobra.Command, args []string) error {
	var buf bytes.Buffer
	switch args[0] {
	case "bash":
		if err := rootCmd.GenBashCompletion(&buf); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to generate bash completion")
		}
	case "zsh":
		if err := rootCmd.GenZshCompletion(&buf); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to generate zsh completion")
		}
	case "fish":
		if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to generate fish completion")
		}
	default:
		return output.Errorf(output.CodeInternal,
			"unsupported shell: %s (supported: bash, zsh, fish)", args[0])
	}

	_, err := cmd.OutOrStdout().Write(buf.Bytes())
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to write completion script")
	}
	return nil
}

func completeRepoAliases(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	_, cfg, err := config.FindConfig(wd)
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	refs := cfg.Repos()
	aliases := make([]string, 0, len(refs))
	for _, ref := range refs {
		aliases = append(aliases, ref.Alias)
	}

	return aliases, cobra.ShellCompDirectiveNoFileComp
}

func completeWorktreeNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projectRoot := filepath.Dir(configPath)
	worktrees, _ := collectWorktrees(cfg, projectRoot)

	results := make([]string, 0, len(worktrees))
	for _, wt := range worktrees {
		results = append(results, wt.DirName)
	}

	return results, cobra.ShellCompDirectiveNoFileComp
}

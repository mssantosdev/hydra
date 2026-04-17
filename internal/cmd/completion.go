package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion <bash|zsh|fish>",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for Hydra.

The output is written to stdout so it can be redirected into a file or
pipelines managed by init-shell.`,
	Args: cobra.ExactArgs(1),
	RunE: runCompletion,
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

func runCompletion(cmd *cobra.Command, args []string) error {
	var buf bytes.Buffer
	switch args[0] {
	case "bash":
		if err := rootCmd.GenBashCompletion(&buf); err != nil {
			return err
		}
	case "zsh":
		if err := rootCmd.GenZshCompletion(&buf); err != nil {
			return err
		}
	case "fish":
		if err := rootCmd.GenFishCompletion(&buf, true); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", args[0])
	}

	_, err := cmd.OutOrStdout().Write(buf.Bytes())
	return err
}

func completeRepoAliases(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	_, cfg, err := config.FindConfig(wd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	aliases := make([]string, 0)
	for _, ecosystem := range cfg.Ecosystems {
		for alias := range ecosystem {
			aliases = append(aliases, alias)
		}
	}

	return aliases, cobra.ShellCompDirectiveNoFileComp
}

func completeWorktreeNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	projectRoot := filepath.Dir(configPath)
	choices, err := collectWorktreeChoices(cfg, projectRoot)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	results := make([]string, 0, len(choices))
	for _, choice := range choices {
		results = append(results, choice.SymlinkName)
	}

	return results, cobra.ShellCompDirectiveNoFileComp
}

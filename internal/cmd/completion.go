package cmd

import (
	"os"
	"path/filepath"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/spf13/cobra"
)

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

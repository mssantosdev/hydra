package cmd

import (
	"fmt"
	"os"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	rootCmd = &cobra.Command{
		Use:   "hydra",
		Short: "Hydra - Git worktree manager",
		Long: `Hydra is a beautiful CLI tool for managing Git worktrees.

It helps you organize multiple worktrees across different repositories
and ecosystems, making it easy to work on multiple branches simultaneously.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip config loading for commands that don't require it
			if cmd.Name() == "init" || cmd.Name() == "clone" || cmd.Name() == "help" || cmd.Name() == "glossary" || cmd.Name() == "config" || cmd.Name() == "init-shell" {
				return nil
			}

			// Find and load config
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}

			_, cfg, err = config.FindConfig(wd)
			if err != nil {
				return fmt.Errorf(`%v

Run "hydra init" to create a new configuration.`, err)
			}

			return nil
		},
	}
)

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .hydra.yaml)")
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}

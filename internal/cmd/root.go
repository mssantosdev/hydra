package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
	cfg     *config.Config
	version = "dev"
	commit  = ""
	builtAt = ""
	rootCmd = &cobra.Command{
		Use:   "hydra",
		Short: "Hydra - Git worktree manager",
		Long: `Hydra is a beautiful CLI tool for managing Git worktrees.

It helps you organize multiple worktrees across different repositories
and ecosystems, making it easy to work on multiple branches simultaneously.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip config loading for commands that don't require it
			if cmd.Parent() == nil || cmd.Name() == "init" || cmd.Name() == "clone" || cmd.Name() == "new" || cmd.Name() == "help" || cmd.Name() == "glossary" || cmd.Name() == "config" || cmd.Name() == "init-shell" || cmd.Name() == "completion" {
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
)

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .hydra.yaml)")
	rootCmd.Version = versionInfo()
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.SetHelpTemplate(`{{with .Long}}{{.}}{{else}}{{.Short}}{{end}}

Version: {{.Version}}

Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}Commands:
{{range .Commands}}{{if .IsAvailableCommand}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}
Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}`)
}

// GetConfig returns the loaded configuration
func GetConfig() *config.Config {
	return cfg
}

func versionInfo() string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "dev"
	}

	if !strings.HasPrefix(v, "v") && v != "dev" {
		v = "v" + v
	}

	parts := []string{v}
	if commit != "" {
		parts = append(parts, commit)
	}
	if builtAt != "" {
		parts = append(parts, builtAt)
	}

	return strings.Join(parts, " ")
}

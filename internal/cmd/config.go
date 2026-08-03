package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/mssantosdev/hydra/internal/ui/themes"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage global configuration",
	Long: `Manage Hydra's global settings.

DESCRIPTION
  Opens an interactive form when stdin and stdout are terminals. Otherwise
  emits the current settings as data (JSON with --output json).

  Configurable options:
    • Theme  - Color scheme (tokyonight, catppuccin, dracula, nord, onedark)
    • Editor - Default editor command (code, vim, nano, etc.)

CONFIG LOCATION
  Linux:   ~/.config/hydra/config.yaml
  macOS:   ~/Library/Application Support/hydra/config.yaml
  Windows: %APPDATA%/hydra/config.yaml

EXAMPLES
  # Open interactive config (TTY)
  $ hydra config

  # Emit current settings as JSON
  $ hydra config --output json

EXIT CODES
  0  Success (config saved, displayed, or no changes)
  1  General error (load/save failed, form cancelled)

SEE ALSO
  • hydra init - Project-level configuration (.hydra.yaml)`,
	RunE: runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

type configPayload struct {
	Theme      string `json:"theme"`
	Editor     string `json:"editor"`
	ConfigPath string `json:"config_path"`
	Changed    bool   `json:"changed,omitempty"`
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := global.Load()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to load global config")
	}

	if !interactive() {
		return emit(cmd, configPayload{
			Theme:      cfg.Theme.Name,
			Editor:     cfg.Defaults.Editor,
			ConfigPath: global.GetConfigPath(),
		}, nil, func() {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Theme:  %s %s\n", cfg.Theme.Name, themes.Get(cfg.Theme.Name).Preview())
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Editor: %s\n", cfg.Defaults.Editor)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Config: %s\n", global.GetConfigPath())
		})
	}

	var themeOptions []huh.Option[string]
	for _, name := range themes.GetNames() {
		theme := themes.Get(name)
		label := fmt.Sprintf("%s %s", name, theme.Preview())
		themeOptions = append(themeOptions, huh.NewOption(label, name))
	}

	var (
		newTheme  string
		newEditor string
	)

	newTheme = cfg.Theme.Name
	newEditor = cfg.Defaults.Editor

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Theme").
				Description("Select a color theme (preview shown)").
				Options(themeOptions...).
				Value(&newTheme),

			huh.NewInput().
				Title("Default Editor").
				Description("Command to open files (e.g., code, vim, nano)").
				Value(&newEditor),
		),
	)

	if err := form.Run(); err != nil {
		return output.Errorf(output.CodeInternal, "configuration cancelled")
	}

	hasChanges := false

	if newTheme != cfg.Theme.Name {
		cfg.Theme.Name = newTheme
		themes.Set(newTheme)
		styles.ReloadTheme()
		hasChanges = true
		log.Success("Theme updated", "value", newTheme)
	}

	if newEditor != cfg.Defaults.Editor {
		cfg.Defaults.Editor = newEditor
		hasChanges = true
		log.Success("Editor updated", "value", newEditor)
	}

	if hasChanges {
		if err := cfg.Save(); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to save global config")
		}
	}

	payload := configPayload{
		Theme:      cfg.Theme.Name,
		Editor:     cfg.Defaults.Editor,
		ConfigPath: global.GetConfigPath(),
		Changed:    hasChanges,
	}

	return emit(cmd, payload, nil, func() {
		fmt.Println(styles.AppHeader.Render(" HYDRA "))
		fmt.Println()
		fmt.Println(styles.Title.Render("Configuration"))
		fmt.Println()
		fmt.Println(styles.Label.Render("Current Settings:"))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Theme:    %s %s\n", cfg.Theme.Name, themes.Get(cfg.Theme.Name).Preview())
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  Editor:   %s\n", cfg.Defaults.Editor)
		fmt.Println()
		if hasChanges {
			fmt.Println(styles.Success.Render("✓ Configuration saved"))
		} else {
			fmt.Println(styles.Dimmed.Render("No changes made"))
		}
		fmt.Println()
		fmt.Println(styles.Label.Render("Config file:"))
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", global.GetConfigPath())
	})
}

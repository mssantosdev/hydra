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
  • hydra init - Project-level configuration (.hydra/config.yaml)`,
	RunE: runConfig,
}

var configShowCmd = &cobra.Command{
	Use:     "show",
	Aliases: []string{"view"},
	Short:   "Print the global configuration",
	Args:    cobra.NoArgs,
	RunE:    runConfigShow,
}

var configSetCmd = &cobra.Command{
	Use:   "set <theme|editor> <value>",
	Short: "Set a global setting without a prompt",
	Long: `Set a global setting.

DESCRIPTION
  The non-interactive equivalent of the form "hydra config" opens. Without this the
  only way to change a setting was a prompt, so an agent — or any non-TTY caller —
  could read the configuration but never write it.

KEYS
  theme   one of the registered theme names; "hydra config show" lists the current one
  editor  any command line, used when hydra opens a file

EXAMPLES
  $ hydra config set theme tokyonight
  $ hydra config set editor "code --wait"

EXIT CODES
  0  Success
  1  internal (unknown key, or an invalid theme name)
  7  needs_input (a key or value is missing)`,
	Args:      cobra.MaximumNArgs(2),
	RunE:      runConfigSet,
	ValidArgs: []string{"theme", "editor"},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configShowCmd, configSetCmd)

	configSetCmd.ValidArgsFunction = completeConfigSetArgs
}

// completeConfigSetArgs completes the key, then that key's values. Only theme has a
// closed set; an editor command line cannot be enumerated.
func completeConfigSetArgs(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return []string{"theme", "editor"}, cobra.ShellCompDirectiveNoFileComp
	case 1:
		if args[0] == "theme" {
			return themes.GetNames(), cobra.ShellCompDirectiveNoFileComp
		}
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := global.Load()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to load global config")
	}
	return emitGlobalConfig(cmd, cfg, false)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return output.Errorf(output.CodeNeedsInput, "which setting?").
			WithDetail("missing", []string{"<key>"}).
			WithDetail("one_of", []string{"theme", "editor"})
	}
	key := args[0]
	if len(args) == 1 {
		return output.Errorf(output.CodeNeedsInput, "a value for %q is required", key).
			WithDetail("missing", []string{"<value>"}).
			WithDetail("key", key)
	}
	value := args[1]

	cfg, err := global.Load()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to load global config")
	}

	switch key {
	case "theme":
		// Validate HERE rather than relying on SetTheme, which does not: it assigns and
		// saves, so `config set theme garbage` used to persist an unusable value that
		// only surfaced as a silent fallback at render time.
		if !themes.IsValid(value) {
			return output.Errorf(output.CodeInternal, "unknown theme %q", value).
				WithDetail("theme", value).
				WithDetail("valid", themes.GetNames())
		}
		if err := cfg.SetTheme(value); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to set the theme")
		}
	case "editor":
		if err := cfg.SetEditor(value); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to set the editor")
		}
	default:
		return output.Errorf(output.CodeInternal, "unknown setting %q", key).
			WithDetail("key", key).
			WithDetail("valid", []string{"theme", "editor"})
	}

	return emitGlobalConfig(cmd, cfg, true)
}

// emitGlobalConfig is the one funnel for both show and set, so a written value is
// reported in exactly the shape a reader would have seen.
func emitGlobalConfig(cmd *cobra.Command, cfg *global.GlobalConfig, changed bool) error {
	payload := configPayload{
		Theme:      cfg.Theme.Name,
		Editor:     cfg.Defaults.Editor,
		ConfigPath: global.GetConfigPath(),
		Changed:    changed,
	}
	summary := "global configuration"
	if changed {
		summary = "global configuration updated"
	}
	return emit(cmd, summary, payload, nil, func() {
		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "Theme:  %s %s\n", cfg.Theme.Name, themes.Get(cfg.Theme.Name).Preview())
		_, _ = fmt.Fprintf(out, "Editor: %s\n", cfg.Defaults.Editor)
		_, _ = fmt.Fprintf(out, "Config: %s\n", global.GetConfigPath())
	})
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
		return emit(cmd, "global configuration", configPayload{
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

	return emit(cmd, "global configuration", payload, nil, func() {
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

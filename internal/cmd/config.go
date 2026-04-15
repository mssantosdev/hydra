package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/i18n"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/mssantosdev/hydra/internal/ui/themes"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage global configuration",
	Long: `Manage Hydra's global configuration settings.

This command allows you to customize:
- Language (en-US, pt-BR)
- Theme (tokyonight, catppuccin, dracula, nord, onedark)
- Default editor
- Other preferences

Configuration is stored in:
  Linux: ~/.config/hydra/config.yaml
  macOS: ~/Library/Application Support/hydra/config.yaml
  Windows: %APPDATA%/hydra/config.yaml`,
	RunE: runConfig,
}

func init() {
	rootCmd.AddCommand(configCmd)
}

func runConfig(cmd *cobra.Command, args []string) error {
	// Load current config
	cfg, err := global.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize i18n
	i18n.Init(cfg.Language)

	// Show header
	fmt.Println()
	fmt.Println(styles.AppHeader.Render(" HYDRA "))
	fmt.Println()
	fmt.Println(styles.Title.Render(i18n.T("cmd.config")))
	fmt.Println()

	// Current settings
	fmt.Println(styles.Label.Render("Current Settings:"))
	fmt.Printf("  Language: %s\n", cfg.Language)
	fmt.Printf("  Theme:    %s %s\n", cfg.Theme.Name, themes.Get(cfg.Theme.Name).Preview())
	fmt.Printf("  Editor:   %s\n", cfg.Defaults.Editor)
	fmt.Println()

	// Build theme options with previews
	var themeOptions []huh.Option[string]
	for _, name := range themes.GetNames() {
		theme := themes.Get(name)
		label := fmt.Sprintf("%s %s", name, theme.Preview())
		themeOptions = append(themeOptions, huh.NewOption(label, name))
	}

	// Language options
	langOptions := []huh.Option[string]{
		huh.NewOption("English (US)", "en-US"),
		huh.NewOption("Português (BR)", "pt-BR"),
	}

	var (
		newLang   string
		newTheme  string
		newEditor string
	)

	// Start with current values
	newLang = cfg.Language
	newTheme = cfg.Theme.Name
	newEditor = cfg.Defaults.Editor

	// Config form
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Language").
				Description("Select your preferred language").
				Options(langOptions...).
				Value(&newLang),

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
		return fmt.Errorf("cancelled")
	}

	// Apply changes
	hasChanges := false

	if newLang != cfg.Language {
		cfg.Language = newLang
		hasChanges = true
		log.Success("Language updated", "value", newLang)
	}

	if newTheme != cfg.Theme.Name {
		cfg.Theme.Name = newTheme
		themes.Set(newTheme)
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
			return fmt.Errorf("failed to save config: %w", err)
		}
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Configuration saved"))
	} else {
		fmt.Println()
		fmt.Println(styles.Dimmed.Render("No changes made"))
	}

	// Show config file location
	fmt.Println()
	fmt.Println(styles.Label.Render("Config file:"))
	fmt.Printf("  %s\n", global.GetConfigPath())

	return nil
}

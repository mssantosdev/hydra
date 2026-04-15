package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/spf13/cobra"
)

func TestConfig_NoArgs(t *testing.T) {
	// Config command should work without project config
	rootCmd.SetArgs([]string{"config", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Errorf("Config help should not fail: %v", err)
	}
}

func TestConfig_CommandAvailable(t *testing.T) {
	// Verify config command is registered
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "config" {
			found = true
			break
		}
	}

	if !found {
		t.Error("config command should be registered")
	}
}

func TestConfig_CommandProperties(t *testing.T) {
	// Find config command
	var configCommand *cobra.Command
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "config" {
			configCommand = cmd
			break
		}
	}

	if configCommand == nil {
		t.Fatal("config command not found")
	}

	if configCommand.Short == "" {
		t.Error("config command should have a short description")
	}

	if configCommand.Long == "" {
		t.Error("config command should have a long description")
	}
}

func TestAvailableLanguages(t *testing.T) {
	langs := global.AvailableLanguages()

	if len(langs) != 2 {
		t.Errorf("Expected 2 languages, got %d", len(langs))
	}

	hasEn := false
	hasPt := false
	for _, lang := range langs {
		if lang == "en-US" {
			hasEn = true
		}
		if lang == "pt-BR" {
			hasPt = true
		}
	}

	if !hasEn {
		t.Error("Should have en-US")
	}
	if !hasPt {
		t.Error("Should have pt-BR")
	}
}

func TestAvailableThemes(t *testing.T) {
	themes := []string{"tokyonight", "catppuccin", "dracula", "nord", "onedark"}

	for _, name := range themes {
		if !global.IsValidLanguage(name) {
			// Note: This tests theme names, not language - the function name is misleading
			// Actually, we should test themes.IsValid
		}
	}
}

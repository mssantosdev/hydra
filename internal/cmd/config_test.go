package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

func TestConfig_NoArgs(t *testing.T) {
	resetCommandState(t)
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

	err := configCommand.Help()
	if err != nil {
		t.Errorf("Config help should not fail: %v", err)
	}
}

func TestConfig_CommandAvailable(t *testing.T) {
	resetCommandState(t)
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
	resetCommandState(t)
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

func TestConfigNonInteractiveJSON(t *testing.T) {
	resetCommandState(t)
	outputFlag = ""

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HYDRA_CONFIG_DIR", filepath.Join(home, ".config", "hydra"))

	cfg, err := global.Load()
	if err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}
	cfg.Theme.Name = "tokyonight"
	cfg.Defaults.Editor = "vim"
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save global config: %v", err)
	}

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"config", "--output", "json"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("config --output json failed: %v", err)
	}

	var envelope struct {
		Data struct {
			Theme  string `json:"theme"`
			Editor string `json:"editor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to parse config JSON: %v\noutput: %s", err, out.String())
	}
	if envelope.Data.Theme != "tokyonight" {
		t.Fatalf("expected theme tokyonight, got %q", envelope.Data.Theme)
	}
	if envelope.Data.Editor != "vim" {
		t.Fatalf("expected editor vim, got %q", envelope.Data.Editor)
	}
}

func TestConfigReloadsThemeAfterChange(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("HYDRA_CONFIG_DIR", filepath.Join(home, ".config", "hydra"))

	cfg, err := global.Load()
	if err != nil {
		t.Fatalf("failed to load global config: %v", err)
	}

	styles.ReloadTheme()
	before := styles.Green

	cfg.Theme.Name = "dracula"
	if err := cfg.Save(); err != nil {
		t.Fatalf("failed to save global config: %v", err)
	}
	styles.ReloadTheme()
	after := styles.Green

	if before == after {
		t.Fatalf("expected theme colors to change after ReloadTheme, green stayed %v", before)
	}

	_ = os.Getenv("HOME") // keep os import used when build tags vary
}

// The default theme is a VALUE, so it is pinned here rather than by grepping prose for one English
// phrasing. A fresh install inherits the terminal's own palette: it needs no OSC query and no config
// parsing, so it is the only choice that is correct before anything has been resolved.
func TestDefaultThemeIsTheTerminalPalette(t *testing.T) {
	resetCommandState(t)
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	loaded, err := global.Load()
	if err != nil {
		t.Fatalf("load global config: %v", err)
	}
	if got := loaded.Theme.Name; got != "terminal" {
		t.Errorf("default theme = %q, want %q — README and docs/README describe the terminal "+
			"palette as the default", got, "terminal")
	}
}

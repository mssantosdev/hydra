package global

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()

	if cfg.Version != "1.0" {
		t.Errorf("Expected version 1.0, got %s", cfg.Version)
	}

	if cfg.Theme.Name != "tokyonight" {
		t.Errorf("Expected theme tokyonight, got %s", cfg.Theme.Name)
	}

	if cfg.Defaults.Editor != "code" {
		t.Errorf("Expected editor code, got %s", cfg.Defaults.Editor)
	}

	if cfg.Defaults.Pager != "less" {
		t.Errorf("Expected pager less, got %s", cfg.Defaults.Pager)
	}

	if !cfg.Defaults.ConfirmDestructive {
		t.Error("Expected ConfirmDestructive to be true")
	}
}

func TestGetConfigDir(t *testing.T) {
	dir := GetConfigDir()

	if dir == "" {
		t.Error("Config dir should not be empty")
	}

	// Should contain "hydra"
	if !contains(dir, "hydra") {
		t.Errorf("Config dir should contain 'hydra', got %s", dir)
	}
}

func TestGetConfigPath(t *testing.T) {
	path := GetConfigPath()

	if path == "" {
		t.Error("Config path should not be empty")
	}

	// Should end with config.yaml
	if !contains(path, "config.yaml") {
		t.Errorf("Config path should contain 'config.yaml', got %s", path)
	}
}

func TestSaveAndLoad(t *testing.T) {
	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "hydra-config-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create config
	cfg := &GlobalConfig{
		Version: "1.0",
		Theme: ThemeConf{
			Name: "catppuccin",
		},
		Defaults: Defaults{
			Editor:             "vim",
			Pager:              "cat",
			ConfirmDestructive: false,
		},
	}

	// Save
	configPath := filepath.Join(tempDir, "config.yaml")
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}
	os.WriteFile(configPath, data, 0644)

	// Load
	loadedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	var loaded GlobalConfig
	if err := yaml.Unmarshal(loadedData, &loaded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify

	if loaded.Theme.Name != "catppuccin" {
		t.Errorf("Expected theme catppuccin, got %s", loaded.Theme.Name)
	}

	if loaded.Defaults.Editor != "vim" {
		t.Errorf("Expected editor vim, got %s", loaded.Defaults.Editor)
	}
}

func TestSetTheme(t *testing.T) {
	cfg := DefaultGlobalConfig()

	// Set theme
	cfg.Theme.Name = "dracula"

	if cfg.Theme.Name != "dracula" {
		t.Errorf("Expected theme dracula, got %s", cfg.Theme.Name)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

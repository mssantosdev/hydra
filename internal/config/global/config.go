package global

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// GlobalConfig represents user-level configuration
type GlobalConfig struct {
	Version  string    `yaml:"version"`
	Language string    `yaml:"language"`
	Theme    ThemeConf `yaml:"theme"`
	Defaults Defaults  `yaml:"defaults"`
}

// ThemeConf holds theme configuration
type ThemeConf struct {
	Name string `yaml:"name"`
}

// Defaults holds default settings
type Defaults struct {
	Editor             string `yaml:"editor"`
	Pager              string `yaml:"pager"`
	ConfirmDestructive bool   `yaml:"confirm_destructive"`
}

// DefaultGlobalConfig returns default global configuration
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Version:  "1.0",
		Language: "en-US",
		Theme: ThemeConf{
			Name: "tokyonight",
		},
		Defaults: Defaults{
			Editor:             "code",
			Pager:              "less",
			ConfirmDestructive: true,
		},
	}
}

// GetConfigDir returns the platform-specific config directory
func GetConfigDir() string {
	switch runtime.GOOS {
	case "darwin":
		// macOS: ~/Library/Application Support/hydra/
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "hydra")
	case "windows":
		// Windows: %APPDATA%/hydra/
		appdata := os.Getenv("APPDATA")
		if appdata == "" {
			home, _ := os.UserHomeDir()
			appdata = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(appdata, "hydra")
	default:
		// Linux and others: ~/.config/hydra/
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "hydra")
	}
}

// GetConfigPath returns the full path to the global config file
func GetConfigPath() string {
	return filepath.Join(GetConfigDir(), "config.yaml")
}

// Load loads the global config from disk
func Load() (*GlobalConfig, error) {
	configPath := GetConfigPath()

	// Check if config exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// Return defaults if not found
		return DefaultGlobalConfig(), nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read global config: %w", err)
	}

	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse global config: %w", err)
	}

	// Set defaults for missing values
	if cfg.Language == "" {
		cfg.Language = "en-US"
	}
	if cfg.Theme.Name == "" {
		cfg.Theme.Name = "tokyonight"
	}

	return &cfg, nil
}

// Save saves the global config to disk
func (c *GlobalConfig) Save() error {
	configDir := GetConfigDir()
	configPath := GetConfigPath()

	// Create directory if it doesn't exist
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal global config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write global config: %w", err)
	}

	return nil
}

// SetLanguage sets the language and saves
func (c *GlobalConfig) SetLanguage(lang string) error {
	c.Language = lang
	return c.Save()
}

// SetTheme sets the theme and saves
func (c *GlobalConfig) SetTheme(theme string) error {
	c.Theme.Name = theme
	return c.Save()
}

// SetEditor sets the default editor and saves
func (c *GlobalConfig) SetEditor(editor string) error {
	c.Defaults.Editor = editor
	return c.Save()
}

// AvailableLanguages returns list of supported languages
func AvailableLanguages() []string {
	return []string{"en-US", "pt-BR"}
}

// IsValidLanguage checks if a language is supported
func IsValidLanguage(lang string) bool {
	for _, l := range AvailableLanguages() {
		if l == lang {
			return true
		}
	}
	return false
}

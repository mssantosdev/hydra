package config

import (
    "fmt"
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

// Config represents the Hydra configuration
type Config struct {
    Version    string                 `yaml:"version"`
    Paths      Paths                  `yaml:"paths"`
    Ecosystems map[string]Ecosystem   `yaml:"ecosystems"`
    Defaults   Defaults               `yaml:"defaults,omitempty"`
}

// Paths configuration
type Paths struct {
    BareDir      string `yaml:"bare_dir"`
    WorktreeDir  string `yaml:"worktree_dir"`
}

// Ecosystem maps aliases to repo names
type Ecosystem map[string]string

// Defaults for the project
type Defaults struct {
    BaseBranch string `yaml:"base_branch,omitempty"`
}

// GlobalConfig represents user-level configuration
type GlobalConfig struct {
    Opener  string   `yaml:"opener,omitempty"`
    UI      UIConfig `yaml:"ui,omitempty"`
}

// UIConfig for theming
type UIConfig struct {
    Theme string `yaml:"theme,omitempty"`
    Mouse bool   `yaml:"mouse,omitempty"`
}

// DefaultConfig returns a new config with defaults
func DefaultConfig() *Config {
    return &Config{
        Version: "1.0",
        Paths: Paths{
            BareDir:     ".bare",
            WorktreeDir: ".",
        },
        Ecosystems: make(map[string]Ecosystem),
        Defaults: Defaults{
            BaseBranch: "stage",
        },
    }
}

// Save writes config to file
func (c *Config) Save(path string) error {
    data, err := yaml.Marshal(c)
    if err != nil {
        return fmt.Errorf("failed to marshal config: %w", err)
    }

    if err := os.WriteFile(path, data, 0644); err != nil {
        return fmt.Errorf("failed to write config: %w", err)
    }

    return nil
}

// Load reads config from file
func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("failed to read config: %w", err)
    }

    var cfg Config
    if err := yaml.Unmarshal(data, &cfg); err != nil {
        return nil, fmt.Errorf("failed to parse config: %w", err)
    }

    // Set defaults if missing
    if cfg.Paths.BareDir == "" {
        cfg.Paths.BareDir = ".bare"
    }
    if cfg.Paths.WorktreeDir == "" {
        cfg.Paths.WorktreeDir = "."
    }
    if cfg.Ecosystems == nil {
        cfg.Ecosystems = make(map[string]Ecosystem)
    }

    return &cfg, nil
}

// FindConfig searches for .hydra.yaml walking up from startDir
func FindConfig(startDir string) (string, *Config, error) {
    dir := startDir
    for dir != "/" && dir != "." {
        configPath := filepath.Join(dir, ".hydra.yaml")
        if _, err := os.Stat(configPath); err == nil {
            cfg, err := Load(configPath)
            if err != nil {
                return "", nil, err
            }
            return configPath, cfg, nil
        }
        parent := filepath.Dir(dir)
        if parent == dir {
            break
        }
        dir = parent
    }
    return "", nil, fmt.Errorf("no .hydra.yaml found in %s or parent directories", startDir)
}

// ResolveAlias converts ecosystem:alias to actual repo name
func (c *Config) ResolveAlias(ecosystem, alias string) (string, bool) {
    eco, ok := c.Ecosystems[ecosystem]
    if !ok {
        return "", false
    }
    repo, ok := eco[alias]
    return repo, ok
}

// GetAllAliases returns all aliases across all ecosystems
func (c *Config) GetAllAliases() []struct {
    Ecosystem string
    Alias     string
    Repo      string
} {
    var result []struct {
        Ecosystem string
        Alias     string
        Repo      string
    }
    
    for ecoName, eco := range c.Ecosystems {
        for alias, repo := range eco {
            result = append(result, struct {
                Ecosystem string
                Alias     string
                Repo      string
            }{ecoName, alias, repo})
        }
    }
    
    return result
}

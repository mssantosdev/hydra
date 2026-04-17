package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
)

func validateRelativeProjectPath(input string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(input))
	if clean == "" || clean == "." {
		return "", fmt.Errorf("project path cannot be empty")
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("project path must be relative")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("project path cannot escape the current directory")
	}
	return clean, nil
}

func validatePathSegment(kind, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s cannot be empty", kind)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("%s cannot be %q", kind, value)
	}
	if strings.ContainsRune(value, os.PathSeparator) || strings.Contains(value, "\\") {
		return fmt.Errorf("%s cannot contain path separators", kind)
	}
	return nil
}

func createProjectRoot(baseDir, projectPath string) (string, string, *config.Config, error) {
	cleanPath, err := validateRelativeProjectPath(projectPath)
	if err != nil {
		return "", "", nil, err
	}

	projectRoot := filepath.Join(baseDir, cleanPath)
	if err := os.MkdirAll(projectRoot, 0755); err != nil {
		return "", "", nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	configPath := filepath.Join(projectRoot, ".hydra.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return "", "", nil, fmt.Errorf("hydra project already exists at %s", projectRoot)
	}

	cfg := config.DefaultConfig()
	if err := cfg.Save(configPath); err != nil {
		return "", "", nil, fmt.Errorf("failed to save config: %w", err)
	}

	return projectRoot, configPath, cfg, nil
}

func registerRepo(cfg *config.Config, configPath, group, alias, repoName string) error {
	if cfg.Ecosystems == nil {
		cfg.Ecosystems = make(map[string]config.Ecosystem)
	}
	if cfg.Ecosystems[group] == nil {
		cfg.Ecosystems[group] = make(config.Ecosystem)
	}
	cfg.Ecosystems[group][alias] = repoName
	return cfg.Save(configPath)
}

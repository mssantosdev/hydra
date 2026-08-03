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
	if err := os.MkdirAll(projectRoot, 0750); err != nil {
		return "", "", nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	configPath := filepath.Join(projectRoot, ".hydra.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return "", "", nil, fmt.Errorf("hydra project already exists at %s", projectRoot)
	}

	cfg := config.DefaultConfig(filepath.Base(cleanPath))
	if err := cfg.Save(configPath); err != nil {
		return "", "", nil, fmt.Errorf("failed to save config: %w", err)
	}

	return projectRoot, configPath, cfg, nil
}

// createProjectRootAt writes a schema v2 .hydra.yaml directly into an existing
// directory, which is how `hydra init` and a bare `hydra clone` create a workspace
// in place rather than in a subdirectory.
func createProjectRootAt(root, projectName string) (string, string, *config.Config, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to resolve %s: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0750); err != nil {
		return "", "", nil, fmt.Errorf("failed to create project directory: %w", err)
	}

	configPath := filepath.Join(abs, ".hydra.yaml")
	if _, err := os.Stat(configPath); err == nil {
		return "", "", nil, fmt.Errorf("hydra project already exists at %s", abs)
	}

	name := strings.TrimSpace(projectName)
	if name == "" {
		name = filepath.Base(abs)
	}
	cfg := config.DefaultConfig(name)
	if err := cfg.Save(configPath); err != nil {
		return "", "", nil, fmt.Errorf("failed to save config: %w", err)
	}

	return abs, configPath, cfg, nil
}

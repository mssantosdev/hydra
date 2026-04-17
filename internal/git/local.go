package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func InitRepository(repoPath, branch string) error {
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}

	if err := runGit(repoPath, "init"); err != nil {
		return err
	}
	if err := runGit(repoPath, "checkout", "-b", branch); err != nil {
		return err
	}
	if err := runGit(repoPath, "config", "user.email", "hydra@local"); err != nil {
		return err
	}
	if err := runGit(repoPath, "config", "user.name", "Hydra"); err != nil {
		return err
	}

	readmePath := filepath.Join(repoPath, "README.md")
	projectName := filepath.Base(repoPath)
	content := "# " + strings.ReplaceAll(projectName, "-", " ") + "\n"
	if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create initial README: %w", err)
	}
	if err := runGit(repoPath, "add", "."); err != nil {
		return err
	}
	if err := runGit(repoPath, "commit", "-m", "Initial commit"); err != nil {
		return err
	}

	return nil
}

func CloneBareFromLocal(sourcePath, barePath string) error {
	cmd := exec.Command("git", "clone", "--bare", sourcePath, barePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to clone bare repository: %w", err)
	}
	return nil
}

func runGit(repoPath string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", repoPath}, args...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var checkoutCmd = &cobra.Command{
	Use:   "checkout <repo-alias> [branch-name]",
	Short: "Create or switch to a worktree",
	Long: `Create a new worktree for the specified branch, or switch to an existing one.

If branch-name is not provided, an interactive prompt will help you select or create one.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runCheckout,
}

func init() {
	rootCmd.AddCommand(checkoutCmd)
}

func runCheckout(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)
	alias := args[0]

	// Find the repo for this alias
	var repoName, ecosystem string
	for ecoName, eco := range cfg.Ecosystems {
		if r, ok := eco[alias]; ok {
			repoName = r
			ecosystem = ecoName
			break
		}
	}

	if repoName == "" {
		return fmt.Errorf("unknown alias: %s", alias)
	}

	bareRepo := filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git")

	// Check if bare repo exists
	if _, err := os.Stat(bareRepo); os.IsNotExist(err) {
		return fmt.Errorf("bare repository not found: %s", bareRepo)
	}

	var branch string
	if len(args) >= 2 {
		branch = args[1]
	} else {
		// For now, use the alias as branch name or ask
		branch = alias + "-work"
	}

	// Normalize branch name
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	worktreePath := filepath.Join(bareRepo, safeBranch)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		fmt.Println(styles.Success.Render("✓ Worktree already exists"))
		fmt.Printf("  Path: %s\n", worktreePath)
		fmt.Printf("  Branch: %s\n", branch)
	} else {
		// Create worktree
		fmt.Printf("Creating worktree for %s:%s...\n", repoName, branch)

		if err := git.CreateWorktree(bareRepo, worktreePath, branch); err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}

		fmt.Println(styles.Success.Render("✓ Worktree created"))
		fmt.Printf("  Path: %s\n", worktreePath)
		fmt.Printf("  Branch: %s\n", branch)
	}

	// Create or update symlink
	symlinkDir := filepath.Join(projectRoot, ecosystem)
	os.MkdirAll(symlinkDir, 0755)

	symlinkName := alias + "-" + safeBranch
	if safeBranch == "prod" || safeBranch == "stage" {
		symlinkName = alias + "-" + safeBranch
	}

	symlinkPath := filepath.Join(symlinkDir, symlinkName)

	// Remove existing symlink if different
	if existing, err := os.Readlink(symlinkPath); err == nil && existing != worktreePath {
		os.Remove(symlinkPath)
	}

	// Create symlink
	relPath, _ := filepath.Rel(symlinkDir, worktreePath)
	if err := os.Symlink(relPath, symlinkPath); err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	fmt.Printf("  Symlink: %s/%s\n", ecosystem, symlinkName)

	return nil
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [<repo-alias> <branch-name>]",
	Short: "Add a new worktree",
	Long: `Create a new worktree for a repository branch.

DESCRIPTION
  Creates a Git worktree - a separate working directory for a specific branch.
  Worktrees allow you to work on multiple branches simultaneously without
  stashing or committing incomplete work.

  When you run this command:
  1. Creates worktree directory: .bare/<repo>/<branch>/
  2. Creates symlink: <ecosystem>/<repo>-<branch>
  3. Checks out the specified branch (creating it if needed)

WHEN TO USE
  • Starting work on a new feature
  • Creating hotfix branches from production
  • Setting up staging or production worktrees
  • Working on multiple features in parallel

EXAMPLES
  # Interactive mode - prompts for repo and branch
  $ hydra add

  # Create worktree from current HEAD
  $ hydra add api feature-x

  # Create branch from specific base (not HEAD)
  $ hydra add api feature-y --from=develop

  # Track remote branch
  $ hydra add api feature-z --track=origin/feature-z

FLAGS
  -f, --from string    Create branch from this branch (default: HEAD)
  -t, --track string   Track remote branch
  -h, --help           Show help

EXIT CODES
  0  Success (worktree created or already exists)
  1  General error (invalid args, repo not found)
  2  Config file (.hydra.yaml) not found

SEE ALSO
  • hydra remove - Remove a worktree
  • hydra switch - Switch to a worktree
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/commands/worktree-management.md`,
	RunE: runAdd,
}

var (
	addFromBranch string
)

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVarP(&addFromBranch, "from", "f", "", "Create branch from this branch (default: HEAD)")
}

func runAdd(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)

	var alias, branch string

	// Interactive mode if no args
	if len(args) == 0 {
		var err error
		alias, branch, err = interactiveAdd(cfg)
		if err != nil {
			return err
		}
	} else if len(args) >= 2 {
		alias = args[0]
		branch = args[1]
	} else {
		return fmt.Errorf("usage: hydra add <repo-alias> <branch-name>")
	}

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

	// Normalize branch name
	safeBranch := strings.ReplaceAll(branch, "/", "-")
	worktreePath := filepath.Join(bareRepo, safeBranch)

	// Check if worktree already exists
	if _, err := os.Stat(worktreePath); err == nil {
		fmt.Println(styles.Success.Render("✓ Worktree already exists"))
		fmt.Printf("  Path: %s\n", worktreePath)
		fmt.Printf("  Branch: %s\n", branch)
		fmt.Println()
		fmt.Printf("Switch to it with: hydra switch %s-%s\n", alias, safeBranch)
		return nil
	}

	// Create worktree
	fmt.Printf("Creating worktree for %s:%s...\n", repoName, branch)

	// TODO: Handle --from flag to create branch from specific base
	if err := git.CreateWorktree(bareRepo, worktreePath, branch); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	fmt.Println(styles.Success.Render("✓ Worktree created"))
	fmt.Printf("  Path: %s\n", worktreePath)
	fmt.Printf("  Branch: %s\n", branch)

	// Create or update symlink
	symlinkDir := filepath.Join(projectRoot, ecosystem)
	os.MkdirAll(symlinkDir, 0755)

	symlinkName := alias + "-" + safeBranch
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
	fmt.Println()
	fmt.Printf("Switch to it with: hydra switch %s-%s\n", alias, safeBranch)

	return nil
}

func interactiveAdd(cfg *config.Config) (string, string, error) {
	// Build list of available repos
	var repoOptions []huh.Option[string]
	for ecoName, eco := range cfg.Ecosystems {
		for alias := range eco {
			label := fmt.Sprintf("%s (%s)", alias, ecoName)
			repoOptions = append(repoOptions, huh.NewOption(label, alias))
		}
	}

	if len(repoOptions) == 0 {
		return "", "", fmt.Errorf("no repositories found in config")
	}

	var selectedAlias string
	var branchName string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Repository").
				Description("Choose which repository to add a worktree for").
				Options(repoOptions...).
				Value(&selectedAlias),

			huh.NewInput().
				Title("Branch Name").
				Description("Enter the branch name for the new worktree").
				Placeholder("feature/my-feature").
				Value(&branchName).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("branch name cannot be empty")
					}
					return nil
				}),
		),
	)

	if err := form.Run(); err != nil {
		return "", "", err
	}

	return selectedAlias, branchName, nil
}

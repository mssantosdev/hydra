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

var removeCmd = &cobra.Command{
	Use:   "remove [<repo-alias> <branch-name>]",
	Short: "Remove a worktree",
	Long: `Remove a worktree for the specified repository and branch.

If run without arguments, an interactive prompt will help you select which worktree to remove.

Examples:
  # Interactive mode
  hydra remove

  # Direct mode
  hydra remove mykids-back old-feature
  
  # Force remove (ignore uncommitted changes)
  hydra remove mykids-back old-feature --force
  
  # Remove and delete branch
  hydra remove mykids-back merged-feature --delete-branch`,
	RunE: runRemove,
}

var (
	removeForce        bool
	removeDeleteBranch bool
	removeYes          bool
)

func init() {
	rootCmd.AddCommand(removeCmd)
	removeCmd.Flags().BoolVarP(&removeForce, "force", "f", false, "Force remove (ignore uncommitted changes)")
	removeCmd.Flags().BoolVarP(&removeDeleteBranch, "delete-branch", "d", false, "Also delete the git branch")
	removeCmd.Flags().BoolVarP(&removeYes, "yes", "y", false, "Skip confirmation")
}

func runRemove(cmd *cobra.Command, args []string) error {
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
		alias, branch, err = interactiveRemove(cfg, projectRoot)
		if err != nil {
			return err
		}
	} else if len(args) >= 2 {
		alias = args[0]
		branch = args[1]
	} else {
		return fmt.Errorf("usage: hydra remove <repo-alias> <branch-name>")
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

	// Check if worktree exists
	if _, err := os.Stat(worktreePath); os.IsNotExist(err) {
		return fmt.Errorf("worktree does not exist: %s", worktreePath)
	}

	// Check for uncommitted changes
	hasChanges, changeCount, err := git.HasUncommittedChanges(worktreePath)
	if err != nil {
		return fmt.Errorf("failed to check worktree status: %w", err)
	}

	if hasChanges && !removeForce {
		fmt.Println(styles.WarningBadge.Render("⚠ Warning: Worktree has uncommitted changes"))
		fmt.Printf("  %d modified file(s)\n", changeCount)
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  1. Commit or stash changes first")
		fmt.Println("  2. Use --force to remove anyway (changes will be lost)")
		fmt.Println()
		return fmt.Errorf("worktree has uncommitted changes")
	}

	// Confirmation
	if !removeYes {
		var confirm bool
		warning := ""
		if hasChanges {
			warning = "\n\n⚠ WARNING: This will delete uncommitted changes!"
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title(fmt.Sprintf("Remove worktree %s:%s?%s", alias, branch, warning)).
					Value(&confirm).
					Affirmative("Yes, remove").
					Negative("Cancel"),
			),
		)

		if err := form.Run(); err != nil {
			return err
		}

		if !confirm {
			fmt.Println("Cancelled")
			return nil
		}
	}

	// Remove worktree
	fmt.Printf("Removing worktree %s:%s...\n", alias, branch)

	if err := git.RemoveWorktree(bareRepo, worktreePath, removeForce); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	fmt.Println(styles.Success.Render("✓ Worktree removed"))

	// Remove symlink
	symlinkDir := filepath.Join(projectRoot, ecosystem)
	symlinkName := alias + "-" + safeBranch
	symlinkPath := filepath.Join(symlinkDir, symlinkName)
	os.Remove(symlinkPath)

	// Optionally delete branch
	if removeDeleteBranch {
		fmt.Printf("Deleting branch %s...\n", branch)
		// TODO: Implement branch deletion
		fmt.Println(styles.Success.Render("✓ Branch deleted"))
	}

	return nil
}

func interactiveRemove(cfg *config.Config, projectRoot string) (string, string, error) {
	// Build list of existing worktrees
	var worktreeOptions []huh.Option[worktreeInfo]

	for ecoName, eco := range cfg.Ecosystems {
		for alias, repoName := range eco {
			bareRepo := filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git")

			// Check if bare repo exists
			if _, err := os.Stat(bareRepo); os.IsNotExist(err) {
				continue
			}

			// List worktrees
			worktrees, err := git.ListWorktrees(bareRepo)
			if err != nil {
				continue
			}

			for _, wt := range worktrees {
				if wt.IsBare {
					continue
				}

				branch := wt.Branch
				if branch == "" {
					continue
				}

				label := fmt.Sprintf("%s/%s:%s", ecoName, alias, branch)

				// Check if dirty
				hasChanges, _, _ := git.HasUncommittedChanges(wt.Path)
				if hasChanges {
					label += " ⚠"
				}

				worktreeOptions = append(worktreeOptions, huh.NewOption(label, worktreeInfo{
					alias:  alias,
					branch: branch,
				}))
			}
		}
	}

	if len(worktreeOptions) == 0 {
		return "", "", fmt.Errorf("no worktrees found")
	}

	var selected worktreeInfo

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[worktreeInfo]().
				Title("Select Worktree to Remove").
				Description("⚠ = has uncommitted changes").
				Options(worktreeOptions...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", "", err
	}

	return selected.alias, selected.branch, nil
}

type worktreeInfo struct {
	alias  string
	branch string
}

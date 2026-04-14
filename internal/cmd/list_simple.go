package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List all worktrees",
	Long:  "Display all worktrees organized by ecosystem with their current status.",
	RunE:  runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)

	// Get terminal width for dynamic layout
	termWidth := styles.GetTerminalWidth()
	const (
		indentWidth    = 2  // "  " at start of line
		statusWidth    = 13 // "[  ✓ clean  ]" or "[  ~ 99 chg  ]"
		spacing        = 2  // spaces between name and branch
		maxBranchWidth = 20 // max branch name before truncation
		minNameWidth   = 15 // minimum worktree name width
	)

	// Calculate max worktree name width
	maxNameWidth := termWidth - indentWidth - statusWidth - spacing - maxBranchWidth
	if maxNameWidth < minNameWidth {
		maxNameWidth = minNameWidth
	}

	// Create styles
	cleanBadge := lipgloss.NewStyle().
		Background(styles.Green).
		Foreground(styles.BgDark).
		Bold(true).
		Width(statusWidth).
		Align(lipgloss.Center)
	modifiedBadge := lipgloss.NewStyle().
		Background(styles.Yellow).
		Foreground(styles.BgDark).
		Bold(true).
		Width(statusWidth).
		Align(lipgloss.Center)
	nameStyle := lipgloss.NewStyle().Foreground(styles.FgBright)
	branchStyle := lipgloss.NewStyle().Foreground(styles.Purple)

	// Print header
	fmt.Println(styles.AppHeader.Render("HYDRA"))
	fmt.Println(styles.Title.Render("Worktree Status"))
	fmt.Println()

	// Iterate through ecosystems
	for ecoName, ecosystem := range cfg.Ecosystems {
		ecoHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Cyan).
			Render("▸ " + strings.ToUpper(ecoName))
		fmt.Println(styles.EcosystemHeader.Render(ecoHeader))

		for alias, repoName := range ecosystem {
			bareRepo := filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git")

			// Check if bare repo exists
			if _, err := os.Stat(bareRepo); os.IsNotExist(err) {
				warningText := styles.WarningBadge.Render("not migrated")
				fmt.Printf("  %s %s\n",
					styles.Dimmed.Render("○"),
					styles.Dimmed.Render(alias+" — "+warningText))
				continue
			}

			// List worktrees
			worktrees, err := git.ListWorktrees(bareRepo)
			if err != nil {
				errorText := styles.ErrorBadge.Render("error: " + err.Error())
				fmt.Printf("  %s %s\n",
					styles.Error.Render("✗"),
					errorText)
				continue
			}

			for _, wt := range worktrees {
				if wt.IsBare {
					continue
				}

				branch := wt.Branch
				if branch == "" {
					branch = "detached"
				}

				// Check for modifications
				hasMod, count, _ := git.HasUncommittedChanges(wt.Path)

				worktreeName := filepath.Base(wt.Path)
				if worktreeName == "prod" {
					worktreeName = alias + "-prod"
				} else if worktreeName == "stage" {
					worktreeName = alias + "-stage"
				} else if worktreeName == repoName+".git" {
					continue // Skip weird paths
				} else {
					worktreeName = alias + "-" + worktreeName
				}

				// Truncate names if too long
				displayName := styles.Truncate(worktreeName, maxNameWidth)
				displayBranch := styles.Truncate(branch, maxBranchWidth)

				// Build output
				var statusStr string
				if hasMod {
					statusStr = modifiedBadge.Render(fmt.Sprintf("~ %d chg", count))
				} else {
					statusStr = cleanBadge.Render("✓ clean")
				}

				// Compact format: status + name + branch with minimal spacing
				fmt.Printf("  %s %s  %s\n",
					statusStr,
					nameStyle.Render(displayName),
					branchStyle.Render(displayBranch))
			}
		}
		fmt.Println()
	}

	return nil
}

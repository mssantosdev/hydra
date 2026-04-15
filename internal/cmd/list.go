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
	Long:  "Display all worktrees organized by group with their current status.",
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

	// Get column widths
	_, nameWidth, branchWidth := styles.WorktreeListLayout()

	// Print header - centered with better styling
	fmt.Println()
	headerBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Blue).
		Background(styles.BgDarker).
		Padding(0, 4).
		Align(lipgloss.Center).
		Width(styles.GetTerminalWidth() - 4)

	fmt.Println(headerBox.Render(
		lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Blue).
			Render("HYDRA") + "\n" +
			lipgloss.NewStyle().
				Foreground(styles.FgComment).
				Render("Worktree Status")))
	fmt.Println()

	// Table styles with fixed widths
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(styles.Blue).
		Underline(true)

	cleanStyle := lipgloss.NewStyle().
		Background(styles.Green).
		Foreground(styles.BgDark).
		Bold(true).
		Padding(0, 1).
		Width(10)

	dirtyStyle := lipgloss.NewStyle().
		Background(styles.Yellow).
		Foreground(styles.BgDark).
		Bold(true).
		Padding(0, 1).
		Width(10)

	// Iterate through groups
	hasWorktrees := false
	for groupName, group := range cfg.Ecosystems {
		groupHasWorktrees := false
		var rows [][]string

		for alias, repoName := range group {
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

				groupHasWorktrees = true
				hasWorktrees = true

				branch := wt.Branch
				if branch == "" {
					branch = "detached"
				}

				// Check for modifications
				hasMod, count, _ := git.HasUncommittedChanges(wt.Path)

				// Build status
				var status string
				if hasMod {
					status = dirtyStyle.Render(fmt.Sprintf("~%d", count))
				} else {
					status = cleanStyle.Render("✓clean")
				}

				worktreeName := filepath.Base(wt.Path)
				if worktreeName == repoName+".git" {
					continue
				}
				if worktreeName == "main" || worktreeName == "master" {
					worktreeName = alias
				} else {
					worktreeName = alias + "-" + worktreeName
				}

				rows = append(rows, []string{
					worktreeName,
					branch,
					status,
				})
			}
		}

		if !groupHasWorktrees {
			continue
		}

		// Print group header
		groupHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(styles.Cyan).
			BorderStyle(lipgloss.ThickBorder()).
			BorderBottom(true).
			BorderForeground(styles.Blue).
			MarginBottom(0)
		fmt.Println(groupHeader.Render("▸ " + strings.ToUpper(groupName)))

		// Print table header with fixed widths
		worktreeHeader := styles.PadRight("WORKTREE", nameWidth)
		branchHeader := styles.PadRight("BRANCH", branchWidth)
		fmt.Printf("  %s  %s  %s\n",
			headerStyle.Render(worktreeHeader),
			headerStyle.Render(branchHeader),
			headerStyle.Render("STATUS"))

		// Print separator
		sepWidth := nameWidth + branchWidth + 10 + 4
		fmt.Printf("  %s\n", strings.Repeat("─", sepWidth))

		// Print rows with fixed column widths
		for _, row := range rows {
			worktreeName := styles.Truncate(row[0], nameWidth)
			branch := styles.Truncate(row[1], branchWidth)

			paddedWorktree := styles.PadRight(worktreeName, nameWidth)
			paddedBranch := styles.PadRight(branch, branchWidth)

			fmt.Printf("  %s  %s  %s\n",
				paddedWorktree,
				styles.Branch.Render(paddedBranch),
				row[2])
		}

		fmt.Println()
	}

	if !hasWorktrees {
		fmt.Println(styles.Dimmed.Render("  No worktrees found."))
		fmt.Println()
		fmt.Println("  Run 'hydra clone <url>' to add a repository.")
	}

	return nil
}

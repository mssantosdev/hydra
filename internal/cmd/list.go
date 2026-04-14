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

	// Print header
	fmt.Println()
	fmt.Println(styles.AppHeader.Render(" HYDRA "))
	fmt.Println()
	fmt.Println(styles.Title.Render("Worktree Status"))
	fmt.Println()

	// Table styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7aa2f7")).
		Padding(0, 1)

	cellStyle := lipgloss.NewStyle().
		Padding(0, 1)

	cleanStyle := lipgloss.NewStyle().
		Background(styles.Green).
		Foreground(styles.BgDark).
		Bold(true).
		Padding(0, 1)

	dirtyStyle := lipgloss.NewStyle().
		Background(styles.Yellow).
		Foreground(styles.BgDark).
		Bold(true).
		Padding(0, 1)

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
					status = dirtyStyle.Render(fmt.Sprintf("~ %d", count))
				} else {
					status = cleanStyle.Render("✓ clean")
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
		fmt.Println(styles.EcosystemHeader.Render("▸ " + strings.ToUpper(groupName)))
		fmt.Println()

		// Print table header
		fmt.Printf("  %s  %s  %s\n",
			headerStyle.Render("WORKTREE"),
			headerStyle.Render("BRANCH"),
			headerStyle.Render("STATUS"))

		// Print separator
		fmt.Printf("  %s\n", strings.Repeat("─", 50))

		// Print rows
		for _, row := range rows {
			fmt.Printf("  %s  %s  %s\n",
				cellStyle.Render(row[0]),
				cellStyle.Render(styles.Branch.Render(row[1])),
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

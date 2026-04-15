package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show worktree status overview",
	Long:  "Display a quick overview of all worktrees and their status.",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)

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
				Render("Status Overview")))
	fmt.Println()

	totalWorktrees := 0
	cleanCount := 0
	modifiedCount := 0

	// Count worktrees
	for _, ecosystem := range cfg.Ecosystems {
		for _, repoName := range ecosystem {
			bareRepo := filepath.Join(projectRoot, cfg.Paths.BareDir, repoName+".git")

			if _, err := os.Stat(bareRepo); os.IsNotExist(err) {
				continue
			}

			worktrees, err := git.ListWorktrees(bareRepo)
			if err != nil {
				continue
			}

			for _, wt := range worktrees {
				if wt.IsBare {
					continue
				}
				totalWorktrees++

				hasMod, _, _ := git.HasUncommittedChanges(wt.Path)
				if hasMod {
					modifiedCount++
				} else {
					cleanCount++
				}
			}
		}
	}

	// Modern stat boxes
	totalBox := styles.TotalBadge.Render(fmt.Sprintf("TOTAL %d", totalWorktrees))
	cleanBox := styles.CleanBadge.Render(fmt.Sprintf("CLEAN %d", cleanCount))
	modifiedBox := styles.ModifiedBadge.Render(fmt.Sprintf("MODIFIED %d", modifiedCount))

	fmt.Println(styles.StatBox.Render(totalBox + "  " + cleanBox + "  " + modifiedBox))
	fmt.Println()

	// Show quick navigation paths
	fmt.Println(styles.Label.Render("Quick navigation:"))

	for ecoName, ecosystem := range cfg.Ecosystems {
		for alias := range ecosystem {
			symlinkDir := filepath.Join(projectRoot, ecoName)

			// Check common worktree names
			for _, suffix := range []string{"", "-stage", "-prod"} {
				linkPath := filepath.Join(symlinkDir, alias+suffix)
				if _, err := os.Stat(linkPath); err == nil {
					relPath, _ := filepath.Rel(wd, linkPath)
					fmt.Printf("  cd %s\n", relPath)
				}
			}
		}
	}

	return nil
}

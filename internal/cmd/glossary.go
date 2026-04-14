package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var glossaryCmd = &cobra.Command{
	Use:   "glossary",
	Short: "Show glossary of Hydra terms",
	Long:  "Display explanations for all Hydra terminology and concepts.",
	RunE:  runGlossary,
}

// GlossaryEntry represents a single glossary entry
type GlossaryEntry struct {
	Term        string
	Description string
	Examples    []string
}

var glossary = []GlossaryEntry{
	{
		Term:        "Group",
		Description: "A category that organizes related repositories. Groups help you navigate between different parts of your project.",
		Examples:    []string{"backend (APIs, services)", "frontend (web apps)", "infra (Terraform, Docker configs)"},
	},
	{
		Term:        "Alias",
		Description: "A short name for a repository within its group. This is how you refer to and navigate to the repository.",
		Examples:    []string{"cd backend/api", "hydra sync worker"},
	},
	{
		Term:        "Worktree",
		Description: "A Git feature that allows multiple working directories from a single repository. Each branch can have its own worktree.",
		Examples:    []string{"main/ - production code", "develop/ - active development", "feature/login/ - new feature"},
	},
	{
		Term:        "Bare Repository",
		Description: "A Git repository without a working directory. It stores all Git history and is kept in .bare/. All worktrees share this single source.",
		Examples:    []string{"Stored in: .bare/my-repo.git/"},
	},
	{
		Term:        "Hydra Project",
		Description: "A directory containing a .hydra.yaml configuration file. This is the root where Hydra manages all your repositories.",
		Examples:    []string{"Contains: .hydra.yaml, .bare/, group folders"},
	},
}

func init() {
	rootCmd.AddCommand(glossaryCmd)
}

func runGlossary(cmd *cobra.Command, args []string) error {
	// Header
	fmt.Println()
	fmt.Println(styles.AppHeader.Render(" HYDRA "))
	fmt.Println()
	fmt.Println(styles.Title.Render("Glossary"))
	fmt.Println()

	// Introduction
	introStyle := lipgloss.NewStyle().
		Foreground(styles.Fg).
		MarginBottom(1)

	fmt.Println(introStyle.Render("Hydra uses Git worktrees to help you work on multiple branches simultaneously."))
	fmt.Println(introStyle.Render("Here's what each term means:"))
	fmt.Println()

	// Term styles
	termStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7aa2f7")).
		MarginBottom(0)

	descStyle := lipgloss.NewStyle().
		Foreground(styles.Fg).
		MarginLeft(2).
		MarginBottom(0)

	exampleStyle := lipgloss.NewStyle().
		Foreground(styles.FgComment).
		MarginLeft(4).
		Italic(true)

	// Print each entry
	for i, entry := range glossary {
		// Term
		fmt.Println(termStyle.Render(entry.Term))

		// Description
		fmt.Println(descStyle.Render(entry.Description))

		// Examples
		if len(entry.Examples) > 0 {
			fmt.Println(exampleStyle.Render("Examples:"))
			for _, ex := range entry.Examples {
				fmt.Println(exampleStyle.Render("  • " + ex))
			}
		}

		// Separator between entries
		if i < len(glossary)-1 {
			fmt.Println()
			fmt.Println(strings.Repeat("─", 60))
			fmt.Println()
		}
	}

	// Footer with keyboard shortcuts
	fmt.Println()
	fmt.Println(strings.Repeat("─", 60))
	fmt.Println()
	fmt.Println(styles.Label.Render("Quick Reference:"))
	fmt.Println()

	commands := []struct {
		cmd  string
		desc string
	}{
		{"hydra clone <url>", "Clone a new repository"},
		{"hydra list", "Show all worktrees"},
		{"hydra sync", "Pull updates for worktrees"},
		{"hydra checkout <alias> <branch>", "Create a new worktree for a branch"},
	}

	cmdStyle := lipgloss.NewStyle().
		Foreground(styles.Cyan).
		Bold(true)

	for _, c := range commands {
		fmt.Printf("  %s %s\n", cmdStyle.Render(c.cmd), styles.Dimmed.Render("- "+c.desc))
	}

	fmt.Println()

	return nil
}

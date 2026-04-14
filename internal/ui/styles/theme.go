package styles

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Tokyo Night theme colors - exported for use across the app
var (
	// Backgrounds
	BgDark   = lipgloss.Color("#1a1b26")
	BgDarker = lipgloss.Color("#16161e")
	BgLight  = lipgloss.Color("#24283b")

	// Foregrounds
	Fg        = lipgloss.Color("#a9b1d6")
	FgBright  = lipgloss.Color("#c0caf5")
	FgComment = lipgloss.Color("#565f89")

	// Accents
	Blue   = lipgloss.Color("#7aa2f7")
	Cyan   = lipgloss.Color("#7dcfff")
	Green  = lipgloss.Color("#9ece6a")
	Orange = lipgloss.Color("#ff9e64")
	Pink   = lipgloss.Color("#bb9af7")
	Purple = lipgloss.Color("#9d7cd8")
	Red    = lipgloss.Color("#f7768e")
	Yellow = lipgloss.Color("#e0af68")
)

// Common styles used across the application
var (
	// App Header
	AppHeader = lipgloss.NewStyle().
			Background(Blue).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 2)

	// Title styles
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Blue).
		MarginBottom(1)

	Subtitle = lipgloss.NewStyle().
			Foreground(FgComment).
			MarginBottom(1)

	// Status badges
	CleanBadge = lipgloss.NewStyle().
			Background(Green).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 1)

	ModifiedBadge = lipgloss.NewStyle().
			Background(Yellow).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 1)

	ErrorBadge = lipgloss.NewStyle().
			Background(Red).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 1)

	WarningBadge = lipgloss.NewStyle().
			Background(Orange).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 1)

	// Ecosystem header
	EcosystemHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(Cyan).
			BorderStyle(lipgloss.ThickBorder()).
			BorderBottom(true).
			BorderForeground(Blue).
			PaddingBottom(0)

	// Text styles
	Branch = lipgloss.NewStyle().
		Foreground(Purple)

	Dimmed = lipgloss.NewStyle().
		Foreground(FgComment)

	// Labels
	Label = lipgloss.NewStyle().
		Bold(true).
		Foreground(FgBright)

	// Help text
	HelpKey = lipgloss.NewStyle().
		Bold(true).
		Foreground(Pink)

	HelpDesc = lipgloss.NewStyle().
			Foreground(FgComment)

	// Error/Success
	Error = lipgloss.NewStyle().
		Foreground(Red).
		Bold(true)

	Success = lipgloss.NewStyle().
		Foreground(Green).
		Bold(true)

	// Box/Panel
	Box = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(Blue).
		Padding(1).
		Background(BgDarker)

	// Stats
	StatBox = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(FgComment).
		Padding(0, 1)

	TotalBadge = lipgloss.NewStyle().
			Background(Blue).
			Foreground(BgDark).
			Bold(true).
			Padding(0, 1)

	// Prompts
	Prompt = lipgloss.NewStyle().
		Foreground(Pink)
)

// StatusBadge returns the appropriate badge for a status (fixed width)
func StatusBadge(isClean bool, count int) string {
	if isClean {
		return CleanBadge.Render("  ✓ clean  ")
	}
	return ModifiedBadge.Render(fmt.Sprintf(" ~ %d chg  ", count))
}

// GetTerminalWidth returns the current terminal width, or 80 if not a terminal
func GetTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width == 0 {
		return 80
	}
	return width
}

// Truncate truncates a string to maxLen, adding "..." if truncated
func Truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// WorktreeListLayout calculates column widths for the worktree list
func WorktreeListLayout() (statusWidth, nameWidth, branchWidth int) {
	termWidth := GetTerminalWidth()

	// Fixed widths (in characters, not cells - approximate)
	statusWidth = 13 // "[  ✓ clean  ]" - fixed width badge
	branchWidth = 15 // "feat-branch-name"
	spacing := 2     // Spaces between columns

	// Calculate remaining space for name
	nameWidth = termWidth - statusWidth - branchWidth - spacing - 2 // -2 for left indent

	// Minimum name width
	if nameWidth < 20 {
		nameWidth = 20
	}

	return statusWidth, nameWidth, branchWidth
}

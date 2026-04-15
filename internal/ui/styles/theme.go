package styles

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/ui/themes"
	"golang.org/x/term"
)

// Theme colors - these will be populated from the selected theme
var (
	// Backgrounds
	BgDark   lipgloss.Color
	BgDarker lipgloss.Color
	BgLight  lipgloss.Color

	// Foregrounds
	Fg        lipgloss.Color
	FgBright  lipgloss.Color
	FgComment lipgloss.Color

	// Accents
	Blue   lipgloss.Color
	Cyan   lipgloss.Color
	Green  lipgloss.Color
	Orange lipgloss.Color
	Pink   lipgloss.Color
	Purple lipgloss.Color
	Red    lipgloss.Color
	Yellow lipgloss.Color
)

// Styles - will be initialized with theme colors
var (
	// App Header
	AppHeader lipgloss.Style

	// Centered header
	CenteredHeader lipgloss.Style

	// Title styles
	Title    lipgloss.Style
	Subtitle lipgloss.Style

	// Status badges
	CleanBadge    lipgloss.Style
	ModifiedBadge lipgloss.Style
	ErrorBadge    lipgloss.Style
	WarningBadge  lipgloss.Style

	// Ecosystem header
	EcosystemHeader lipgloss.Style

	// Text styles
	Branch lipgloss.Style
	Dimmed lipgloss.Style

	// Labels
	Label lipgloss.Style

	// Help text
	HelpKey  lipgloss.Style
	HelpDesc lipgloss.Style

	// Error/Success
	Error   lipgloss.Style
	Success lipgloss.Style

	// Box/Panel
	Box lipgloss.Style

	// Stats
	StatBox    lipgloss.Style
	TotalBadge lipgloss.Style

	// Prompts
	Prompt lipgloss.Style

	// Table styles
	TableHeader lipgloss.Style
	TableCell   lipgloss.Style
	TableBorder lipgloss.Style

	// Compact mode
	Compact lipgloss.Style
)

// init loads the global config and applies the selected theme
func init() {
	loadTheme()
}

// loadTheme reads the global config and applies the selected theme
func loadTheme() {
	// Load global config (ignore errors, use defaults)
	cfg, err := global.Load()
	if err != nil {
		cfg = global.DefaultGlobalConfig()
	}

	// Get theme
	theme := themes.Get(cfg.Theme.Name)

	// Apply theme colors
	applyTheme(theme)
}

// applyTheme sets all color variables and styles from a theme
func applyTheme(theme themes.Theme) {
	// Set colors from theme
	BgDark = theme.Background
	BgDarker = darken(theme.Background)
	BgLight = lighten(theme.Background)
	Fg = theme.Foreground
	FgBright = theme.Highlight
	FgComment = theme.Muted
	Blue = theme.Primary
	Cyan = theme.Secondary
	Green = theme.Success
	Orange = theme.Warning
	Pink = theme.Highlight
	Purple = theme.Secondary
	Red = theme.Error
	Yellow = theme.Warning

	// Initialize styles with theme colors
	initStyles()
}

// darken returns a darker version of a color (simple approximation)
func darken(c lipgloss.Color) lipgloss.Color {
	// For now, return the same color
	// In a full implementation, this would darken the color
	return c
}

// lighten returns a lighter version of a color (simple approximation)
func lighten(c lipgloss.Color) lipgloss.Color {
	// For now, return the same color
	// In a full implementation, this would lighten the color
	return c
}

// initStyles initializes all styles with current theme colors
func initStyles() {
	// App Header
	AppHeader = lipgloss.NewStyle().
		Background(Blue).
		Foreground(BgDark).
		Bold(true).
		Padding(0, 3)

	// Centered header
	CenteredHeader = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(Blue).
		Background(BgDarker).
		Padding(1, 3).
		Align(lipgloss.Center)

	// Title styles
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Blue).
		MarginTop(1).
		MarginBottom(1)

	Subtitle = lipgloss.NewStyle().
		Foreground(FgComment).
		MarginBottom(0)

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

	// Table styles
	TableHeader = lipgloss.NewStyle().
		Bold(true).
		Foreground(Blue).
		Underline(true)

	TableCell = lipgloss.NewStyle().
		Foreground(Fg)

	TableBorder = lipgloss.NewStyle().
		Foreground(BgLight)

	// Compact mode
	Compact = lipgloss.NewStyle().
		MarginTop(0).
		MarginBottom(0)
}

// ReloadTheme reloads the theme from config (call after changing theme)
func ReloadTheme() {
	loadTheme()
}

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
// Returns fixed widths for consistent table alignment
func WorktreeListLayout() (statusWidth, nameWidth, branchWidth int) {
	termWidth := GetTerminalWidth()

	// Fixed column widths
	statusWidth = 12 // Fixed width for status badges
	branchWidth = 20 // Fixed width for branch names
	spacing := 2     // Spaces between columns
	indent := 2      // Left indent

	// Calculate remaining space for name
	nameWidth = termWidth - statusWidth - branchWidth - spacing - indent

	// Set reasonable min/max
	if nameWidth < 20 {
		nameWidth = 20
	}
	if nameWidth > 50 {
		nameWidth = 50
	}

	return statusWidth, nameWidth, branchWidth
}

// FormatTableRow formats a table row with fixed column widths
func FormatTableRow(worktree, branch, status string) string {
	_, nameWidth, branchWidth := WorktreeListLayout()

	// Truncate fields
	worktree = Truncate(worktree, nameWidth)
	branch = Truncate(branch, branchWidth)

	// Pad fields to fixed widths
	worktree = PadRight(worktree, nameWidth)
	branch = PadRight(branch, branchWidth)

	return fmt.Sprintf("  %s  %s  %s", worktree, branch, status)
}

// PadRight pads a string to the right with spaces (exported for use in commands)
func PadRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	padding := make([]rune, width-len([]rune(s)))
	for i := range padding {
		padding[i] = ' '
	}
	return s + string(padding)
}

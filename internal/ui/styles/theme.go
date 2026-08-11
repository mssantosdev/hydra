package styles

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/ui/themes"
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
	// Centered header

	// Title styles
	// Header is a heading. It replaces AppHeader, which painted a background colour
	// that no terminal program owns; a heading is a foreground weight decision.
	Header   lipgloss.Style
	Title    lipgloss.Style
	Subtitle lipgloss.Style

	// Group header
	GroupHeader lipgloss.Style

	// Text styles
	Branch lipgloss.Style
	Dimmed lipgloss.Style

	// Labels
	Label lipgloss.Style

	// Help text

	// Error/Success
	Error   lipgloss.Style
	Success lipgloss.Style

	// Box/Panel

	// Stats
	StatBox    lipgloss.Style
	TotalBadge lipgloss.Style

	// Prompts

	// Table styles

	// Compact mode
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

	theme := themes.Get(cfg.Theme.Name)

	// Publish the resolved theme as well as applying it. themes.Current must reflect the
	// user's configured choice, not a package-default palette, so every consumer reads the
	// same resolved theme as interactive views.
	themes.Set(theme.Name)
	applyTheme(theme)
}

// applyTheme sets all color variables and styles from a theme
func applyTheme(theme themes.Theme) {
	// Set colors from theme
	BgDark = theme.Background
	BgDarker = theme.Background
	BgLight = theme.Background
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

// SetColorEnabled gates every style in the tree through one lever. Disabling it
// downgrades the renderer to plain ASCII, so no inline style can leak ANSI into
// piped or NO_COLOR output.
func SetColorEnabled(enabled bool) {
	if enabled {
		lipgloss.SetColorProfile(termenv.ColorProfile())
		return
	}
	lipgloss.SetColorProfile(termenv.Ascii)
}

// initStyles initializes all styles with current theme colors
func initStyles() {
	// Centered header

	Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(FgBright)

	// Title styles
	Title = lipgloss.NewStyle().
		Bold(true).
		Foreground(Blue).
		MarginTop(1).
		MarginBottom(1)

	Subtitle = lipgloss.NewStyle().
		Foreground(FgComment).
		MarginBottom(0)

	// Group header
	GroupHeader = lipgloss.NewStyle().
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

	// Error/Success
	Error = lipgloss.NewStyle().
		Foreground(Red).
		Bold(true)

	Success = lipgloss.NewStyle().
		Foreground(Green).
		Bold(true)

	// Box/Panel

	// Stats
	StatBox = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(FgComment).
		Padding(0, 1)

	TotalBadge = lipgloss.NewStyle().
		Foreground(Blue).
		Bold(true).
		Padding(0, 1)

	// Prompts

	// Table styles

	// Compact mode
}

// ReloadTheme reloads the theme from config (call after changing theme)
func ReloadTheme() {
	loadTheme()
}

// StatusBadge returns a foreground-only status label. A terminal program does not
// own the user's background, so status is glyph + word in a foreground colour
// rather than a filled chip.
func StatusBadge(isClean bool, count int) string {
	if isClean {
		return lipgloss.NewStyle().Foreground(Green).Render("✓ clean")
	}
	return lipgloss.NewStyle().Foreground(Yellow).Render(fmt.Sprintf("~ %d chg", count))
}

// GetTerminalWidth returns the current terminal width, or 80 if not a terminal
func GetTerminalWidth() int {
	// COLUMNS wins whenever it is set and parseable, TTY or not — that is the
	// convention, and without it a piped caller cannot control width at all: term.GetSize
	// fails on a pipe and falls back to 80, so a fixed-width consumer had no way to ask
	// for anything narrower.
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if cols, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && cols > 0 {
			return cols
		}
	}
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width == 0 {
		return 80
	}
	return width
}

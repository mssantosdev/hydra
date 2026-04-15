package themes

import "github.com/charmbracelet/lipgloss"

// Theme represents a color theme
type Theme struct {
	Name       string
	Background lipgloss.Color
	Foreground lipgloss.Color
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	Muted      lipgloss.Color
	Border     lipgloss.Color
	Highlight  lipgloss.Color
}

// Available themes
var (
	// TokyoNight is the default theme (dark blue-based)
	TokyoNight = Theme{
		Name:       "tokyonight",
		Background: "#1a1b26",
		Foreground: "#a9b1d6",
		Primary:    "#7aa2f7",
		Secondary:  "#bb9af7",
		Success:    "#9ece6a",
		Warning:    "#e0af68",
		Error:      "#f7768e",
		Muted:      "#565f89",
		Border:     "#24283b",
		Highlight:  "#c0caf5",
	}

	// Catppuccin Mocha (warm dark)
	Catppuccin = Theme{
		Name:       "catppuccin",
		Background: "#1e1e2e",
		Foreground: "#cdd6f4",
		Primary:    "#89b4fa",
		Secondary:  "#cba6f7",
		Success:    "#a6e3a1",
		Warning:    "#f9e2af",
		Error:      "#f38ba8",
		Muted:      "#6c7086",
		Border:     "#313244",
		Highlight:  "#f5c2e7",
	}

	// Dracula (purple-based dark)
	Dracula = Theme{
		Name:       "dracula",
		Background: "#282a36",
		Foreground: "#f8f8f2",
		Primary:    "#bd93f9",
		Secondary:  "#ff79c6",
		Success:    "#50fa7b",
		Warning:    "#f1fa8c",
		Error:      "#ff5555",
		Muted:      "#6272a4",
		Border:     "#44475a",
		Highlight:  "#8be9fd",
	}

	// Nord (arctic bluish)
	Nord = Theme{
		Name:       "nord",
		Background: "#2e3440",
		Foreground: "#d8dee9",
		Primary:    "#88c0d0",
		Secondary:  "#b48ead",
		Success:    "#a3be8c",
		Warning:    "#ebcb8b",
		Error:      "#bf616a",
		Muted:      "#4c566a",
		Border:     "#3b4252",
		Highlight:  "#81a1c1",
	}

	// OneDark (VS Code dark)
	OneDark = Theme{
		Name:       "onedark",
		Background: "#282c34",
		Foreground: "#abb2bf",
		Primary:    "#61afef",
		Secondary:  "#c678dd",
		Success:    "#98c379",
		Warning:    "#e5c07b",
		Error:      "#e06c75",
		Muted:      "#5c6370",
		Border:     "#3e4451",
		Highlight:  "#56b6c2",
	}
)

// Themes map for lookup
var Themes = map[string]Theme{
	"tokyonight": TokyoNight,
	"catppuccin": Catppuccin,
	"dracula":    Dracula,
	"nord":       Nord,
	"onedark":    OneDark,
}

// Get returns a theme by name (defaults to tokyonight)
func Get(name string) Theme {
	if theme, ok := Themes[name]; ok {
		return theme
	}
	return TokyoNight
}

// GetNames returns all available theme names
func GetNames() []string {
	names := make([]string, 0, len(Themes))
	for name := range Themes {
		names = append(names, name)
	}
	return names
}

// IsValid checks if a theme name is valid
func IsValid(name string) bool {
	_, ok := Themes[name]
	return ok
}

// Current holds the currently active theme
var Current = TokyoNight

// Set sets the current theme
func Set(name string) {
	Current = Get(name)
}

// Preview returns a short preview string for a theme
func (t Theme) Preview() string {
	return lipgloss.NewStyle().
		Background(t.Background).
		Foreground(t.Primary).
		Render(" Aa ") +
		lipgloss.NewStyle().
			Background(t.Background).
			Foreground(t.Success).
			Render(" ✓ ") +
		lipgloss.NewStyle().
			Background(t.Background).
			Foreground(t.Error).
			Render(" ✗ ")
}

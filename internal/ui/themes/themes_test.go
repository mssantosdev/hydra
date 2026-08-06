package themes

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestTokyoNightTheme(t *testing.T) {
	theme := TokyoNight

	if theme.Name != "tokyonight" {
		t.Errorf("Expected name tokyonight, got %s", theme.Name)
	}

	if theme.Background != "#1a1b26" {
		t.Errorf("Expected specific background color, got %s", theme.Background)
	}

	if theme.Primary != "#7aa2f7" {
		t.Errorf("Expected specific primary color, got %s", theme.Primary)
	}
}

func TestGet(t *testing.T) {
	// Test valid themes
	tests := []struct {
		name     string
		expected string
	}{
		{"tokyonight", "tokyonight"},
		{"catppuccin", "catppuccin"},
		{"dracula", "dracula"},
		{"nord", "nord"},
		{"onedark", "onedark"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			theme := Get(tt.name)
			if theme.Name != tt.expected {
				t.Errorf("Get(%s) returned theme %s, expected %s", tt.name, theme.Name, tt.expected)
			}
		})
	}
}

func TestGetInvalidTheme(t *testing.T) {
	// An unknown name falls back to the first-party default, never to a borrowed palette.
	theme := Get("invalid-theme")

	if theme.Name != "hydra" {
		t.Errorf("Expected default theme hydra for invalid input, got %s", theme.Name)
	}
}

// The default must be the one palette this project owns. Shipping a borrowed community
// palette as the face of the tool is what this asserts against.
func TestDefaultThemeIsFirstParty(t *testing.T) {
	if Current.Name != "hydra" {
		t.Errorf("Current theme is %q, want hydra", Current.Name)
	}
	if Get("").Name != "hydra" {
		t.Errorf("empty name resolved to %q, want hydra", Get("").Name)
	}
}

func TestGetNames(t *testing.T) {
	names := GetNames()

	if len(names) == 0 {
		t.Error("Should have theme names")
	}

	// Check that all themes are present
	expected := map[string]bool{
		"tokyonight": false,
		"catppuccin": false,
		"dracula":    false,
		"nord":       false,
		"onedark":    false,
	}

	for _, name := range names {
		if _, ok := expected[name]; ok {
			expected[name] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Errorf("Theme %s should be in names list", name)
		}
	}
}

func TestIsValid(t *testing.T) {
	if !IsValid("tokyonight") {
		t.Error("tokyonight should be valid")
	}

	if !IsValid("catppuccin") {
		t.Error("catppuccin should be valid")
	}

	if IsValid("invalid") {
		t.Error("invalid should not be valid")
	}

	if IsValid("") {
		t.Error("empty string should not be valid")
	}
}

func TestSet(t *testing.T) {
	// Set to dracula
	Set("dracula")

	if Current.Name != "dracula" {
		t.Errorf("Expected Current to be dracula, got %s", Current.Name)
	}

	// Reset to tokyonight
	Set("tokyonight")

	if Current.Name != "tokyonight" {
		t.Errorf("Expected Current to be tokyonight, got %s", Current.Name)
	}
}

func TestThemePreview(t *testing.T) {
	theme := TokyoNight
	preview := theme.Preview()

	// Preview should not be empty
	if preview == "" {
		t.Error("Preview should not be empty")
	}

	// Should be a lipgloss style output (contains styled text)
	// We can't easily test the exact content, but it should render
	_ = lipgloss.NewStyle().Render(preview)
}

func TestAllThemesHaveColors(t *testing.T) {
	// The semantic roles must always be set: each one carries information (a reference, a
	// topic, a state), and a theme missing one renders that information as plain text.
	semantic := func(th Theme) map[string]lipgloss.Color {
		return map[string]lipgloss.Color{
			"Primary": th.Primary, "Secondary": th.Secondary, "Success": th.Success,
			"Warning": th.Warning, "Error": th.Error, "Muted": th.Muted,
			"Border": th.Border, "Highlight": th.Highlight,
		}
	}

	for name, theme := range Themes {
		t.Run(name, func(t *testing.T) {
			for role, v := range semantic(theme) {
				if v == "" {
					t.Errorf("%s is empty; every semantic role must be set", role)
				}
			}

			// Background and Foreground are the one pair allowed to be empty, and only
			// TOGETHER. Empty means "defer to the terminal", which is what the `terminal`
			// theme does deliberately. Setting one and not the other paints half a surface:
			// a declared background under inherited text, or inherited ground under
			// declared text, either of which can render unreadable on someone else's
			// terminal. This is a coherence check, not a "must not be blank" check.
			bg, fg := theme.Background == "", theme.Foreground == ""
			if bg != fg {
				t.Errorf("Background empty=%v but Foreground empty=%v; a theme either declares both or defers both to the terminal", bg, fg)
			}
		})
	}
}

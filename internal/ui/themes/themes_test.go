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
	// Invalid theme should return TokyoNight (default)
	theme := Get("invalid-theme")

	if theme.Name != "tokyonight" {
		t.Errorf("Expected default theme tokyonight for invalid input, got %s", theme.Name)
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
	for name, theme := range Themes {
		t.Run(name, func(t *testing.T) {
			if theme.Background == "" {
				t.Error("Background should not be empty")
			}
			if theme.Foreground == "" {
				t.Error("Foreground should not be empty")
			}
			if theme.Primary == "" {
				t.Error("Primary should not be empty")
			}
			if theme.Success == "" {
				t.Error("Success should not be empty")
			}
			if theme.Error == "" {
				t.Error("Error should not be empty")
			}
		})
	}
}

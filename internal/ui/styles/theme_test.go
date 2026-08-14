package styles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func containsANSI(s string) bool {
	return strings.Contains(s, "\x1b")
}

func TestSetColorEnabled_DisablesANSI(t *testing.T) {
	t.Cleanup(func() { SetColorEnabled(true) })

	SetColorEnabled(false)

	out := lipgloss.NewStyle().Foreground(Red).Render("status")
	if containsANSI(out) {
		t.Fatalf("rendered output still contains ANSI when color disabled: %q", out)
	}
}

func TestSetColorEnabled_ForcesAsciiProfile(t *testing.T) {
	t.Cleanup(func() { SetColorEnabled(true) })

	SetColorEnabled(false)

	if lipgloss.ColorProfile() != termenv.Ascii {
		t.Fatalf("color profile = %v, want Ascii when disabled", lipgloss.ColorProfile())
	}
}

func TestStatusBadge_Clean(t *testing.T) {
	t.Cleanup(func() { SetColorEnabled(true) })
	SetColorEnabled(false)

	out := StatusBadge(true, 5)
	if !strings.Contains(out, "clean") {
		t.Errorf("StatusBadge(clean) = %q, want substring %q", out, "clean")
	}
	if strings.Contains(out, "chg") {
		t.Errorf("clean badge should not mention changes: %q", out)
	}
}

func TestStatusBadge_Dirty(t *testing.T) {
	t.Cleanup(func() { SetColorEnabled(true) })
	SetColorEnabled(false)

	out := StatusBadge(false, 3)
	if !strings.Contains(out, "3") {
		t.Errorf("StatusBadge(dirty) = %q, want change count", out)
	}
	if !strings.Contains(out, "chg") {
		t.Errorf("StatusBadge(dirty) = %q, want change label", out)
	}
}

func TestStatusBadge_ColorDisabled_NoANSI(t *testing.T) {
	t.Cleanup(func() { SetColorEnabled(true) })
	SetColorEnabled(false)

	for _, tc := range []struct {
		name  string
		clean bool
		count int
	}{
		{"clean", true, 0},
		{"dirty", false, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := StatusBadge(tc.clean, tc.count)
			if containsANSI(out) {
				t.Fatalf("StatusBadge leaked ANSI with color disabled: %q", out)
			}
		})
	}
}

func TestGetTerminalWidth_COLUMNS(t *testing.T) {
	t.Setenv("COLUMNS", "120")

	if got := GetTerminalWidth(); got != 120 {
		t.Errorf("GetTerminalWidth() = %d, want 120", got)
	}
}

func TestGetTerminalWidth_COLUMNSTrimsWhitespace(t *testing.T) {
	t.Setenv("COLUMNS", "  64 ")

	if got := GetTerminalWidth(); got != 64 {
		t.Errorf("GetTerminalWidth() = %d, want 64", got)
	}
}

func TestGetTerminalWidth_InvalidCOLUMNS_FallsBack(t *testing.T) {
	t.Setenv("COLUMNS", "not-a-number")

	got := GetTerminalWidth()
	if got <= 0 {
		t.Fatalf("GetTerminalWidth() = %d, want positive fallback", got)
	}
}

func TestGetTerminalWidth_ZeroCOLUMNS_FallsBack(t *testing.T) {
	t.Setenv("COLUMNS", "0")

	got := GetTerminalWidth()
	if got <= 0 {
		t.Fatalf("GetTerminalWidth() = %d, want positive fallback", got)
	}
}

func TestGetTerminalWidth_NoCOLUMNS_ReturnsPositive(t *testing.T) {
	t.Setenv("COLUMNS", "")

	got := GetTerminalWidth()
	if got <= 0 {
		t.Fatalf("GetTerminalWidth() = %d, want positive width", got)
	}
}

func TestReloadTheme_DoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	ReloadTheme()

	if Green == "" || Red == "" || Blue == "" {
		t.Fatal("theme colors were not populated after reload")
	}
}

func TestApplyThemeViaLoad_SetsPalette(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	ReloadTheme()

	if Green == "" || Red == "" || Blue == "" {
		t.Fatal("theme colors were not populated after reload")
	}
	if !Header.GetBold() {
		t.Error("Header style was not initialized with bold")
	}
}

func TestLoadTheme_LoadError_FallsBackToDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: \"1.0\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(configPath, 0000); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0600) })

	ReloadTheme()

	if Green == "" || Red == "" {
		t.Fatal("ReloadTheme did not apply a fallback theme after load error")
	}
}

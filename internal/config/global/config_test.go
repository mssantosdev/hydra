package global

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)
	return dir
}

func TestDefaultGlobalConfig(t *testing.T) {
	cfg := DefaultGlobalConfig()

	if cfg.Version != "1.0" {
		t.Errorf("expected version 1.0, got %s", cfg.Version)
	}
	if cfg.Theme.Name != "terminal" {
		t.Errorf("expected theme terminal, got %s", cfg.Theme.Name)
	}
	if cfg.Defaults.Editor != "code" {
		t.Errorf("expected editor code, got %s", cfg.Defaults.Editor)
	}
	if cfg.Defaults.Pager != "less" {
		t.Errorf("expected pager less, got %s", cfg.Defaults.Pager)
	}
	if !cfg.Defaults.ConfirmDestructive {
		t.Error("expected ConfirmDestructive to be true")
	}
}

func TestGetConfigDir_HYDRA_CONFIG_DIR(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	if got := GetConfigDir(); got != dir {
		t.Errorf("GetConfigDir() = %q, want %q", got, dir)
	}
}

func TestGetConfigPath(t *testing.T) {
	dir := configTestDir(t)

	want := filepath.Join(dir, "config.yaml")
	if got := GetConfigPath(); got != want {
		t.Errorf("GetConfigPath() = %q, want %q", got, want)
	}
}

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	configTestDir(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	defaults := DefaultGlobalConfig()
	if cfg.Version != defaults.Version {
		t.Errorf("Version = %q, want %q", cfg.Version, defaults.Version)
	}
	if cfg.Theme.Name != defaults.Theme.Name {
		t.Errorf("Theme.Name = %q, want %q", cfg.Theme.Name, defaults.Theme.Name)
	}
	if cfg.Defaults.Editor != defaults.Defaults.Editor {
		t.Errorf("Defaults.Editor = %q, want %q", cfg.Defaults.Editor, defaults.Defaults.Editor)
	}
}

func TestLoad_MalformedFile_ReturnsError(t *testing.T) {
	dir := configTestDir(t)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(":\tbad yaml: ["), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for malformed config, got nil")
	}
}

func TestLoad_MissingThemeName_DefaultsToTokyonight(t *testing.T) {
	dir := configTestDir(t)
	configPath := filepath.Join(dir, "config.yaml")
	content := []byte("version: \"1.0\"\ndefaults:\n  editor: vim\n")
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Theme.Name != "tokyonight" {
		t.Errorf("Theme.Name = %q, want tokyonight for empty theme in file", cfg.Theme.Name)
	}
}

func TestSave_CreatesDirectoryAndFileModes(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "hydra-config")
	t.Setenv("HYDRA_CONFIG_DIR", dir)
	cfg := DefaultGlobalConfig()

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("config dir mode = %o, want 0700", dirInfo.Mode().Perm())
	}

	configPath := filepath.Join(dir, "config.yaml")
	fileInfo, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("stat config file: %v", err)
	}
	if fileInfo.Mode().Perm() != 0600 {
		t.Errorf("config file mode = %o, want 0600", fileInfo.Mode().Perm())
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	configTestDir(t)
	cfg := &GlobalConfig{
		Version: "1.0",
		Theme: ThemeConf{
			Name: "catppuccin",
		},
		Defaults: Defaults{
			Editor:             "vim",
			Pager:              "cat",
			ConfirmDestructive: false,
		},
	}

	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Theme.Name != "catppuccin" {
		t.Errorf("Theme.Name = %q, want catppuccin", loaded.Theme.Name)
	}
	if loaded.Defaults.Editor != "vim" {
		t.Errorf("Defaults.Editor = %q, want vim", loaded.Defaults.Editor)
	}
	if loaded.Defaults.Pager != "cat" {
		t.Errorf("Defaults.Pager = %q, want cat", loaded.Defaults.Pager)
	}
	if loaded.Defaults.ConfirmDestructive {
		t.Error("Defaults.ConfirmDestructive = true, want false")
	}
}

func TestSetTheme_Persists(t *testing.T) {
	configTestDir(t)
	cfg := DefaultGlobalConfig()

	if err := cfg.SetTheme("dracula"); err != nil {
		t.Fatalf("SetTheme() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Theme.Name != "dracula" {
		t.Errorf("Theme.Name = %q, want dracula", loaded.Theme.Name)
	}
}

func TestSetEditor_Persists(t *testing.T) {
	configTestDir(t)
	cfg := DefaultGlobalConfig()

	if err := cfg.SetEditor("nvim"); err != nil {
		t.Fatalf("SetEditor() error = %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded.Defaults.Editor != "nvim" {
		t.Errorf("Defaults.Editor = %q, want nvim", loaded.Defaults.Editor)
	}
}

func TestLoad_ReadError(t *testing.T) {
	dir := configTestDir(t)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: \"1.0\"\n"), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(configPath, 0000); err != nil {
		t.Fatalf("chmod config: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0600) })

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error when config is unreadable, got nil")
	}
}

func TestGetConfigDir_DefaultPath(t *testing.T) {
	t.Setenv("HYDRA_CONFIG_DIR", "")

	dir := GetConfigDir()
	if dir == "" {
		t.Fatal("GetConfigDir() returned empty path")
	}
	if !strings.Contains(dir, "hydra") {
		t.Errorf("GetConfigDir() = %q, want path containing hydra", dir)
	}
}

func TestSave_MkdirAllError(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	t.Setenv("HYDRA_CONFIG_DIR", filepath.Join(blocker, "hydra"))

	if err := DefaultGlobalConfig().Save(); err == nil {
		t.Fatal("Save() expected error when config dir cannot be created")
	}
}

func TestSave_WriteFileError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.Mkdir(configPath, 0700); err != nil {
		t.Fatalf("mkdir config path: %v", err)
	}

	if err := DefaultGlobalConfig().Save(); err == nil {
		t.Fatal("Save() expected error when config path is a directory")
	}
}

func TestPlatformConfigDir_Darwin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := platformConfigDir("darwin")
	want := filepath.Join(home, "Library", "Application Support", "hydra")
	if dir != want {
		t.Errorf("platformConfigDir(darwin) = %q, want %q", dir, want)
	}
}

func TestPlatformConfigDir_Windows_AppData(t *testing.T) {
	appdata := filepath.Join("C:", "Users", "me", "AppData", "Roaming")
	t.Setenv("APPDATA", appdata)

	dir := platformConfigDir("windows")
	want := filepath.Join(appdata, "hydra")
	if dir != want {
		t.Errorf("platformConfigDir(windows) = %q, want %q", dir, want)
	}
}

func TestPlatformConfigDir_Windows_FallbackHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", "")

	dir := platformConfigDir("windows")
	want := filepath.Join(home, "AppData", "Roaming", "hydra")
	if dir != want {
		t.Errorf("platformConfigDir(windows) = %q, want %q", dir, want)
	}
}

func TestPlatformConfigDir_Linux(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := platformConfigDir("linux")
	want := filepath.Join(home, ".config", "hydra")
	if dir != want {
		t.Errorf("platformConfigDir(linux) = %q, want %q", dir, want)
	}
}

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitShellWithCompletionWritesGeneratedAssets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	withCompletion = true
	withoutCompletion = false

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"init-shell", "bash"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init-shell failed: %v", err)
	}

	helperPath := filepath.Join(home, ".config", "hydra", "shell", "hydra-shell.bash")
	completionPath := filepath.Join(home, ".config", "hydra", "shell", "hydra-completion.bash")
	if _, err := os.Stat(helperPath); err != nil {
		t.Fatalf("expected helper file: %v", err)
	}
	if _, err := os.Stat(completionPath); err != nil {
		t.Fatalf("expected completion file: %v", err)
	}

	rcPath := filepath.Join(home, ".bashrc")
	rcData, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("expected rc file: %v", err)
	}
	rc := string(rcData)
	if !strings.Contains(rc, helperMarkerStart) || !strings.Contains(rc, "source \""+helperPath+"\"") {
		t.Fatalf("rc file missing loader block: %s", rc)
	}
	if strings.Contains(rc, "hydra() {") || strings.Contains(rc, "complete -c hydra") {
		t.Fatalf("rc file should only contain loader block: %s", rc)
	}
}

func TestInitShellWithoutCompletionSkipsCompletionFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	withCompletion = false
	withoutCompletion = true

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"init-shell", "zsh"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init-shell failed: %v", err)
	}

	completionPath := filepath.Join(home, ".config", "hydra", "shell", "hydra-completion.zsh")
	if _, err := os.Stat(completionPath); !os.IsNotExist(err) {
		t.Fatalf("expected no completion file, got: %v", err)
	}
}

func TestInitShellPromptsWhenCompletionFlagMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/fish")
	withCompletion = false
	withoutCompletion = false

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetIn(strings.NewReader("y\n"))
	rootCmd.SetArgs([]string{"init-shell", "fish"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init-shell failed: %v", err)
	}

	if !strings.Contains(out.String(), "Install completion files for fish too?") {
		t.Fatalf("expected prompt, got: %s", out.String())
	}
}

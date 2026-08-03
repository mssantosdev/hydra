package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resetInitShellFlags(t *testing.T) {
	t.Helper()
	withCompletion = false
	withoutCompletion = false
	installFlag = true
	printFlag = false
}

func TestInitShellWithCompletionWritesGeneratedAssets(t *testing.T) {
	resetCommandState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/bash")
	resetInitShellFlags(t)
	withCompletion = true

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
	resetCommandState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/zsh")
	resetInitShellFlags(t)
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
	resetCommandState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SHELL", "/bin/fish")
	resetInitShellFlags(t)

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

func TestInitShellPrintWritesLoaderOnly(t *testing.T) {
	resetCommandState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	resetInitShellFlags(t)
	printFlag = true
	installFlag = false

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"init-shell", "bash", "--print"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("init-shell --print failed: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, helperMarkerStart) {
		t.Fatalf("expected loader block, got: %s", output)
	}
	if strings.Contains(output, "Shell helper installed") {
		t.Fatalf("print mode should not emit install summary: %s", output)
	}

	rcPath := filepath.Join(home, ".bashrc")
	if _, err := os.Stat(rcPath); !os.IsNotExist(err) {
		t.Fatalf("print mode should not write rc file")
	}
}

func TestFishHelperContainsNoBashSyntax(t *testing.T) {
	helper := generateFishHelper()

	forbidden := []string{
		"${TMPDIR:-/tmp}",
		"${",
		":-",
		"env HYDRA_SWITCH_OUTPUT_FILE",
	}
	for _, sub := range forbidden {
		if strings.Contains(helper, sub) {
			t.Fatalf("fish helper contains bash-only syntax %q:\n%s", sub, helper)
		}
	}
}

func TestFishHelperMktempUsesFishTmpdirFallback(t *testing.T) {
	helper := generateFishHelper()

	if !strings.Contains(helper, "set -l tmpdir $TMPDIR") {
		t.Fatalf("expected fish TMPDIR fallback handling")
	}
	if !strings.Contains(helper, "set -lx HYDRA_SWITCH_OUTPUT_FILE $output_file") {
		t.Fatalf("expected fish local export for HYDRA_SWITCH_OUTPUT_FILE")
	}
}

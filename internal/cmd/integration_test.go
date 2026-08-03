package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestIntegration_InitShell(t *testing.T) {
	resetCommandState(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	resetInitShellFlags(t)
	installFlag = false
	printFlag = true

	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			rootCmd.SetArgs([]string{"init-shell", shell, "--print"})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("init-shell %s failed: %v", shell, err)
			}
			if !strings.Contains(out.String(), helperMarkerStart) {
				t.Fatalf("expected loader block for %s", shell)
			}
		})
	}

	fishHelper := generateFishHelper()
	for _, forbidden := range []string{"${TMPDIR:-/tmp}", "${", ":-", "env HYDRA_SWITCH_OUTPUT_FILE"} {
		if strings.Contains(fishHelper, forbidden) {
			t.Fatalf("fish helper contains bash-only syntax %q", forbidden)
		}
	}

	rootCmd.SetArgs([]string{"init-shell", "powershell"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("Should fail for unsupported shell")
	}
}

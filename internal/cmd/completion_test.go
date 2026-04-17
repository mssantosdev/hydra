package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestCompletionCommandPrintsScripts(t *testing.T) {
	tests := []struct {
		shell string
		want  string
	}{
		{shell: "bash", want: "complete"},
		{shell: "zsh", want: "compdef"},
		{shell: "fish", want: "complete -c hydra"},
	}

	for _, tt := range tests {
		t.Run(tt.shell, func(t *testing.T) {
			var out bytes.Buffer
			rootCmd.SetOut(&out)
			rootCmd.SetErr(&out)
			rootCmd.SetArgs([]string{"completion", tt.shell})

			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s failed: %v", tt.shell, err)
			}

			if !strings.Contains(out.String(), tt.want) {
				t.Fatalf("completion %s output missing %q", tt.shell, tt.want)
			}
		})
	}
}

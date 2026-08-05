package cmd

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Help text must never recommend a command that does not exist.
//
// This is not hypothetical tidiness. `hydra clone` and `hydra adopt` were replaced by
// `hydra repo add` in v0.2.0, but eleven places kept naming them — including the "Next:"
// block `hydra init` prints on success, which is the first thing a new caller reads. An
// agent with no prior knowledge of hydra followed that advice, ran `hydra clone`, and got
// `unknown_command`. The tool taught it something false at the first opportunity.
//
// Any future rename must either update the prose or fail here.
func TestHelpNeverNamesAMissingCommand(t *testing.T) {
	resetCommandState(t)

	real := map[string]bool{}
	var collect func(*cobra.Command, string)
	collect = func(c *cobra.Command, prefix string) {
		for _, child := range c.Commands() {
			name := strings.TrimSpace(prefix + " " + child.Name())
			real[name] = true
			for _, alias := range child.Aliases {
				real[strings.TrimSpace(prefix+" "+alias)] = true
			}
			collect(child, name)
		}
	}
	collect(rootCmd, "")

	// Only INVOCATION shapes count, not prose. "hydra has no worktrees" is a sentence;
	// "  • hydra repo add - …" and "$ hydra list" and `hydra doctor` are references a
	// reader will type. Matching bare "hydra <word>" anywhere flags every sentence that
	// happens to name the tool.
	//
	// Two-word forms are captured so `repo add` is recognised before the bare `repo`.
	mention := regexp.MustCompile(
		"(?m)(?:^[\\s*•-]*\\$?\\s*|`)hydra ([a-z][a-z-]+)(?: ([a-z][a-z-]+))?")

	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, text := range []string{c.Short, c.Long, c.Example} {
			for _, m := range mention.FindAllStringSubmatch(text, -1) {
				one, two := m[1], m[2]
				if two != "" && real[one+" "+two] {
					continue
				}
				if real[one] {
					continue
				}
				// A placeholder or a flag is not a command reference.
				if strings.HasPrefix(one, "-") {
					continue
				}
				t.Errorf("%q help names %q, which is not a registered command",
					strings.TrimSpace(c.CommandPath()), "hydra "+one)
			}
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

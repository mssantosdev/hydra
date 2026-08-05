package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandsCmd = &cobra.Command{
	Use:   "commands",
	Short: "Describe hydra's whole command surface",
	Long: `Describe every command, flag and error code hydra has.

DESCRIPTION
  The machine-readable surface. An agent can discover what hydra can do, which flags
  each command takes, and how every error code maps to an exit status — without
  parsing --help text that is written for humans and free to change wording.

  The error-code table is the authority for exit statuses. It is generated from the
  same enum the code raises, so it cannot drift from behaviour.

  scripts/e2e.sh and a contract test compare this output against the committed
  SURFACE.txt, so adding or renaming anything shows up as a reviewable diff instead
  of arriving unnoticed.

EXAMPLES
  # everything, as JSON
  $ hydra commands --output json

  # which flags does start take?
  $ hydra commands --output json | jq '.data.commands[] | select(.name=="start") | .flags'

  # what exit status does busy have, and may I retry it?
  $ hydra commands --output json | jq '.data.error_codes[] | select(.code=="busy")'

  # the stable text snapshot, as committed
  $ hydra commands

EXIT CODES
  0  Success`,
	Args: cobra.NoArgs,
	RunE: runCommands,
}

func init() {
	rootCmd.AddCommand(commandsCmd)
}

type flagJSON struct {
	Name string `json:"name"`
	// Shorthand is empty for most flags; it is reported so a caller can render usage
	// without re-deriving it.
	Shorthand string `json:"shorthand,omitempty"`
	Usage     string `json:"usage"`
	Type      string `json:"type"`
	Default   string `json:"default,omitempty"`
}

type commandJSON struct {
	// Name is the full path, so a subcommand is addressable as it is typed:
	// "topic attach", not "attach".
	Name    string     `json:"name"`
	Short   string     `json:"short"`
	Aliases []string   `json:"aliases,omitempty"`
	Usage   string     `json:"usage"`
	Flags   []flagJSON `json:"flags,omitempty"`
	// HasSubcommands tells a caller this is a group rather than something runnable.
	HasSubcommands bool `json:"has_subcommands"`
}

type errorCodeJSON struct {
	Code string `json:"code"`
	Exit int    `json:"exit"`
	// Retryable is the one fact a caller cannot derive from the code or the exit
	// status, which is why the envelope carries it too.
	Retryable bool `json:"retryable"`
}

type commandsJSON struct {
	Schema     int             `json:"surface_schema"`
	Commands   []commandJSON   `json:"commands"`
	ErrorCodes []errorCodeJSON `json:"error_codes"`
	// GlobalFlags apply to every command, so they are listed once rather than repeated
	// on each entry.
	GlobalFlags []flagJSON `json:"global_flags"`
}

// surfaceSchema versions this description independently of the output envelope: the
// surface can gain a field without the envelope changing, and vice versa.
const surfaceSchema = 1

func runCommands(cmd *cobra.Command, args []string) error {
	payload := describeSurface()
	return emit(cmd,
		fmt.Sprintf("%d commands, %d error codes", len(payload.Commands), len(payload.ErrorCodes)),
		payload, nil, func() {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), renderSurfaceText(payload))
		})
}

// describeSurface walks the cobra tree.
func describeSurface() commandsJSON {
	payload := commandsJSON{
		Schema:      surfaceSchema,
		GlobalFlags: describeFlags(rootCmd.PersistentFlags()),
	}

	var walk func(parent string, c *cobra.Command)
	walk = func(parent string, c *cobra.Command) {
		for _, child := range c.Commands() {
			if !child.IsAvailableCommand() || child.Name() == "help" {
				continue
			}
			name := strings.TrimSpace(parent + " " + child.Name())
			entry := commandJSON{
				Name:    name,
				Short:   child.Short,
				Aliases: child.Aliases,
				Usage:   child.UseLine(),
				// LocalNonPersistentFlags, not Flags(): cobra's Flags() includes every
				// inherited persistent flag, which would repeat the five global flags on
				// all 30 commands and bury each command's own surface in noise.
				Flags:          describeFlags(child.LocalNonPersistentFlags()),
				HasSubcommands: child.HasAvailableSubCommands(),
			}
			payload.Commands = append(payload.Commands, entry)
			walk(name, child)
		}
	}
	walk("", rootCmd)

	sort.Slice(payload.Commands, func(i, j int) bool {
		return payload.Commands[i].Name < payload.Commands[j].Name
	})

	// Generated from the same map the code raises, so the published table cannot
	// disagree with behaviour.
	for _, code := range output.Codes() {
		payload.ErrorCodes = append(payload.ErrorCodes, errorCodeJSON{
			Code:      code,
			Exit:      output.ExitFor(code),
			Retryable: output.Retryable(code),
		})
	}
	return payload
}

// describeFlags renders a flag set, skipping cobra's auto-added --help: it exists on
// every command and says nothing about the surface.
func describeFlags(set *pflag.FlagSet) []flagJSON {
	var out []flagJSON
	set.VisitAll(func(f *pflag.Flag) {
		if f.Hidden || f.Name == "help" {
			return
		}
		out = append(out, flagJSON{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Usage:     f.Usage,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
		})
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// renderSurfaceText produces the committed snapshot.
//
// Deliberately plain and stable: no colour, no terminal width, no timestamps, no
// version string. A snapshot that changes for a reason unrelated to the surface is
// noise in every review, and would make the contract test fail for nothing.
func renderSurfaceText(payload commandsJSON) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# hydra command surface, schema %d\n", payload.Schema)
	sb.WriteString("# Generated by \"hydra commands\". Committed so surface drift is reviewable.\n")

	sb.WriteString("\n## Global flags\n")
	for _, flag := range payload.GlobalFlags {
		fmt.Fprintf(&sb, "%s\n", formatFlag(flag))
	}

	sb.WriteString("\n## Commands\n")
	for _, command := range payload.Commands {
		line := command.Name
		if len(command.Aliases) > 0 {
			line += " (" + strings.Join(command.Aliases, ", ") + ")"
		}
		fmt.Fprintf(&sb, "%s\n", line)
		for _, flag := range command.Flags {
			fmt.Fprintf(&sb, "%s\n", formatFlag(flag))
		}
	}

	sb.WriteString("\n## Error codes\n")
	for _, entry := range payload.ErrorCodes {
		retry := ""
		if entry.Retryable {
			retry = "  retryable"
		}
		fmt.Fprintf(&sb, "  %-28s exit %d%s\n", entry.Code, entry.Exit, retry)
	}
	return sb.String()
}

func formatFlag(flag flagJSON) string {
	name := "--" + flag.Name
	if flag.Shorthand != "" {
		name = "-" + flag.Shorthand + ", " + name
	}
	// The default is included because it is part of the contract: a caller needs to
	// know that --jobs defaults to 0 rather than 1.
	if flag.Default != "" && flag.Default != "false" && flag.Default != "[]" {
		return fmt.Sprintf("  %-34s %s (default %s)", name, flag.Type, flag.Default)
	}
	return fmt.Sprintf("  %-34s %s", name, flag.Type)
}

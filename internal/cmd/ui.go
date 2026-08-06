package cmd

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/browser"
)

// uiCmd is the interactive reporting surface.
//
// Eight mutating flows already prompt when run bare on a terminal. Every REPORTING command
// was flags-only, so exploring a workspace required already knowing the flag that would
// answer the question. This closes that half: browse the register, filter it, and leave
// with the same answer `hydra switch` gives.
//
// It is a separate command rather than a change to bare `hydra` on purpose. Bare `hydra`
// prints help, which is what a caller and every shell completion already expect; taking
// that over would be a breaking change to earn an entry point that a name provides for
// free.
var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"tui"},
	Short:   "Browse the workspace interactively",
	Long: `Browse every worktree in the project as a live register.

DESCRIPTION
  A full-screen view of the same data "hydra status" reports, with filtering and
  selection. Every refresh re-reads git rather than patching what is on screen, so the
  register cannot drift from disk.

  Selecting a worktree prints its path on stdout and exits, which is exactly what
  "hydra switch" does — so it composes the same way:

    cd "$(hydra ui)"

  Requires a terminal. Without one it returns needs_input (exit 7) rather than
  rendering escape codes into a pipe.

KEYS
  ↑ ↓ / j k     move            enter    select and print the path
  /             filter          d        filter to dirty worktrees
  g G           top / bottom    r        re-read git
  ctrl+d ctrl+u half page       q        quit without selecting

FILTER
  Bare text matches repo, branch, worktree name, topic and group. The words "dirty",
  "behind" and "ahead" filter by state, and "topic:<id>" by membership — the same
  vocabulary as --filter on the non-interactive commands.

EXIT CODES
  0  a worktree was selected, or the browser was quit cleanly
  2  not in a project
  7  needs_input (no terminal)

SEE ALSO
  hydra status   - the same data, non-interactively
  hydra switch   - select a worktree by name`,
	Args: cobra.NoArgs,
	RunE: runUI,
}

func init() {
	rootCmd.AddCommand(uiCmd)
}

func runUI(cmd *cobra.Command, _ []string) error {
	if err := loadProject(); err != nil {
		return err
	}

	// A full-screen program in a pipe would emit escape sequences as data. This is the
	// same refusal every prompting command already makes, with the same code.
	if !interactive() {
		return output.Errorf(output.CodeNeedsInput,
			"hydra ui needs a terminal").
			WithDetail("missing", "tty").
			WithNext(output.Next{
				Argv: []string{"hydra", "status", "--output", "json"},
				Why:  "the same data, without a terminal",
			})
	}

	root := projectRoot
	conf := cfg

	load := func() ([]browser.Row, string, error) {
		worktrees, _ := collectWorktrees(conf, root)

		// Membership is recorded, never inferred, and this is the same index `list` and
		// `status` decorate from — so the three views cannot disagree about a topic.
		idx, err := newTopicIndex(root)
		if err != nil {
			idx = nil
		}

		rows := make([]browser.Row, 0, len(worktrees))
		var asOf string
		for _, wt := range worktrees {
			item, err := wt.withTracking()
			if err != nil {
				// One unreadable worktree must not blank the register; it is shown with
				// what is known instead of dropped silently.
				item = wt.json()
			}
			idx.decorate(&item)

			up := "local-only"
			if item.Upstream != nil && *item.Upstream != "" {
				up = *item.Upstream
			}
			if item.UpstreamAsOf != nil && *item.UpstreamAsOf > asOf {
				asOf = *item.UpstreamAsOf
			}
			topicID := ""
			if item.Topic != nil {
				topicID = *item.Topic
			}
			rows = append(rows, browser.Row{
				Group:    item.Group,
				Repo:     item.Repo,
				Name:     item.Name,
				Branch:   item.Branch,
				Path:     item.Path,
				Upstream: up,
				Topic:    topicID,
				Ahead:    item.Ahead,
				Behind:   item.Behind,
				Dirty:    item.Dirty,
				Changes:  item.Changes,
				Detached: item.Detached,
			})
		}
		return rows, asOf, nil
	}

	model := browser.New(conf.Project, load)
	final, err := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithOutput(os.Stderr), // the register is chrome; stdout stays the answer
	).Run()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "the browser failed")
	}

	// stdout carries only the selection, so `cd "$(hydra ui)"` works. Quitting prints
	// nothing and still exits 0: choosing not to choose is not a failure.
	if sel, ok := browser.Chosen(final); ok {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), sel.Path); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to write the selected path")
		}
	}
	return nil
}

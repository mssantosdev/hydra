package cmd

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/browser"
)

// uiCmd is a hidden alias of status. Bare `hydra status` on a terminal opens the
// interactive register; `hydra ui` remains for scripts and muscle memory.
var uiCmd = &cobra.Command{
	Use:     "ui",
	Aliases: []string{"tui"},
	Hidden:  true,
	Short:   "Hidden alias of hydra status",
	Long: `Hidden alias of hydra status.

DESCRIPTION
  Delegates to "hydra status". On a terminal with default output, status opens the
  same full-screen register this command used to own; with --output text or --output
  json it renders the non-interactive status view instead.

SEE ALSO
  hydra status   - the supported entry point`,
	Args: cobra.NoArgs,
	RunE: runStatus,
}

func init() {
	rootCmd.AddCommand(uiCmd)
}

// explicitOutputMode reports whether --output was set to text or json (including via
// HYDRA_OUTPUT), as opposed to auto.
func explicitOutputMode() bool {
	return outMode == output.ModeText || outMode == output.ModeJSON
}

// statusLaunchesBoard reports whether this invocation should open the register
// rather than render status to stdout.
func statusLaunchesBoard(args []string) bool {
	return len(args) == 0 && !explicitOutputMode()
}

func boardProjectLabel(targets []projectTarget, all bool) string {
	if all {
		return "all projects"
	}
	if len(targets) > 0 {
		return targets[0].Name
	}
	if cfg != nil {
		return cfg.Project
	}
	return ""
}

// newBoardLoader builds the register loader shared by status's interactive path.
// Every refresh re-reads git through the same resolver list/status use, so selector
// semantics cannot drift between the board and the non-interactive views.
func newBoardLoader(targets []projectTarget, sel Selector) browser.Loader {
	return func() ([]browser.Row, string, error) {
		rows := make([]browser.Row, 0)
		var asOf string
		for _, target := range targets {
			resolved, _, _, err := resolveTargets(sessionFor(target), sel, true)
			if err != nil {
				return nil, "", err
			}
			project := target.Name
			if target.Cfg != nil {
				project = target.Cfg.Project
			}
			for _, entry := range resolved {
				rows = append(rows, worktreeItemToRow(project, entry.Item, &asOf))
			}
		}
		return rows, asOf, nil
	}
}

func worktreeItemToRow(project string, item worktreeJSON, asOf *string) browser.Row {
	up := "local-only"
	if item.Upstream != nil && *item.Upstream != "" {
		up = *item.Upstream
	}
	if item.UpstreamAsOf != nil && *item.UpstreamAsOf > *asOf {
		*asOf = *item.UpstreamAsOf
	}
	topicID := ""
	if item.Topic != nil {
		topicID = *item.Topic
	}
	return browser.Row{
		Project:  project,
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
		Changes:  item.DirtyFiles,
		Detached: item.Detached,
		Against:  againstInfoForBoard(item.Against),
	}
}

func againstInfoForBoard(against *againstJSON) *browser.AgainstInfo {
	if against == nil {
		return nil
	}
	return &browser.AgainstInfo{
		Ref:    against.Ref,
		Ahead:  against.Ahead,
		Behind: against.Behind,
		Merged: against.Merged,
	}
}

// runStatusBoard opens the full-screen register. The register renders to stderr;
// stdout carries only a selected path, so `cd "$(hydra status)"` works.
func runStatusBoard(cmd *cobra.Command, targets []projectTarget, sel Selector, all bool) error {
	load := newBoardLoader(targets, sel)
	model := browser.New(boardProjectLabel(targets, all), load, browser.State{
		Filter: boardInitialFilter(sel),
	})
	final, err := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithOutput(os.Stderr), // the register is chrome; stdout stays the answer
	).Run()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "the browser failed")
	}

	// stdout carries only the selection. Quitting prints nothing and still exits 0:
	// choosing not to choose is not a failure.
	if sel, ok := browser.Chosen(final); ok {
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), sel.Path); err != nil {
			return output.Wrap(output.CodeIOFailed, err, "failed to write the selected path")
		}
	}
	return nil
}

// boardInitialFilter maps the resolved selector flags onto the register's filter
// vocabulary so the board opens pre-scoped.
func boardInitialFilter(sel Selector) string {
	if sel.Topic != "" {
		return "topic:" + sel.Topic
	}
	for _, entry := range sel.Filter {
		value := strings.TrimSpace(entry)
		switch value {
		case "dirty", "behind":
			return value
		}
		if strings.HasPrefix(value, "branch:") {
			return value
		}
	}
	if sel.Group != "" {
		return sel.Group
	}
	if len(sel.Repos) == 1 {
		return sel.Repos[0]
	}
	return ""
}

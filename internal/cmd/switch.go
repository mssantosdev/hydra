package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/output"
)

// switchMarker is the token the shell helper greps for. It is part of hydra's
// public interface with the shell, not an implementation detail.
const switchMarker = "__HYDRA_CD__"

var switchCD bool

type switchResult struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Group     string `json:"group"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	CDEmitted bool   `json:"cd_emitted"`
}

var switchCmd = &cobra.Command{
	Use:     "switch [<worktree>]",
	Aliases: []string{"sw"},
	Short:   "Change directory to a worktree",
	Long: `Resolve a worktree and hand its path to the shell.

DESCRIPTION
  With the shell helper installed (see "hydra init-shell"), switch emits the change
  directory instruction the helper consumes, and your shell lands in the worktree.

  Without the helper, switch still answers: it prints the absolute path on stdout,
  a one-line hint on stderr, and exits 0. Scripts and agents can therefore always
  ask where a worktree is. For scripting, prefer "hydra path", which is the explicit
  path-printing command and never involves the shell helper.

  Only "switch --cd" requires the helper; without it that form fails with exit 3.

WHEN TO USE
  • Interactively moving between worktrees
  • Picking a worktree from a list when you do not remember its name

EXAMPLES
  # Jump to a worktree (with the shell helper installed)
  $ hydra switch api-stage

  # Pick from a list
  $ hydra switch

  # Just tell me the path
  $ hydra path api-stage

FLAGS
  --cd   require the shell helper and fail with exit 3 when it is missing

EXIT CODES
  0  Success
  1  repo_unknown, bare_missing, git_failed
  1  worktree_unknown (details.did_you_mean lists close matches)
  2  not_in_project, config_version_unsupported, project_unknown
  3  shell_helper_missing (only with --cd)

SEE ALSO
  • hydra path       - print a worktree path, no shell helper needed
  • hydra init-shell - install the shell helper
  • hydra list       - list worktrees`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completeWorktreeNames,
	RunE:              runSwitch,
}

func init() {
	switchCmd.Flags().BoolVar(&switchCD, "cd", false, "require the shell helper")
	rootCmd.AddCommand(switchCmd)
}

func runSwitch(cmd *cobra.Command, args []string) error {
	wt, err := resolveSwitchTarget(args)
	if err != nil {
		return err
	}

	helperActive := isShellHelperInitialized()
	if switchCD && !helperActive {
		return output.Errorf(output.CodeShellHelperMissing,
			"the hydra shell helper is not initialized; run \"hydra init-shell\" and restart your shell, or use \"hydra path\"").
			WithDetail("path", wt.Path)
	}

	result := switchResult{
		Name:      wt.DirName,
		Path:      wt.Path,
		Group:     wt.RepoContext.Group,
		Repo:      wt.RepoContext.Alias,
		Branch:    wt.Branch,
		CDEmitted: helperActive,
	}

	if helperActive {
		// The helper reads a file when it provides one, otherwise the marker line.
		if target := os.Getenv("HYDRA_SWITCH_OUTPUT_FILE"); target != "" {
			//nolint:gosec // G306: the switch target file is read back by the shell helper
			if err := os.WriteFile(target, []byte(wt.Path+"\n"), 0644); err != nil {
				return output.Wrap(output.CodeInternal, err, "failed to write the switch target file")
			}
		} else if !jsonMode() {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", switchMarker, wt.Path)
			return nil
		}
	}

	return emit(cmd, result, nil, func() {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), wt.Path)
		if !helperActive {
			_, _ = fmt.Fprintln(os.Stderr, "hint: run \"hydra init-shell\" to have switch change directory for you; \"hydra path\" is the scriptable form")
		}
	})
}

func resolveSwitchTarget(args []string) (worktreeContext, error) {
	items, warnings := collectWorktrees(cfg, projectRoot)

	if len(args) == 1 {
		name := strings.TrimSpace(args[0])
		wt, err := resolveOneWorktree(items, name)
		if err == nil {
			return wt, nil
		}
		// Enrich only the not-found case; an ambiguity error already names every
		// candidate, and suggestions there would be noise.
		if output.Classify(err).Code == output.CodeWorktreeUnknown {
			return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
				"no worktree named %q", name).
				WithDetail("name", name).
				WithDetail("did_you_mean", findSimilarWorktreesByName(cfg, projectRoot, name))
		}
		return worktreeContext{}, err
	}

	if len(items) == 0 {
		return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
			"this project has no worktrees").
			WithDetail("warnings", warnings)
	}

	if !interactive() {
		return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
			"a worktree name is required: hydra switch <worktree>").
			WithDetail("available", worktreeHandles(items))
	}

	options := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s (%s)", item.Qualified(), item.BranchLabel()), item.Qualified()))
	}
	selected := items[0].Qualified()
	if err := huh.NewSelect[string]().Title("Switch to").Options(options...).Value(&selected).Run(); err != nil {
		return worktreeContext{}, output.Wrap(output.CodeInternal, err, "cancelled")
	}
	// The picker yields a Qualified() name, which is unique by construction, so this
	// cannot be ambiguous — it goes through the same resolver anyway so there is one
	// matching rule rather than two that could drift.
	return resolveOneWorktree(items, selected)
}

func worktreeHandles(items []worktreeContext) []string {
	handles := make([]string, 0, len(items))
	for _, item := range items {
		handles = append(handles, item.Qualified())
	}
	return handles
}

// isShellHelperInitialized reports whether the shell function wrapping hydra is
// active in this process's environment.
func isShellHelperInitialized() bool {
	return os.Getenv("HYDRA_SHELL_HELPER") == "1" || os.Getenv("HYDRA_SWITCH_OUTPUT_FILE") != ""
}

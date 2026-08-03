package cmd

import (
	"fmt"
	"os"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var pathCmd = &cobra.Command{
	Use:   "path [<worktree-name>]",
	Short: "Print a worktree path",
	Long: `Print the absolute path of a hydra worktree.

DESCRIPTION
  Resolves a worktree by name and prints its absolute path on stdout. This command
  is always scriptable: it never requires the shell helper and never prompts.

  The path is the payload, so it stays a bare path when stdout is captured. Unlike
  every other command, "path" does NOT auto-upgrade to JSON off a terminal —
  otherwise cd "$(hydra path api)" would break. Pass --output json explicitly (or
  set HYDRA_OUTPUT=json) to get the envelope with group, repo, and branch.

  With no argument, resolves the current worktree from $PWD.

WHEN TO USE
  • Any script or agent that needs a worktree's location
  • Anywhere you would have used "hydra switch" non-interactively

EXAMPLES
  # The current worktree
  $ hydra path

  # A named worktree
  $ hydra path api-feature-login

  # Change directory to it
  $ cd "$(hydra path api-feature-login)"

  # Full detail as JSON
  $ hydra path api-feature-login --output json

EXIT CODES
  0  Success (path printed)
  1  worktree_unknown (details.did_you_mean lists close matches)
  2  not_in_project, config_version_unsupported, project_unknown

SEE ALSO
  • hydra switch - interactive counterpart, uses the shell helper
  • hydra list   - list every worktree`,
	RunE: runPath,
	Args: cobra.MaximumNArgs(1),
}

type pathResult struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Group  string `json:"group"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
}

func init() {
	rootCmd.AddCommand(pathCmd)
	pathCmd.ValidArgsFunction = completeWorktreeNames
}

func runPath(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	var wt worktreeContext
	var err error

	if len(args) == 0 {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return output.Wrap(output.CodeInternal, wdErr, "failed to resolve the working directory")
		}
		current := resolveCurrentHydraContext(wd, cfg, projectRoot)
		if current == nil {
			return output.Errorf(output.CodeWorktreeUnknown, "current directory is not inside a hydra worktree")
		}
		wt = *current
	} else {
		wt, err = resolvePathTarget(args[0])
		if err != nil {
			return err
		}
	}

	result := pathResult{
		Name:   wt.DirName,
		Path:   wt.Path,
		Group:  wt.RepoContext.Group,
		Repo:   wt.RepoContext.Alias,
		Branch: wt.Branch,
	}
	return emitValue(cmd, result, nil, func() {
		fmt.Fprintln(cmd.OutOrStdout(), wt.Path)
	})
}

func resolvePathTarget(name string) (worktreeContext, error) {
	wt, ok := findWorktreeByName(cfg, projectRoot, name)
	if ok {
		return wt, nil
	}
	similar := findSimilarWorktreesByName(cfg, projectRoot, name)
	err := output.Errorf(output.CodeWorktreeUnknown, "worktree not found: %s", name)
	if len(similar) > 0 {
		err = err.WithDetail("did_you_mean", similar)
		if !explicitJSON() {
			// Suggestions go to stderr so they never pollute the path on stdout.
			fmt.Fprintln(os.Stderr, styles.Error.Render(fmt.Sprintf("Worktree not found: %s", name)))
			for i, s := range similar {
				fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, s)
			}
		}
	}
	return worktreeContext{}, err
}

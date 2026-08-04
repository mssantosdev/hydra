package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

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

var pathTopic string

func init() {
	rootCmd.AddCommand(pathCmd)
	pathCmd.ValidArgsFunction = completeWorktreeNames
	pathCmd.Flags().StringVar(&pathTopic, "topic", "", "Print the path of this topic's only worktree")
}

// resolveTopicPathTarget prints a topic's worktree only when there is exactly one.
//
// path must emit one path or fail: it is consumed as `cd "$(hydra path …)"`, so
// returning several lines would silently produce a broken cd, and returning the
// first would pick an arbitrary repo. A multi-worktree topic is therefore an error
// that names the candidates, and `hydra list --topic X` is the way to see them all.
func resolveTopicPathTarget() (worktreeContext, error) {
	session := currentSession()
	if err := requireTopicInTargets([]projectTarget{{Cfg: session.Cfg, Root: session.Root}}, pathTopic); err != nil {
		return worktreeContext{}, err
	}

	resolved, _, err := resolveTargets(session, Selector{Topic: pathTopic}, false)
	if err != nil {
		return worktreeContext{}, err
	}

	switch len(resolved) {
	case 1:
		return resolved[0].Context, nil
	case 0:
		return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
			"topic %q has no worktrees on disk", pathTopic).
			WithDetail("topic", pathTopic)
	default:
		candidates := make([]string, 0, len(resolved))
		for _, entry := range resolved {
			candidates = append(candidates, entry.Context.Qualified())
		}
		sort.Strings(candidates)
		return worktreeContext{}, output.Errorf(output.CodeWorktreeNameConflict,
			"topic %q spans %d worktrees; name one of %s, or use \"hydra list --topic %s\"",
			pathTopic, len(resolved), strings.Join(candidates, ", "), pathTopic).
			WithDetail("topic", pathTopic).
			WithDetail("candidates", candidates)
	}
}

func runPath(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	var wt worktreeContext
	var err error

	switch {
	case pathTopic != "":
		if len(args) > 0 {
			return output.Errorf(output.CodeInternal,
				"pass either a worktree or --topic, not both")
		}
		wt, err = resolveTopicPathTarget()
		if err != nil {
			return err
		}
	case len(args) == 0:
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return output.Wrap(output.CodeInternal, wdErr, "failed to resolve the working directory")
		}
		current := resolveCurrentHydraContext(wd, cfg, projectRoot)
		if current == nil {
			return output.Errorf(output.CodeWorktreeUnknown, "current directory is not inside a hydra worktree")
		}
		wt = *current
	default:
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
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), wt.Path)
	})
}

func resolvePathTarget(name string) (worktreeContext, error) {
	items, _ := collectWorktrees(cfg, projectRoot)
	matches := matchWorktrees(items, name)
	if len(matches) == 1 {
		return matches[0], nil
	}
	// Ambiguity must never resolve to "the first one": path is consumed by
	// `cd "$(hydra path X)"`, so a wrong-but-plausible answer sends the caller into
	// the wrong worktree with no indication anything happened.
	if len(matches) > 1 {
		_, err := resolveOneWorktree(items, name)
		return worktreeContext{}, err
	}
	similar := findSimilarWorktreesByName(cfg, projectRoot, name)
	err := output.Errorf(output.CodeWorktreeUnknown, "worktree not found: %s", name)
	if len(similar) > 0 {
		err = err.WithDetail("did_you_mean", similar)
		if !explicitJSON() {
			// Suggestions go to stderr so they never pollute the path on stdout.
			_, _ = fmt.Fprintln(os.Stderr, styles.Error.Render(fmt.Sprintf("Worktree not found: %s", name)))
			for i, s := range similar {
				_, _ = fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, s)
			}
		}
	}
	return worktreeContext{}, err
}

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var hooksWorktree string

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Inspect and run lifecycle hooks",
	Long: `Inspect and manually run the declarative shell hooks configured in .hydra/config.yaml.

DESCRIPTION
  Hooks run via sh -c in a chosen working directory. Each hook receives HYDRA_*
  environment variables describing the project, repo, branch, and worktree.

  Use --no-hooks on any command to skip hook execution globally.

SUBCOMMANDS
  ls          List configured hook events
  run <event> Run a hook chain for an event

ENVIRONMENT
  HYDRA_EVENT, HYDRA_PROJECT, HYDRA_PROJECT_ROOT, HYDRA_GROUP, HYDRA_REPO,
  HYDRA_BRANCH, HYDRA_WORKTREE_PATH, HYDRA_BARE_PATH

EXAMPLES
  $ hydra hooks ls
  $ hydra hooks run post_add --worktree api
  $ hydra hooks run post_add

EXIT CODES
  0  Success
  1  General error, hook_failed, worktree_unknown, or internal (unknown event name)
  2  not_in_project`,
}

var hooksLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List configured hook events",
	RunE:  runHooksLs,
}

var hooksRunCmd = &cobra.Command{
	Use:   "run <event>",
	Short: "Run hooks for an event",
	Args:  cobra.ExactArgs(1),
	RunE:  runHooksRun,
}

func init() {
	rootCmd.AddCommand(hooksCmd)
	hooksCmd.AddCommand(hooksLsCmd, hooksRunCmd)
	hooksRunCmd.Flags().StringVar(&hooksWorktree, "worktree", "", "Worktree directory name or group/name")
}

type hooksEventEntry struct {
	Event string `json:"event"`
	Count int    `json:"count"`
}

type hooksLsPayload struct {
	Events []hooksEventEntry `json:"events"`

	// Env names every variable a hook is given. Published so the contract is discoverable
	// instead of only documented: an agent writing a hook can learn what it receives without
	// reading the guide, and the docs gate asserts the guide's list against this. The guide
	// listed eight for a commit after two more were added.
	Env []string `json:"env"`
}

func runHooksLs(cmd *cobra.Command, args []string) error {
	events := config.HookEvents()
	entries := make([]hooksEventEntry, 0, len(events))
	for _, event := range events {
		hs, _ := cfg.HooksFor(event)
		entries = append(entries, hooksEventEntry{Event: event, Count: len(hs)})
	}

	return emit(cmd, fmt.Sprintf("%d hook event(s) configured", len(entries)), hooksLsPayload{Events: entries, Env: hooks.EnvKeys()}, nil, func() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-14s  %s\n",
			styles.Label.Render("EVENT"),
			styles.Label.Render("HOOKS"))
		for _, entry := range entries {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-14s  %d\n", entry.Event, entry.Count)
		}
	})
}

type hooksRunPayload struct {
	Event    string       `json:"event"`
	Worktree string       `json:"worktree,omitempty"`
	Cwd      string       `json:"cwd"`
	Result   hooks.Result `json:"result"`
}

func runHooksRun(cmd *cobra.Command, args []string) error {
	event := args[0]
	if _, ok := cfg.HooksFor(event); !ok {
		return output.Errorf(output.CodeInternal, "unknown hook event: %s", event)
	}

	wt, cwd, err := resolveHooksWorktree(event)
	if err != nil {
		return err
	}

	hctx := hooks.Context{
		Event:        event,
		Project:      cfg.Project,
		ProjectRoot:  projectRoot,
		Group:        wt.RepoContext.Group,
		Repo:         wt.RepoContext.Alias,
		Branch:       wt.Branch,
		WorktreePath: wt.Path,
		BarePath:     wt.RepoContext.BareRepo,
	}

	result, err := runHookEvent(event, hctx, cwd)
	if err != nil {
		return err
	}

	payload := hooksRunPayload{
		Event:    event,
		Worktree: wt.Qualified(),
		Cwd:      cwd,
		Result:   result,
	}
	return emit(cmd, fmt.Sprintf("ran %d hook(s) for %s", result.Ran, event), payload, result.Warnings, func() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Ran %d hook(s) for %s in %s\n", result.Ran, event, cwd)
	})
}

func resolveHooksWorktree(event string) (worktreeContext, string, error) {
	var wt worktreeContext

	if strings.TrimSpace(hooksWorktree) != "" {
		items, _ := collectWorktrees(cfg, projectRoot)
		resolved, err := resolveOneWorktree(items, hooksWorktree)
		if err != nil {
			if output.Classify(err).Code != output.CodeWorktreeUnknown {
				return worktreeContext{}, "", err
			}
			similar := findSimilarWorktreesByName(cfg, projectRoot, hooksWorktree)
			unknown := output.Errorf(output.CodeWorktreeUnknown, "worktree not found: %s", hooksWorktree)
			if len(similar) > 0 {
				unknown = unknown.WithDetail("did_you_mean", similar)
			}
			return worktreeContext{}, "", unknown
		}
		wt = resolved
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return worktreeContext{}, "", output.Wrap(output.CodeInternal, err, "failed to get current directory")
		}
		ctx := resolveCurrentHydraContext(wd, cfg, projectRoot)
		if ctx == nil {
			return worktreeContext{}, "", output.Errorf(output.CodeWorktreeUnknown,
				"current directory is not inside a hydra worktree; pass --worktree")
		}
		wt = *ctx
	}

	cwd := wt.Path
	if event == "post_remove" {
		cwd = projectRoot
	}
	return wt, cwd, nil
}

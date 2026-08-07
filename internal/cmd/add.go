package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/carry"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var (
	addFrom string
	addAs   string
)

var addCmd = &cobra.Command{
	Use:   "add [<alias> <branch>]",
	Short: "Create a worktree for a branch",
	Long: `Create a worktree as a real sibling directory under its group.

DESCRIPTION
  The worktree directory is <group>/<alias> for the repo's default branch, and
  <group>/<alias>-<slug> otherwise, where slug replaces "/" with "-". Pass --as to
  choose the directory name outright, which is how a long branch name gets a short
  directory.

  Upstream tracking is decided by where the branch actually exists:
    • on origin        -> a local branch WITH upstream origin/<branch>
    • locally only     -> attached as-is; no upstream is invented
    • nowhere yet      -> a new branch cut from the resolved base, no upstream
                          until you push (reported as local-only)

  The base for a brand-new branch resolves in this order:
    --from  ->  defaults.base_branch  ->  the repo's default_branch  ->  origin/HEAD

WHEN TO USE
  • Starting work on a new branch
  • Checking out an existing remote branch alongside your current work

EXAMPLES
  # Existing remote branch
  $ hydra add api stage

  # New branch from the resolved base
  $ hydra add api feat/login

  # New branch from an explicit base
  $ hydra add api hotfix/x --from prod

  # Short directory name for a long branch
  $ hydra add gileadeweb marcus/feat-2072958-excel-xlsx --as gileadeweb-excel-xlsx

FLAGS
  --from <branch>  base branch for a brand-new branch
  --as <name>      worktree directory name (overrides the derived name)

HOOKS
  Runs the post_add chain with cwd set to the new worktree. A failing hook does NOT
  remove the worktree: it was created correctly. Fix the hook and run
  "hydra hooks run post_add".

EXIT CODES
  0  Success
  1  repo_unknown, bare_missing, branch_unknown, worktree_exists,
     worktree_name_conflict, git_failed, hook_failed
  2  not_in_project, config_version_unsupported, project_unknown

SEE ALSO
  • hydra remove - delete a worktree
  • hydra path   - print a worktree's path
  • hydra list   - list worktrees`,
	Args: cobra.MaximumNArgs(2),
	RunE: runAdd,
}

func init() {
	addCmd.Flags().StringVar(&addFrom, "from", "", "base branch for a brand-new branch")
	addCmd.Flags().StringVar(&addAs, "as", "", "worktree directory name")
	rootCmd.AddCommand(addCmd)
}

// addJSON is add's payload: the worktree, plus whether THIS invocation created it.
//
// The embedded struct is promoted in JSON, so the shape callers already read is unchanged
// and `disposition` is purely additive. It exists because invariant 3 requires a
// convergent no-op to be *reported* as skipped, and the envelope is the only place a
// caller is allowed to read that from — summary text is explicitly not a contract.
type addJSON struct {
	worktreeJSON
	Disposition string `json:"disposition"`

	// Carried is the per-entry outcome for this worktree's declared files. Warnings say why
	// something is missing; this also says which files were placed or skipped, which is what a
	// caller checking "can this worktree actually run" needs. Omitted when nothing is declared.
	Carried []carry.Result `json:"carried,omitempty"`
}

func runAdd(cmd *cobra.Command, args []string) error {
	alias, branch, err := resolveAddTarget(args)
	if err != nil {
		return err
	}

	repo, err := resolveRepoByAlias(cfg, projectRoot, alias)
	if err != nil {
		return err
	}

	dirName := strings.TrimSpace(addAs)
	if dirName == "" {
		dirName = worktreeDirName(repo, branch)
	} else if err := validatePathSegment("--as", dirName); err != nil {
		return output.Wrap(output.CodeInternal, err, "invalid --as value")
	}

	target := worktreePath(projectRoot, repo.Group, dirName)

	// Invariant 3 promises every command is convergent: twice is a no-op that exits 0 and
	// reports `skipped`. start, apply and clone already translate worktree_exists into a
	// skip here — startOne does it on this exact call — and add was the one command that
	// let it escape as exit 1, so a provisioning script re-running its own steps died on
	// the second `add`.
	//
	// Only THIS branch already having a worktree is the desired state. A directory held by
	// a DIFFERENT branch is worktree_name_conflict and still fails.
	created := true
	if err := checkWorktreeNameConflict(repo, projectRoot, dirName, branch); err != nil {
		if output.Classify(err).Code != output.CodeWorktreeExists {
			return err
		}
		created = false
	}

	var carried carryOutcome
	if created {
		w, err := createWorktreeForBranch(cfg, repo, target, branch, addFrom)
		if err != nil {
			return err
		}
		carried = w
	}

	wt, ok := findRepoWorktreeByBranch(repo, branch)
	if !ok {
		return output.Errorf(output.CodeGitFailed,
			"worktree for %q was created at %s but git does not report it", branch, target).
			WithDetail("branch", branch).
			WithDetail("path", target)
	}

	item, trackErr := wt.withTracking()
	if idx, err := newTopicIndex(projectRoot); err == nil {
		idx.decorate(&item)
	}
	warnings := carried.Warnings
	if trackErr != nil {
		warnings = append(warnings, fmt.Sprintf("%s: %v", branch, trackErr))
	}

	// Hooks fire only on creation, matching fanout's rule for start: re-running add must
	// not re-provision a worktree that already exists. Retrying a failed post_add is
	// `hydra hooks run post_add --worktree <name>`, which is explicit about what it reruns.
	var addErr *output.Error
	var hookErr error
	if created {
		var hookResult hooks.Result
		hookResult, hookErr = runHookEvent("post_add", hooksContextFor(repo, branch, wt.Path), wt.Path)
		warnings = append(warnings, hookResult.Warnings...)

		// The hook failure has to ride the envelope. It used to be returned only as the
		// process error, so `add` exited 1 while stdout said `outcome: success` with an empty
		// warnings array — the worktree was created correctly and the failure was invisible to
		// anything reading the envelope.
		if hookErr != nil {
			addErr = output.Classify(hookErr)
		}
	}

	disposition, verb := "created", "created"
	if !created {
		disposition, verb = "skipped", "already present"
	}
	emitErr := emitResult(cmd, output.Result{
		Summary:  fmt.Sprintf("worktree %s %s for %s", wt.Qualified(), verb, wt.BranchLabel()),
		Data:     addJSON{worktreeJSON: item, Disposition: disposition, Carried: carried.Results},
		Warnings: warnings,
		Err:      addErr,
	}, func() {
		wd, _ := os.Getwd()
		cdHint, switchHint := navigationHints(wd, wt)
		fmt.Println()
		if created {
			fmt.Println(styles.Success.Render("✓ Worktree created"))
		} else {
			// Printing "created" over a worktree that was already there is the text half of
			// the same lie the exit code used to tell.
			fmt.Println(styles.Success.Render("✓ Worktree already present"))
		}
		fmt.Printf("  Repo:   %s/%s\n", repo.Group, repo.Alias)
		fmt.Printf("  Branch: %s\n", wt.BranchLabel())
		fmt.Printf("  Path:   %s\n", wt.Path)
		if item.Upstream != nil {
			fmt.Printf("  Upstream: %s\n", *item.Upstream)
		} else {
			fmt.Println("  Upstream: local-only (push to create it)")
		}
		fmt.Println()
		fmt.Println(cdHint)
		fmt.Println(switchHint)
	})
	if emitErr != nil {
		return emitErr
	}
	// The worktree is real and reported; only the hook failed.
	return hookErr
}

// resolveAddTarget determines the repo alias and branch, prompting only when a
// terminal is genuinely attached.
func resolveAddTarget(args []string) (string, string, error) {
	if len(args) == 2 {
		return strings.TrimSpace(args[0]), strings.TrimSpace(args[1]), nil
	}

	if !interactive() {
		if len(args) == 0 {
			return "", "", output.Errorf(output.CodeRepoUnknown,
				"a repo alias and a branch are required: hydra add <alias> <branch>")
		}
		return "", "", output.Errorf(output.CodeBranchUnknown,
			"a branch is required: hydra add %s <branch>", args[0])
	}

	alias := ""
	if len(args) == 1 {
		alias = strings.TrimSpace(args[0])
	} else {
		repos := cfg.Repos()
		if len(repos) == 0 {
			return "", "", output.Errorf(output.CodeRepoUnknown,
				"no repos registered; run \"hydra repo add <url>\" first")
		}
		options := make([]huh.Option[string], 0, len(repos))
		for _, ref := range repos {
			options = append(options, huh.NewOption(ref.Group+"/"+ref.Alias, ref.Alias))
		}
		alias = repos[0].Alias
		if err := huh.NewSelect[string]().Title("Repo").Options(options...).Value(&alias).Run(); err != nil {
			return "", "", output.Wrap(output.CodeInternal, err, "cancelled")
		}
	}

	repo, err := resolveRepoByAlias(cfg, projectRoot, alias)
	if err != nil {
		return "", "", err
	}

	choices, defaultBranch, err := branchChoicesForRepo(repo)
	if err != nil {
		return "", "", err
	}

	const newBranchSentinel = "\x00new"
	options := make([]huh.Option[string], 0, len(choices)+1)
	options = append(options, huh.NewOption("+ new branch…", newBranchSentinel))
	for _, choice := range choices {
		if choice.HasWorktree {
			continue
		}
		options = append(options, huh.NewOption(choice.DisplayName, choice.Name))
	}

	branch := defaultBranch
	if err := huh.NewSelect[string]().Title("Branch").Options(options...).Value(&branch).Run(); err != nil {
		return "", "", output.Wrap(output.CodeInternal, err, "cancelled")
	}

	if branch == newBranchSentinel {
		branch = ""
		if err := huh.NewInput().Title("New branch name").Value(&branch).Run(); err != nil {
			return "", "", output.Wrap(output.CodeInternal, err, "cancelled")
		}
		branch = strings.TrimSpace(branch)
		if branch == "" {
			return "", "", output.Errorf(output.CodeBranchUnknown, "a branch name is required")
		}
	}

	return alias, branch, nil
}

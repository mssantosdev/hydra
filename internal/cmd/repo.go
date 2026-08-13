package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	repoRemoveYes bool
	repoAdopt     bool
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Register, list and unregister repositories in this workspace",
	Long: `Manage the repositories a workspace contains.

DESCRIPTION
  One front door for getting a repository into a workspace, replacing the separate
  "clone" and "adopt" commands.

    repo add <url|path>        clone it, creating the worktrees you ask for
    repo add <path> --adopt    track a checkout you already have, in place

  --adopt is REQUIRED rather than inferred. A local path is a perfectly ordinary clone
  SOURCE — "git clone /some/path" is normal — so guessing from the argument would
  silently turn "clone from here" into "track this directory in place", which is a
  different operation on a different directory.

SUBCOMMANDS
  add     register a repository from a URL or an existing checkout
  list    list the registered repositories
  remove  unregister a repository (git data and worktrees are left alone)

EXAMPLES
  $ hydra repo add https://github.com/acme/api.git --group backend --branches main
  $ hydra repo add ../existing-checkout --adopt --group backend
  $ hydra repo add git@github.com:acme/web.git --group frontend --all
  $ hydra repo list
  $ hydra repo remove api --yes

EXIT CODES
  0  Success
  1  repo_unknown, worktree_exists, git_failed, branch_unknown
  2  not_in_project
  4  partial_failure (some worktrees failed)
  7  needs_input`,
}

var repoAddCmd = &cobra.Command{
	Use:   "add <url|path>",
	Short: "Register a repository from a URL or an existing checkout",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runRepoAdd,
}

var repoListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List the registered repositories",
	Args:    cobra.NoArgs,
	RunE:    runRepoList,
}

var repoRemoveCmd = &cobra.Command{
	Use:     "remove <alias>",
	Aliases: []string{"rm"},
	Short:   "Unregister a repository without deleting anything",
	Args:    cobra.MaximumNArgs(1),
	RunE:    runRepoRemove,
}

func init() {
	rootCmd.AddCommand(repoCmd)
	repoCmd.AddCommand(repoAddCmd, repoListCmd, repoRemoveCmd)

	// add carries the union of what the two paths need. A flag that does not apply to
	// the resolved path is reported rather than ignored, so nothing is silently
	// dropped.
	repoAddCmd.Flags().StringVar(&cloneGroup, "group", "", "Group directory for the worktrees")
	repoAddCmd.Flags().StringVar(&cloneAlias, "as", "", "Repository alias (default: derived from the URL or directory)")
	repoAddCmd.Flags().StringSliceVar(&cloneBranches, "branches", nil, "Branches to create worktrees for")
	repoAddCmd.Flags().BoolVar(&cloneAll, "all", false, "Create a worktree for every branch on origin")
	repoAddCmd.Flags().BoolVar(&cloneDryRun, "dry-run", false, "Report the plan without touching disk")
	repoAddCmd.Flags().BoolVar(&repoAdopt, "adopt", false,
		"Track an existing checkout in place instead of cloning it")

	repoRemoveCmd.Flags().BoolVar(&repoRemoveYes, "yes", false, "Skip the confirmation prompt")
	repoRemoveCmd.ValidArgsFunction = completeRepoAliases
}

// runRepoAdd dispatches on WHAT the argument is, not on which flags were passed.
//
// It CLONES by default and adopts only under --adopt. A local path is a normal clone
// source (`git clone /local/path` is valid), not an adopt signal — the two intents
// target different directories, so the caller states which one with --adopt.
func runRepoAdd(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	if len(args) == 0 {
		return output.Errorf(output.CodeNeedsInput,
			"a repository URL, or a path with --adopt, is required").
			WithDetail("missing", []string{"<url|path>"})
	}

	source := strings.TrimSpace(args[0])
	if !repoAdopt {
		return runClone(cmd, []string{source})
	}

	// Branch selection has no meaning for an existing checkout: it already has the
	// branch it is on. Saying so beats silently dropping the flag.
	if len(cloneBranches) > 0 || cloneAll {
		return output.Errorf(output.CodeInternal,
			"--branches and --all do not apply with --adopt; the checkout already has a branch").
			WithDetail("path", source)
	}
	if cloneGroup == "" {
		return output.Errorf(output.CodeNeedsInput,
			"a group is required when adopting a checkout").
			WithDetail("missing", []string{"--group"})
	}
	adoptGroup = cloneGroup
	adoptAlias = cloneAlias
	return runAdopt(cmd, []string{source})
}

type repoEntryJSON struct {
	Group         string `json:"group"`
	Alias         string `json:"alias"`
	Remote        string `json:"remote"`
	DefaultBranch string `json:"default_branch,omitempty"`
	BarePath      string `json:"bare_path"`
	// BareExists distinguishes "registered" from "present on disk", which is exactly
	// the half-built state an interrupted add leaves and doctor reports.
	BareExists bool `json:"bare_exists"`
	Worktrees  int  `json:"worktrees"`

	// Branches is the shape the manifest declares for this repo, so `repo list` answers
	// "what should this workspace look like" and not only "what is on disk right now".
	// Absent means the manifest declares nothing and restore falls back to the default
	// branch.
	Branches []string `json:"branches,omitempty"`
}

type repoListJSON struct {
	Repos []repoEntryJSON `json:"repos"`
	Total int             `json:"total"`
}

func runRepoList(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}

	payload := repoListJSON{}
	for _, repo := range allRepoContexts(cfg, projectRoot) {
		entry := repoEntryJSON{
			Group:         repo.Group,
			Alias:         repo.Alias,
			Remote:        repo.Remote,
			DefaultBranch: repo.DefaultBranch,
			BarePath:      repo.BareRepo,
		}
		// The manifest is the only source for the declared shape: repoContext carries what
		// is on disk, and the declaration is deliberately not derived from that.
		if ref, ok := cfg.FindRepo(repo.Alias); ok {
			entry.Branches = ref.Repo.Branches
		}
		if _, err := os.Stat(repo.BareRepo); err == nil {
			entry.BareExists = true
			if worktrees, err := listRepoWorktrees(repo); err == nil {
				entry.Worktrees = len(worktrees)
			}
		}
		payload.Repos = append(payload.Repos, entry)
	}
	payload.Total = len(payload.Repos)

	return emit(cmd, fmt.Sprintf("%d registered repo(s)", payload.Total), payload, nil, func() {
		if payload.Total == 0 {
			fmt.Println("No repositories registered. \"hydra repo add <url|path>\" adds one.")
			return
		}
		fmt.Println()
		for _, entry := range payload.Repos {
			state := ""
			if !entry.BareExists {
				state = styles.Error.Render("  (bare missing — run \"hydra doctor\")")
			}
			fmt.Printf("  %-14s %-20s %d worktree(s)%s\n",
				styles.Label.Render(entry.Group), entry.Alias, entry.Worktrees, state)
		}
		fmt.Println()
	})
}

type repoRemoveJSON struct {
	Group string `json:"group"`
	Alias string `json:"alias"`
	// Kept names what was deliberately NOT deleted, so a caller is not left guessing
	// whether its data survived.
	Kept []string `json:"kept"`
}

// runRepoRemove unregisters a repository and deletes nothing.
//
// Removing git data is a separate, irreversible decision; conflating it with
// "hydra should stop tracking this" is how a registry edit becomes data loss. The
// response says what was kept so that is unambiguous.
func runRepoRemove(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	if len(args) == 0 {
		return output.Errorf(output.CodeNeedsInput, "a repository alias is required").
			WithDetail("missing", []string{"<alias>"}).
			WithDetail("available", registeredAliases())
	}

	alias := strings.TrimSpace(args[0])
	ref, ok := cfg.FindRepo(alias)
	if !ok {
		return output.Errorf(output.CodeRepoUnknown,
			"repository %q is not registered", alias).
			WithDetail("repo", alias).
			WithDetail("known", registeredAliases())
	}

	repo := repoContextFor(cfg, projectRoot, ref)
	worktrees, _ := listRepoWorktrees(repo)
	if len(worktrees) > 0 && !repoRemoveYes {
		// Unregistering a repo that still has worktrees leaves them orphaned from
		// hydra's view, so it needs an explicit yes rather than a silent success.
		if !interactive() {
			return output.Errorf(output.CodeNeedsInput,
				"%s still has %d worktree(s); pass --yes to unregister it anyway",
				alias, len(worktrees)).
				WithDetail("missing", []string{"--yes"}).
				WithDetail("repo", alias).
				WithDetail("worktrees", len(worktrees))
		}
		// A terminal used to fall straight through here: the branch above refused
		// only when nobody could be asked, so the interactive case asserted that an
		// explicit yes was needed and then never asked for one.
		confirm := false
		title := fmt.Sprintf("Unregister %s? %d worktree(s) stay on disk",
			alias, len(worktrees))
		if err := huh.NewConfirm().Title(title).Value(&confirm).Run(); err != nil {
			return output.Wrap(output.CodeInternal, err, "cancelled")
		}
		if !confirm {
			return output.Errorf(output.CodeInternal, "cancelled")
		}
	}

	cfg.RemoveRepo(ref.Group, alias)
	if err := cfg.Save(projectConfigPath); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to save the manifest")
	}

	payload := repoRemoveJSON{
		Group: ref.Group,
		Alias: alias,
		Kept:  []string{repo.BareRepo},
	}
	for _, wt := range worktrees {
		payload.Kept = append(payload.Kept, wt.Path)
	}

	return emit(cmd, fmt.Sprintf("unregistered %s; %d path(s) left on disk", alias, len(payload.Kept)),
		payload, nil, func() {
			fmt.Printf("Unregistered %s/%s. Nothing was deleted:\n", ref.Group, alias)
			for _, path := range payload.Kept {
				fmt.Printf("  %s\n", path)
			}
		})
}

func registeredAliases() []string {
	if cfg == nil {
		return nil
	}
	var out []string
	for _, ref := range cfg.Repos() {
		out = append(out, ref.Alias)
	}
	sort.Strings(out)
	return out
}

var _ = config.Repo{}

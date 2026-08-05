package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/fanout"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var (
	cloneAlias    string
	cloneGroup    string
	cloneBranches []string
	cloneAll      bool
	cloneDryRun   bool
)

// CloneOptions is the input to performClone, shared with `hydra new`.
type CloneOptions struct {
	URL         string
	Alias       string
	Group       string
	Branches    []string
	AllBranches bool
	Interactive bool
}

type cloneResult struct {
	Project   string         `json:"project"`
	Root      string         `json:"root"`
	Group     string         `json:"group"`
	Repo      string         `json:"repo"`
	Remote    string         `json:"remote"`
	BarePath  string         `json:"bare_path"`
	Worktrees []worktreeJSON `json:"worktrees"`
}

var cloneCmd = &cobra.Command{
	Use:   "clone <url>",
	Short: "Clone a repository into the project as worktrees",
	Long: `Clone a remote repository and create one worktree per selected branch.

DESCRIPTION
  Creates <bare_dir>/<alias>.git holding git data only, then creates a real sibling
  directory under <group>/ for each selected branch.

  The bare repository is built with "git init --bare" plus an explicit
  remote.origin.fetch refspec and a fetch, never with "git clone --bare". That is
  what makes refs/remotes/origin/* authoritative and lets every worktree get a
  real upstream. A branch that exists on origin becomes a local branch WITH
  upstream tracking configured.

WHEN TO USE
  • Onboarding an existing remote repository into a hydra workspace
  • Adding a second repository to a workspace that already has one

EXAMPLES
  # Clone and pick branches interactively
  $ hydra clone git@github.com:acme/api.git

  # Non-interactive: name the alias, group, and branches
  $ hydra clone git@github.com:acme/api.git --alias api --group backend --branches main,stage

  # Every branch on origin
  $ hydra clone git@github.com:acme/api.git --alias api --group backend --all

  # Show what would happen
  $ hydra clone git@github.com:acme/api.git --dry-run

FLAGS
  --alias <name>       repo alias; also the bare filename and worktree dir base name
  --group <name>       group directory to place worktrees in
  --branches <list>    comma-separated branches to create worktrees for
  --all                create a worktree for every branch on origin
  --yes                skip confirmation prompts
  --dry-run            report the plan without touching disk

HOOKS
  Runs the post_clone chain once per created worktree, with cwd set to that
  worktree. A failing hook never deletes a correctly-created worktree.

EXIT CODES
  0  Success
  1  General error (git_failed, hook_failed)
  1  repo_unknown / bare_missing while verifying the result
  2  not_in_project, config_version_unsupported
  4  partial_failure (the repo cloned, but some worktrees failed)

SEE ALSO
  • hydra new    - bootstrap a new project and its first repo
  • hydra adopt  - import an existing local checkout
  • hydra add    - add another worktree later`,
	Args: cobra.MaximumNArgs(1),
	RunE: runClone,
}

func init() {
	cloneCmd.Flags().StringVar(&cloneAlias, "alias", "", "repo alias (also the bare filename and worktree directory base name)")
	cloneCmd.Flags().StringVar(&cloneGroup, "group", "", "group directory for the worktrees")
	cloneCmd.Flags().StringSliceVar(&cloneBranches, "branches", nil, "branches to create worktrees for")
	cloneCmd.Flags().BoolVar(&cloneAll, "all", false, "create a worktree for every branch on origin")
	cloneCmd.Flags().BoolVar(&cloneDryRun, "dry-run", false, "report the plan without touching disk")
}

func runClone(cmd *cobra.Command, args []string) error {
	url := ""
	if len(args) == 1 {
		url = strings.TrimSpace(args[0])
	}

	// clone runs outside the PersistentPreRunE project load, so it resolves the
	// workspace itself and creates one in place when there is none.
	if err := loadProject(); err != nil {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return output.Wrap(output.CodeInternal, wdErr, "failed to resolve the working directory")
		}
		created, configPath, newCfg, createErr := createProjectRootAt(wd, filepath.Base(wd))
		if createErr != nil {
			return output.Wrap(output.CodeInternal, createErr, "failed to create a workspace for the clone")
		}
		cfg = newCfg
		projectRoot = created
		projectConfigPath = configPath
		if err := registry.Register(cfg.Project, projectRoot); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to register project %q", cfg.Project)
		}
	}

	opts, err := resolveCloneOptions(url)
	if err != nil {
		return err
	}

	if cloneDryRun {
		return emit(cmd, fmt.Sprintf("dry run: would clone %s as %s/%s", opts.URL, opts.Group, opts.Alias), map[string]any{
			"project":   cfg.Project,
			"root":      projectRoot,
			"group":     opts.Group,
			"repo":      opts.Alias,
			"remote":    opts.URL,
			"bare_path": cfg.BarePath(projectRoot, opts.Alias),
			"branches":  branchesOrEmpty(opts.Branches),
			// A dry run does no network I/O, so it cannot know what the remote has.
			// `branches: null` read as "the remote has none"; this says which it is.
			"branches_source": branchesSource(opts),
			"dry_run":         true,
		}, nil, func() {
			fmt.Println()
			fmt.Println(styles.Label.Render("Plan (dry run):"))
			fmt.Printf("  bare:     %s\n", cfg.BarePath(projectRoot, opts.Alias))
			for _, branch := range opts.Branches {
				fmt.Printf("  worktree: %s/%s (branch %s)\n", opts.Group, opts.Alias, branch)
			}
		})
	}

	result, warnings, err := performClone(opts, cfg, projectConfigPath, projectRoot)
	if err != nil {
		return err
	}

	emitErr := emit(cmd, fmt.Sprintf("cloned %s/%s with %d worktree(s)", result.Group, result.Repo, len(result.Worktrees)), result, warnings, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Repository cloned"))
		fmt.Printf("  Repo: %s/%s\n", result.Group, result.Repo)
		for _, wt := range result.Worktrees {
			fmt.Printf("  %s  (%s)\n", wt.Path, wt.Branch)
		}
	})
	if emitErr != nil {
		return emitErr
	}
	return nil
}

// resolveCloneOptions fills in the alias, group, and branch selection, prompting
// only when a terminal is genuinely attached.
func resolveCloneOptions(url string) (*CloneOptions, error) {
	opts := &CloneOptions{
		URL:         url,
		Alias:       strings.TrimSpace(cloneAlias),
		Group:       strings.TrimSpace(cloneGroup),
		Branches:    cloneBranches,
		AllBranches: cloneAll,
		Interactive: interactive(),
	}

	if opts.URL == "" {
		if !opts.Interactive {
			return nil, output.Errorf(output.CodeNeedsInput,
				"a repository URL is required; pass it as the first argument").
				WithDetail("missing", []string{"<url|path>"})
		}
		if err := huh.NewInput().Title("Repository URL").Value(&opts.URL).Run(); err != nil {
			return nil, output.Wrap(output.CodeInternal, err, "cancelled")
		}
		opts.URL = strings.TrimSpace(opts.URL)
		if opts.URL == "" {
			return nil, output.Errorf(output.CodeInternal, "a repository URL is required")
		}
	}

	if opts.Alias == "" {
		opts.Alias = aliasFromURL(opts.URL)
	}
	if err := validatePathSegment("alias", opts.Alias); err != nil {
		return nil, output.Wrap(output.CodeInternal, err, "invalid alias")
	}

	if opts.Group == "" {
		if opts.Interactive {
			if err := huh.NewInput().Title("Group").
				Description("Directory the worktrees live in (e.g. backend)").
				Value(&opts.Group).Run(); err != nil {
				return nil, output.Wrap(output.CodeInternal, err, "cancelled")
			}
		}
		opts.Group = strings.TrimSpace(opts.Group)
	}
	if opts.Group == "" {
		return nil, output.Errorf(output.CodeInternal,
			"a group is required; pass --group <name>")
	}
	if err := validatePathSegment("group", opts.Group); err != nil {
		return nil, output.Wrap(output.CodeInternal, err, "invalid group")
	}

	// An already-registered alias is not automatically an error: re-running a clone
	// that was interrupted must be able to finish. performClone refuses only when the
	// registered remote actually differs.
	if existing, exists := cfg.FindRepo(opts.Alias); exists && existing.Group != opts.Group {
		return nil, output.Errorf(output.CodeWorktreeExists,
			"repo alias %q is already registered under group %q", opts.Alias, existing.Group).
			WithDetail("alias", opts.Alias).
			WithDetail("registered_group", existing.Group).
			WithDetail("requested_group", opts.Group)
	}

	return opts, nil
}

// performClone creates the bare repository and the selected worktrees. It is the
// shared engine behind `hydra clone` and `hydra new --remote`.
func performClone(opts *CloneOptions, c *config.Config, configPath, root string) (cloneResult, []string, error) {
	result := cloneResult{
		Project:  c.Project,
		Root:     root,
		Group:    opts.Group,
		Repo:     opts.Alias,
		Remote:   opts.URL,
		BarePath: c.BarePath(root, opts.Alias),
	}
	var warnings []string

	if err := os.MkdirAll(filepath.Dir(result.BarePath), 0750); err != nil {
		return result, nil, output.Wrap(output.CodeInternal, err, "failed to create the bare directory")
	}

	// Register the repo BEFORE any network work. InitBareWithRemote performs a full
	// fetch, and an interruption during it used to leave <bare_dir>/<alias>.git on
	// disk with nothing in .hydra/config.yaml referencing it — an orphan that every command
	// ignored while the user saw a directory that looked cloned. Registering first
	// means an interrupted clone always leaves a repo hydra can see, `hydra doctor`
	// can diagnose, and re-running this command can finish.
	alreadyRegistered := false
	if existing, ok := c.FindRepo(opts.Alias); ok {
		alreadyRegistered = true
		if existing.Repo.Remote != "" && opts.URL != "" && existing.Repo.Remote != opts.URL {
			return result, nil, output.Errorf(output.CodeWorktreeExists,
				"repo alias %q is already registered with a different remote (%s)", opts.Alias, existing.Repo.Remote).
				WithDetail("alias", opts.Alias).
				WithDetail("registered_remote", existing.Repo.Remote).
				WithDetail("requested_remote", opts.URL)
		}
	} else {
		c.SetRepo(opts.Group, opts.Alias, config.Repo{Remote: opts.URL})
		if err := c.Save(configPath); err != nil {
			return result, nil, output.Wrap(output.CodeInternal, err, "failed to save config")
		}
	}

	bareExisted := false
	if _, err := os.Stat(result.BarePath); err == nil {
		bareExisted = true
	}

	if bareExisted {
		// Resume: bring a possibly half-built bare repo up to spec instead of
		// failing or discarding whatever was already fetched.
		log.Debug("repairing existing bare repository", "path", result.BarePath)
		warnings = append(warnings, fmt.Sprintf("%s: bare repository already existed; completing it instead of re-cloning", opts.Alias))
		if err := git.RepairBareRemote(result.BarePath, opts.URL); err != nil {
			return result, warnings, output.Wrap(output.CodeGitFailed, err,
				"failed to complete the existing bare repository for %q", opts.Alias)
		}
	} else {
		log.Debug("creating bare repository", "path", result.BarePath, "remote", opts.URL)
		if err := git.InitBareWithRemote(result.BarePath, opts.URL); err != nil {
			// This call created the bare dir, so removing it is safe and complete.
			_ = os.RemoveAll(result.BarePath)
			if !alreadyRegistered {
				c.RemoveRepo(opts.Group, opts.Alias)
				_ = c.Save(configPath)
			}
			return result, warnings, output.Wrap(output.CodeGitFailed, err, "failed to create the bare repository for %q", opts.Alias)
		}
	}

	defaultBranch, defaultErr := git.GetRemoteDefaultBranch(result.BarePath)
	if defaultErr != nil {
		warnings = append(warnings, fmt.Sprintf("%s: origin/HEAD is not set; run \"hydra doctor --fix\"", opts.Alias))
	}

	repo := repoContext{
		Group:         opts.Group,
		Alias:         opts.Alias,
		Remote:        opts.URL,
		DefaultBranch: defaultBranch,
		BareRepo:      result.BarePath,
	}

	// Record the resolved default branch now that the fetch has actually happened.
	c.SetRepo(opts.Group, opts.Alias, config.Repo{Remote: opts.URL, DefaultBranch: defaultBranch})
	if err := c.Save(configPath); err != nil {
		return result, warnings, output.Wrap(output.CodeInternal, err, "failed to save config")
	}

	branches, err := resolveCloneBranches(opts, repo, defaultBranch)
	if err != nil {
		return result, warnings, err
	}

	// Converge each branch through the shared engine.
	//
	// SerialPerRepo is TRUE here, unlike sync: `git worktree add` with upstream
	// config contends on the bare repo's config.lock, and 8 concurrent adds were
	// measured to leave only 3 successes — the failures producing worktrees with no
	// upstream at all, a silent partial. Creation must never run concurrently within
	// one bare repository.
	targets := make([]fanout.Target, 0, len(branches))
	for _, branch := range branches {
		targets = append(targets, fanout.Target{
			Group:    repo.Group,
			Repo:     repo.Alias,
			Branch:   branch,
			Path:     worktreePath(root, repo.Group, worktreeDirName(repo, branch)),
			BareRepo: repo.BareRepo,
		})
	}

	var hookMu sync.Mutex
	results := fanout.Run(context.Background(), targets, fanout.Config{
		SerialPerRepo: true,
		Hook: func(_ context.Context, t fanout.Target) ([]string, error) {
			// runHookEventForProject is not documented as concurrency-safe and writes
			// through shared config; SerialPerRepo already prevents overlap here, and
			// the lock makes that independent of the engine's scheduling.
			hookMu.Lock()
			defer hookMu.Unlock()
			hookResult, err := runHookEventForProject(c, root, "post_clone",
				hooksContextFor(repo, t.Branch, t.Path), t.Path)
			return hookResult.Warnings, err
		},
	}, func(_ context.Context, t fanout.Target) fanout.ItemResult {
		dirName := worktreeDirName(repo, t.Branch)

		// Convergence: a worktree that already exists for THIS branch at THIS path is
		// the desired state, not a failure. Without this, re-running clone on a
		// complete repository reported git_failed "no worktree could be created" —
		// every branch counted as a failure precisely because it was already correct.
		// A directory taken by a DIFFERENT branch stays a real conflict.
		if err := checkWorktreeNameConflict(repo, root, dirName, t.Branch); err != nil {
			if output.Classify(err).Code == output.CodeWorktreeExists {
				return fanout.ItemResult{Disposition: fanout.Skipped, Reason: "already present"}
			}
			return fanout.ItemResult{Disposition: fanout.Failed, Reason: err.Error(), Err: err}
		}
		if err := createWorktreeForBranch(c, repo, t.Path, t.Branch, ""); err != nil {
			return fanout.ItemResult{Disposition: fanout.Failed, Reason: err.Error(), Err: err}
		}
		return fanout.ItemResult{Disposition: fanout.Created, Reason: "created"}
	})

	var failures []map[string]string
	var created []string
	for _, item := range results {
		if item.Disposition == fanout.Failed {
			failures = append(failures, map[string]string{"branch": item.Target.Branch, "error": item.Reason})
			continue
		}
		if item.Disposition == fanout.Created {
			created = append(created, item.Target.Path)
		}
		warnings = append(warnings, item.HookWarnings...)

		// Report created AND skipped worktrees: a converged clone still has to say
		// what is on disk, or a caller cannot tell success from "did nothing".
		wt, ok := findRepoWorktreeByBranch(repo, item.Target.Branch)
		if !ok {
			failures = append(failures, map[string]string{
				"branch": item.Target.Branch,
				"error":  "worktree was created but git does not report it",
			})
			continue
		}
		entry, trackErr := wt.withTracking()
		if idx, err := newTopicIndex(root); err == nil {
			idx.decorate(&entry)
		}
		if trackErr != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", item.Target.Branch, trackErr))
		}
		result.Worktrees = append(result.Worktrees, entry)
	}

	if len(failures) > 0 {
		scope := fanout.RollbackScope{
			Enabled:            true,
			CreatedThisCall:    created,
			BareWasNew:         !bareExisted,
			RegistrationWasNew: !alreadyRegistered,
		}
		// Undo only on TOTAL failure, and only what this call created: on a resume the
		// bare repo and the registration predate us and must survive so the next
		// attempt can finish.
		if scope.ShouldRollback(results) {
			for _, path := range scope.CreatedThisCall {
				_ = os.RemoveAll(path)
			}
			if scope.BareWasNew {
				_ = os.RemoveAll(result.BarePath)
			}
			if scope.RegistrationWasNew && scope.BareWasNew {
				c.RemoveRepo(opts.Group, opts.Alias)
				_ = c.Save(configPath)
			}
			return result, warnings, output.Errorf(output.CodeGitFailed,
				"no worktree could be created for %q", opts.Alias).
				WithDetail("failures", failures)
		}
		return result, warnings, output.Errorf(output.CodePartialFailure,
			"%d of %d worktrees failed for %q", len(failures), len(branches), opts.Alias).
			WithDetail("failures", failures).
			WithDetail("created", len(result.Worktrees))
	}

	return result, warnings, nil
}

// resolveCloneBranches decides which branches get worktrees.
func resolveCloneBranches(opts *CloneOptions, repo repoContext, defaultBranch string) ([]string, error) {
	available, err := git.ListRemoteBranchesCached(repo.BareRepo)
	if err != nil {
		return nil, output.Wrap(output.CodeGitFailed, err, "failed to list branches on origin")
	}
	if len(available) == 0 {
		return nil, output.Errorf(output.CodeBranchUnknown, "origin has no branches")
	}

	known := make(map[string]struct{}, len(available))
	names := make([]string, 0, len(available))
	for _, branch := range available {
		known[branch.Name] = struct{}{}
		names = append(names, branch.Name)
	}

	if opts.AllBranches {
		return names, nil
	}

	if len(opts.Branches) > 0 {
		// Asking for the same branch twice is a harmless request, not an error:
		// dedupe rather than failing on the second worktree.
		seen := make(map[string]struct{}, len(opts.Branches))
		requested := make([]string, 0, len(opts.Branches))
		for _, branch := range opts.Branches {
			branch = strings.TrimSpace(branch)
			if branch == "" {
				continue
			}
			if _, ok := known[branch]; !ok {
				return nil, output.Errorf(output.CodeBranchUnknown,
					"branch %q does not exist on origin", branch).
					WithDetail("branch", branch).
					WithDetail("available", names)
			}
			if _, dup := seen[branch]; dup {
				continue
			}
			seen[branch] = struct{}{}
			requested = append(requested, branch)
		}
		if len(requested) == 0 {
			return nil, output.Errorf(output.CodeBranchUnknown, "no branches selected")
		}
		return requested, nil
	}

	if !opts.Interactive {
		// Non-interactive with no selection: the default branch is the only
		// defensible choice, and it always exists.
		if defaultBranch != "" {
			return []string{defaultBranch}, nil
		}
		return []string{git.GetDefaultBranch(available)}, nil
	}

	choices, resolvedDefault, err := branchChoicesForRepo(repo)
	if err != nil {
		return nil, err
	}

	options := make([]huh.Option[string], 0, len(choices))
	var selected []string
	for _, choice := range choices {
		options = append(options, huh.NewOption(choice.DisplayName, choice.Name))
	}
	// Pre-selection must be assigned BEFORE the form is built: huh binds the
	// pointer, so assigning afterwards silently discards the default.
	if resolvedDefault != "" {
		selected = []string{resolvedDefault}
	}

	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Branches").
			Description("Create a worktree for each selected branch (/ to filter)").
			Options(options...).
			// Bound the visible window. huh sets no default height, so a repository
			// with 140 branches renders all of them at once; filtering — which huh
			// enables by default under "/" — is the practical way through a list that
			// long, and it is unreachable behind a wall of options.
			Height(15).
			Value(&selected),
	))
	if err := form.Run(); err != nil {
		return nil, output.Wrap(output.CodeInternal, err, "cancelled")
	}
	if len(selected) == 0 {
		return nil, output.Errorf(output.CodeBranchUnknown, "no branches selected")
	}
	return selected, nil
}

func aliasFromURL(url string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(url), "/")
	trimmed = strings.TrimSuffix(trimmed, ".git")
	if idx := strings.LastIndexAny(trimmed, "/:"); idx >= 0 {
		trimmed = trimmed[idx+1:]
	}
	return trimmed
}

// branchesOrEmpty keeps an absent list as [] rather than null: a consumer branches on a
// detail being present, and null is a value it would have to special-case.
func branchesOrEmpty(branches []string) []string {
	if branches == nil {
		return []string{}
	}
	return branches
}

// branchesSource says where a branch list came from, so "not queried" is distinguishable
// from "queried and empty".
func branchesSource(opts *CloneOptions) string {
	switch {
	case len(opts.Branches) > 0:
		return "flag"
	case opts.AllBranches:
		// --all resolves against the remote, which a dry run does not contact.
		return "not-queried"
	default:
		return "default-branch"
	}
}

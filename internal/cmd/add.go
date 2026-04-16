package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add [<repo-alias> <branch-name>]",
	Short: "Add a new worktree",
	Long: `Create a new worktree for a repository branch.

DESCRIPTION
  Creates a Git worktree - a separate working directory for a specific branch.
  Worktrees allow you to work on multiple branches simultaneously without
  stashing or committing incomplete work.

  When you run this command:
  1. Creates worktree directory: .bare/<repo>/<branch>/
  2. Creates symlink: <ecosystem>/<repo>-<branch>
  3. Checks out the specified branch (creating it if needed)

WHEN TO USE
  • Starting work on a new feature
  • Creating hotfix branches from production
  • Setting up staging or production worktrees
  • Working on multiple features in parallel

EXAMPLES
  # Interactive mode - prompts for repo and branch
  $ hydra add

  # Create worktree for a branch
  $ hydra add api feature-x

  # Create branch from specific base branch
  $ hydra add api feature-y --from=develop

FLAGS
  -f, --from string    Create branch from this branch
  -h, --help           Show help

EXIT CODES
  0  Success (worktree created or already exists)
  1  General error (invalid args, repo not found)
  2  Config file (.hydra.yaml) not found

SEE ALSO
  • hydra remove - Remove a worktree
  • hydra switch - Switch to a worktree
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/commands/worktree-management.md`,
	RunE: runAdd,
}

var (
	addFromBranch string
)

type addSelection struct {
	Alias  string
	Branch string
	From   string
}

func init() {
	rootCmd.AddCommand(addCmd)
	addCmd.Flags().StringVarP(&addFromBranch, "from", "f", "", "Create branch from this branch")
	addCmd.ValidArgsFunction = completeRepoAliases
}

func runAdd(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)
	currentCtx, _ := resolveCurrentHydraContext(wd, cfg, projectRoot)

	var selection addSelection
	if len(args) == 0 {
		selection, err = interactiveAdd(cfg, projectRoot, currentCtx)
		if err != nil {
			return err
		}
	} else if len(args) >= 2 {
		selection = addSelection{Alias: args[0], Branch: args[1]}
	} else {
		return fmt.Errorf("usage: hydra add <repo-alias> <branch-name>")
	}

	repo, err := resolveRepoByAlias(cfg, projectRoot, selection.Alias)
	if err != nil {
		return err
	}
	if _, err := os.Stat(repo.BareRepo); os.IsNotExist(err) {
		return fmt.Errorf("bare repository not found: %s", repo.BareRepo)
	}

	wt := buildWorktreeContext(repo, projectRoot, selection.Branch)
	if _, err := os.Stat(wt.WorktreePath); err == nil {
		_ = ensureSymlink(wt)
		printAddSummary(wd, wt, selection.Branch, "", true)
		return nil
	}

	fmt.Printf("Creating worktree for %s:%s...\n", repo.RepoName, selection.Branch)

	branchExists, err := git.BranchExists(repo.BareRepo, selection.Branch)
	if err != nil {
		return fmt.Errorf("failed to resolve branch: %w", err)
	}

	fromBranch := selection.From
	if branchExists {
		err = git.CreateWorktreeForBranch(repo.BareRepo, wt.WorktreePath, selection.Branch)
	} else {
		fromBranch, err = resolveAddBaseBranch(repo, currentCtx, addFromBranch, selection.From)
		if err != nil {
			err = git.CreateWorktreeNewBranch(repo.BareRepo, wt.WorktreePath, selection.Branch)
			fromBranch = ""
		} else {
			err = git.CreateWorktreeFromBase(repo.BareRepo, wt.WorktreePath, selection.Branch, fromBranch)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	if err := ensureSymlink(wt); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	printAddSummary(wd, wt, selection.Branch, fromBranch, false)
	return nil
}

func interactiveAdd(cfg *config.Config, projectRoot string, currentCtx *currentHydraContext) (addSelection, error) {
	selectedAlias, err := promptForRepo(cfg, currentCtx)
	if err != nil {
		return addSelection{}, err
	}

	var mode string
	modeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Branch Mode").
				Description("Choose an existing branch or create a new one").
				Options(
					huh.NewOption("Existing branch", "existing"),
					huh.NewOption("New branch", "new"),
				).
				Value(&mode),
		),
	)
	if err := modeForm.Run(); err != nil {
		return addSelection{}, err
	}

	repo, err := resolveRepoByAlias(cfg, projectRoot, selectedAlias)
	if err != nil {
		return addSelection{}, err
	}

	branches, defaultBranch, err := branchChoicesForRepo(repo, projectRoot)
	if err != nil {
		return addSelection{}, err
	}

	if mode == "existing" {
		branchName, err := promptForExistingBranch(branches)
		if err != nil {
			return addSelection{}, err
		}
		return addSelection{Alias: selectedAlias, Branch: branchName}, nil
	}

	fromBranch, err := resolveAddBaseBranch(repo, currentCtx, addFromBranch, "")
	if err != nil && defaultBranch != "" {
		fromBranch = defaultBranch
		err = nil
	}
	if err != nil {
		return addSelection{}, err
	}

	branchName, selectedFrom, err := promptForNewBranch(branches, fromBranch)
	if err != nil {
		return addSelection{}, err
	}

	return addSelection{Alias: selectedAlias, Branch: branchName, From: selectedFrom}, nil
}

func promptForRepo(cfg *config.Config, currentCtx *currentHydraContext) (string, error) {
	var repoOptions []huh.Option[string]
	selectedAlias := ""
	if currentCtx != nil {
		selectedAlias = currentCtx.RepoContext.Alias
	}

	for ecoName, eco := range cfg.Ecosystems {
		for alias := range eco {
			repoOptions = append(repoOptions, huh.NewOption(fmt.Sprintf("%s (%s)", alias, ecoName), alias))
		}
	}
	if len(repoOptions) == 0 {
		return "", fmt.Errorf("no repositories found in config")
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Repository").
				Description("Choose which repository to add a worktree for").
				Options(repoOptions...).
				Value(&selectedAlias),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return selectedAlias, nil
}

func promptForExistingBranch(branches []branchChoice) (string, error) {
	options := make([]huh.Option[string], 0, len(branches))
	for _, branch := range branches {
		options = append(options, huh.NewOption(branch.DisplayName, branch.Name))
	}

	var branchName string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Branch").
				Description("Choose an existing branch to use for the worktree").
				Options(options...).
				Value(&branchName),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return branchName, nil
}

func promptForNewBranch(branches []branchChoice, initialFrom string) (string, string, error) {
	branchName := ""
	fromBranch := initialFrom

	updatedBranch, err := promptForNewBranchName(branchName, branches)
	if err != nil {
		return "", "", err
	}
	branchName = updatedBranch

	for {
		action, err := promptForNewBranchAction(branchName, fromBranch)
		if err != nil {
			return "", "", err
		}

		switch action {
		case "branch":
			updatedBranch, err := promptForNewBranchName(branchName, branches)
			if err != nil {
				return "", "", err
			}
			branchName = updatedBranch
		case "from":
			updatedFrom, err := promptForBaseBranchSelection(fromBranch, branches)
			if err != nil {
				continue
			}
			fromBranch = updatedFrom
		case "create":
			if err := validateNewBranchName(branchName, branches); err != nil {
				fmt.Println(styles.Error.Render(err.Error()))
				continue
			}
			return branchName, fromBranch, nil
		}
	}
}

func promptForNewBranchAction(branchName, fromBranch string) (string, error) {
	var action string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("New Branch").
				Description("Select a row to edit. Choosing 'From' opens the branch picker and cancel keeps the current selection.").
				Options(
					huh.NewOption("Branch Name: "+branchName, "branch"),
					huh.NewOption("From: "+fromBranch, "from"),
					huh.NewOption("Create worktree", "create"),
				).
				Value(&action),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return action, nil
}

func promptForNewBranchName(current string, branches []branchChoice) (string, error) {
	branchName := current
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Branch Name").
				Description("Enter the new branch name for the worktree").
				Placeholder("feature/my-feature").
				Value(&branchName).
				Validate(func(s string) error {
					return validateNewBranchName(s, branches)
				}),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return branchName, nil
}

func promptForBaseBranchSelection(current string, branches []branchChoice) (string, error) {
	fromBranch := current
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("From").
				Description("Choose the base branch for the new branch.").
				Options(baseBranchOptions(branches, current)...).
				Value(&fromBranch),
		),
	)
	if err := form.Run(); err != nil {
		return "", err
	}
	return fromBranch, nil
}

func validateNewBranchName(branchName string, branches []branchChoice) error {
	if branchName == "" {
		return fmt.Errorf("branch name cannot be empty")
	}
	for _, branch := range branches {
		if branch.Name == branchName {
			return fmt.Errorf("branch already exists, use Existing branch mode")
		}
	}
	return nil
}

func baseBranchOptions(branches []branchChoice, current string) []huh.Option[string] {
	options := make([]huh.Option[string], 0, len(branches))
	seen := make(map[string]struct{}, len(branches))

	appendOption := func(branch branchChoice) {
		if _, ok := seen[branch.Name]; ok {
			return
		}
		seen[branch.Name] = struct{}{}
		label := branch.DisplayName
		if branch.Name == current {
			label = branch.Name + " (current selection)"
		}
		options = append(options, huh.NewOption(label, branch.Name))
	}

	for _, branch := range branches {
		if branch.Name == current {
			appendOption(branch)
		}
	}
	for _, branch := range branches {
		appendOption(branch)
	}

	return options
}

func printAddSummary(wd string, wt worktreeContext, branch, fromBranch string, alreadyExists bool) {
	cdHint, switchHint := navigationHints(wd, wt)
	if alreadyExists {
		fmt.Println(styles.Success.Render("✓ Worktree already exists"))
	} else {
		fmt.Println(styles.Success.Render("✓ Worktree created"))
	}
	fmt.Printf("  Path: %s\n", wt.WorktreePath)
	fmt.Printf("  Branch: %s\n", branch)
	if !alreadyExists && fromBranch != "" {
		fmt.Printf("  From: %s\n", fromBranch)
	}
	fmt.Printf("  Symlink: %s\n", filepath.Join(wt.RepoContext.Ecosystem, wt.SymlinkName))
	fmt.Println()
	fmt.Println(cdHint)
	fmt.Println(switchHint)
}

func resolveAddBaseBranch(repo repoContext, currentCtx *currentHydraContext, explicitFrom, selectedFrom string) (string, error) {
	if explicitFrom != "" {
		return explicitFrom, nil
	}
	if selectedFrom != "" {
		return selectedFrom, nil
	}
	if currentCtx != nil && currentCtx.RepoContext.Alias == repo.Alias && currentCtx.Branch != "" && currentCtx.Branch != "HEAD" && currentCtx.Branch != "detached" {
		return currentCtx.Branch, nil
	}
	locals, localErr := git.ListLocalBranches(repo.BareRepo)
	if localErr == nil && len(locals) > 0 {
		for _, branch := range []string{"main", "master"} {
			for _, local := range locals {
				if local == branch {
					return local, nil
				}
			}
		}
		return locals[0], nil
	}
	defaultBranch, err := git.GetRemoteDefaultBranch(repo.BareRepo)
	if err == nil && defaultBranch != "" {
		return defaultBranch, nil
	}
	branches, branchErr := git.GetRemoteBranchesFromBare(repo.BareRepo)
	if branchErr == nil && len(branches) > 0 {
		return git.GetDefaultBranch(branches), nil
	}
	if err != nil {
		return "", err
	}
	if branchErr != nil {
		return "", branchErr
	}
	if localErr != nil {
		return "", localErr
	}
	if git.RefExists(repo.BareRepo, "HEAD") {
		return "HEAD", nil
	}
	return "", fmt.Errorf("could not determine base branch for %s", repo.Alias)
}

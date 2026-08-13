package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var repoSetBranches []string

var repoSetCmd = &cobra.Command{
	Use:   "set <alias>",
	Short: "Change a registered repository's declared branches",
	Long: `Change the branches a repository declares, after it was registered.

"repo add --branches" declares the shape at registration. This changes it later, which
otherwise meant hand-editing .hydra/config.yaml — a file hydra also writes, so the two
were competing over the same document.

The branch list is validated against origin, so a name that does not exist is refused
rather than written and discovered by whoever restores the workspace next. On a terminal
with no --branches, the same multi-select "repo add" uses opens with the current
declaration pre-selected.

Declaring FEWER branches never deletes a worktree. The declaration is what a restore
builds; removing a worktree stays "hydra remove", which asks about unmerged work.`,
	Example: `  # Declare the long-lived branches this repo keeps checked out
  $ hydra repo set api --branches main,stage,prod

  # Pick them from the remote's real branches
  $ hydra repo set api`,
	Args:              cobra.ExactArgs(1),
	RunE:              runRepoSet,
	ValidArgsFunction: completeRepoAliases,
}

func init() {
	repoSetCmd.Flags().StringSliceVar(&repoSetBranches, "branches", nil,
		"branches this repository keeps worktrees for (comma-separated)")
	repoCmd.AddCommand(repoSetCmd)
}

type repoSetJSON struct {
	Group string `json:"group"`
	Alias string `json:"alias"`
	// Before and Branches are both reported because a declaration change is worth a diff:
	// the caller asked for a set, not a delta, and seeing what it replaced is how a review
	// catches a narrowing nobody meant.
	Before   []string `json:"before"`
	Branches []string `json:"branches"`
	// Undeclared names worktrees that exist but are no longer declared. They are NOT
	// removed — a declaration is what a restore builds, not an instruction to delete work.
	Undeclared []string `json:"undeclared"`
}

// unknownDeclaredRepo mirrors resolve.go's repo_unknown shape so a caller can self-correct
// from details.known rather than guessing at an alias.
func unknownDeclaredRepo(alias string) *output.Error {
	var names []string
	for _, ref := range cfg.Repos() {
		names = append(names, ref.Alias)
	}
	sort.Strings(names)
	return output.Errorf(output.CodeRepoUnknown,
		"repository %q is not registered; run \"hydra repo list\" to see registered repositories", alias).
		WithDetail("repo", alias).
		WithDetail("known", names)
}

func runRepoSet(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	alias := strings.TrimSpace(args[0])

	ref, ok := cfg.FindRepo(alias)
	if !ok {
		return unknownDeclaredRepo(alias)
	}

	repo := repoContext{
		Group:         ref.Group,
		Alias:         ref.Alias,
		Remote:        ref.Repo.Remote,
		DefaultBranch: ref.Repo.DefaultBranch,
		BareRepo:      cfg.BarePath(projectRoot, ref.Alias),
	}

	// Non-interactive with no --branches must not fall through to resolveCloneBranches:
	// there, "nothing selected" means the default branch, which is right for `add` and
	// wrong here — it would silently narrow an existing declaration to one entry. Name what
	// is missing and what the valid values are, per the needs_input contract.
	if len(repoSetBranches) == 0 && !interactive() {
		err := output.Errorf(output.CodeNeedsInput,
			"--branches is required when there is no terminal to pick from").
			WithDetail("missing", []string{"--branches"})
		if available, listErr := git.ListRemoteBranchesCached(repo.BareRepo); listErr == nil {
			names := make([]string, 0, len(available))
			for _, branch := range available {
				names = append(names, branch.Name)
			}
			err = err.WithDetail("one_of", names)
		}
		return err
	}

	// Reuse the resolution `repo add` uses: it lists origin's branches from the bare repo,
	// validates every requested name against them, dedupes, and opens the same multi-select
	// when nothing was named. A second implementation here would be a second set of rules
	// about what a valid branch is.
	opts := &CloneOptions{
		URL:         ref.Repo.Remote,
		Alias:       ref.Alias,
		Group:       ref.Group,
		Branches:    repoSetBranches,
		Interactive: interactive(),
		// The form must open showing what it is about to replace, not the default branch.
		Preselect: ref.Repo.Branches,
	}
	branches, err := resolveCloneBranches(opts, repo, ref.Repo.DefaultBranch)
	if err != nil {
		return err
	}

	before := ref.Repo.Branches
	if err := config.Update(projectRoot, func(live *config.Config) error {
		found, ok := live.FindRepo(alias)
		if !ok {
			return unknownDeclaredRepo(alias)
		}
		found.Repo.Branches = branches
		live.SetRepo(found.Group, found.Alias, found.Repo)
		return nil
	}); err != nil {
		return classifyManifestErr(err)
	}

	payload := repoSetJSON{
		Group:      ref.Group,
		Alias:      ref.Alias,
		Before:     before,
		Branches:   branches,
		Undeclared: undeclaredWorktrees(repo, branches),
	}

	var warnings []*output.Diagnostic
	if len(payload.Undeclared) > 0 {
		warnings = append(warnings, output.Notef("",
			"%s: %d worktree(s) exist outside the declared branches and were left alone: %s",
			ref.Alias, len(payload.Undeclared), strings.Join(payload.Undeclared, ", ")).
			WithSubject("repo", ref.Alias))
	}

	return emitResult(cmd, output.Result{
		Summary:  fmt.Sprintf("%s/%s declares %d branch(es)", ref.Group, ref.Alias, len(branches)),
		Data:     payload,
		Warnings: warnings,
	}, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Declared branches updated"))
		fmt.Printf("  Repo:     %s/%s\n", ref.Group, ref.Alias)
		fmt.Printf("  Declared: %s\n", strings.Join(branches, ", "))
		if len(payload.Undeclared) > 0 {
			fmt.Printf("  Present but not declared: %s\n", strings.Join(payload.Undeclared, ", "))
			fmt.Println("  (left alone — use \"hydra remove\" to delete a worktree)")
		}
		fmt.Println()
		fmt.Println(styles.Label.Render("  hydra repo restore   creates any declared branch that is missing"))
	})
}

// undeclaredWorktrees lists branches with a worktree on disk that the new declaration does
// not name. Reported so a narrowing is visible at the moment it happens rather than being
// discovered later as drift, and never acted on: this command changes a declaration, and
// deleting work is a different decision with a different command.
func undeclaredWorktrees(repo repoContext, declared []string) []string {
	worktrees, err := listRepoWorktrees(repo)
	if err != nil {
		return nil
	}
	keep := make(map[string]struct{}, len(declared))
	for _, branch := range declared {
		keep[branch] = struct{}{}
	}
	var out []string
	for _, wt := range worktrees {
		if wt.Detached || wt.Branch == "" {
			continue
		}
		if _, ok := keep[wt.Branch]; !ok {
			out = append(out, wt.Branch)
		}
	}
	return out
}

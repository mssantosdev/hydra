package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var pruneDryRun bool

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Prune stale worktree registrations and empty group directories",
	Long: `Remove stale git worktree registrations and tidy group folders.

DESCRIPTION
  For every registered repository in the workspace, runs git worktree prune on
  the bare repository, then removes group directories that became empty, and
  finally prunes dangling entries from the global project registry.

  Use --dry-run to print the plan without making changes.

WHEN TO USE
  • After manually deleting worktree directories
  • Following doctor reports of missing worktrees
  • Periodic workspace cleanup

EXAMPLES
  hydra prune
  hydra prune --dry-run
  hydra prune --output json

FLAGS
  --dry-run   Show what would be pruned without acting

EXIT CODES
  0  Success
  1  git_failed
  2  not_in_project

SEE ALSO
  hydra doctor - Diagnose issues before pruning
  hydra repo add <path> --adopt - track an existing checkout`,
	RunE: runPrune,
}

type pruneJSON struct {
	Project         string   `json:"project"`
	Root            string   `json:"root"`
	PrunedWorktrees []string `json:"pruned_worktrees"`
	RemovedGroups   []string `json:"removed_groups"`
	PrunedProjects  []string `json:"pruned_projects"`
	DryRun          bool     `json:"dry_run"`
}

func init() {
	rootCmd.AddCommand(pruneCmd)
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "Show the plan without making changes")
}

func runPrune(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject,
			`no hydra workspace found; run "hydra init" or pass --project <name>`)
	}

	result := pruneJSON{
		Project: cfg.Project,
		Root:    projectRoot,
		DryRun:  pruneDryRun,
	}

	for _, repo := range allRepoContexts(cfg, projectRoot) {
		before, _ := git.ListWorktrees(repo.BareRepo)
		prunable := make([]string, 0)
		for _, wt := range before {
			if wt.Prunable {
				prunable = append(prunable, wt.Path)
			}
		}
		if len(prunable) == 0 {
			continue
		}
		if pruneDryRun {
			result.PrunedWorktrees = append(result.PrunedWorktrees, prunable...)
			continue
		}
		if err := git.PruneWorktrees(repo.BareRepo); err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to prune worktrees for %s", repo.Alias)
		}
		result.PrunedWorktrees = append(result.PrunedWorktrees, prunable...)
	}

	for _, group := range cfg.SortedGroups() {
		groupDir := filepath.Join(projectRoot, group)
		entries, err := os.ReadDir(groupDir)
		if err != nil {
			continue
		}
		if len(entries) > 0 {
			continue
		}
		result.RemovedGroups = append(result.RemovedGroups, group)
		if pruneDryRun {
			continue
		}
		if err := os.Remove(groupDir); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to remove empty group %s", group)
		}
	}

	if pruneDryRun {
		reg, err := registry.Load()
		if err == nil {
			for name, root := range reg.Projects {
				if _, err := os.Stat(config.ManifestPath(root)); err != nil {
					result.PrunedProjects = append(result.PrunedProjects, name)
				}
			}
		}
	} else {
		reg, err := registry.Load()
		if err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to load project registry")
		}
		result.PrunedProjects = reg.Prune()
		if err := reg.Save(); err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to save project registry")
		}
	}

	return emit(cmd, prunePayloadSummary(result), result, nil, func() { printPruneText(result) })
}

// prunePayloadSummary distinguishes a dry run from a real one, because "3 removed"
// and "3 would be removed" are not the same answer.
func prunePayloadSummary(result pruneJSON) string {
	verb := "removed"
	if pruneDryRun {
		verb = "would remove"
	}
	return fmt.Sprintf("%s %d worktree registration(s), %d group dir(s), %d project(s)",
		verb, len(result.PrunedWorktrees), len(result.RemovedGroups), len(result.PrunedProjects))
}

func printPruneText(result pruneJSON) {
	fmt.Println()
	fmt.Println(styles.Title.Render("Prune Results"))
	if result.DryRun {
		fmt.Println(styles.Dimmed.Render("(dry run — no changes made)"))
	}
	if len(result.PrunedWorktrees) > 0 {
		fmt.Printf("%s Pruned worktrees:\n", styles.Success.Render("✓"))
		for _, path := range result.PrunedWorktrees {
			fmt.Printf("  • %s\n", path)
		}
	}
	if len(result.RemovedGroups) > 0 {
		fmt.Printf("%s Removed empty groups:\n", styles.Success.Render("✓"))
		for _, group := range result.RemovedGroups {
			fmt.Printf("  • %s\n", group)
		}
	}
	if len(result.PrunedProjects) > 0 {
		fmt.Printf("%s Pruned registry projects:\n", styles.Success.Render("✓"))
		for _, name := range result.PrunedProjects {
			fmt.Printf("  • %s\n", name)
		}
	}
	if len(result.PrunedWorktrees) == 0 && len(result.RemovedGroups) == 0 && len(result.PrunedProjects) == 0 {
		fmt.Println(styles.Dimmed.Render("Nothing to prune."))
	}
	fmt.Println()
}

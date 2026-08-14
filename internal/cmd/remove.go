package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var (
	removeForce        bool
	removeYes          bool
	removeDeleteBranch bool
)

type removeResult struct {
	Group         string `json:"group"`
	Repo          string `json:"repo"`
	Branch        string `json:"branch"`
	Path          string `json:"path"`
	BranchDeleted bool   `json:"branch_deleted"`
	// Topic is the topic this worktree was detached from, nil when unassigned.
	Topic *string `json:"topic"`
}

var removeCmd = &cobra.Command{
	Use:     "remove [<alias> <branch>] | [<worktree>]",
	Aliases: []string{"rm"},
	Short:   "Delete a worktree",
	Long: `Remove a worktree, and optionally its branch.

DESCRIPTION
  The worktree is located through "git worktree list", never by rebuilding a path
  from the branch name, so a directory renamed with --as is still found.

  Uncommitted changes block removal (exit 5) unless --force is passed.

  --delete-branch deletes the branch from the bare repository after the worktree is
  gone. Without --force this uses "git branch -d", so an unmerged branch is refused
  and reported as git_failed rather than silently discarded. hydra never claims to
  have deleted a branch it did not delete.

WHEN TO USE
  • The work on a branch is merged or abandoned
  • Reclaiming disk space from a worktree you no longer need

EXAMPLES
  # By repo and branch
  $ hydra remove api stage

  # By worktree directory name
  $ hydra remove api-stage

  # Remove and delete the branch, no prompts
  $ hydra remove api stage --yes --delete-branch

  # Discard uncommitted changes too
  $ hydra remove api stage --force --yes

FLAGS
  --force           remove even with uncommitted changes; force branch deletion
  --yes             skip the confirmation prompt
  --delete-branch   delete the branch after removing the worktree

HOOKS
  Runs pre_remove with cwd set to the worktree, then post_remove with cwd set to
  the project root (the worktree no longer exists by then).

EXIT CODES
  0  Success
  1  repo_unknown, bare_missing, worktree_unknown, git_failed, hook_failed
  2  not_in_project, config_version_unsupported, project_unknown
  5  worktree_dirty (uncommitted changes and no --force)

SEE ALSO
  • hydra add   - create a worktree
  • hydra prune - drop stale registrations and empty groups
  • hydra list  - list worktrees`,
	Args: cobra.MaximumNArgs(2),
	RunE: runRemove,
}

func init() {
	removeCmd.Flags().BoolVar(&removeForce, "force", false, "remove even with uncommitted changes")
	removeCmd.Flags().BoolVar(&removeYes, "yes", false, "skip the confirmation prompt")
	removeCmd.Flags().BoolVar(&removeDeleteBranch, "delete-branch", false, "delete the branch after removing the worktree")
	rootCmd.AddCommand(removeCmd)
}

func runRemove(cmd *cobra.Command, args []string) error {
	wt, err := resolveRemoveTarget(args)
	if err != nil {
		return err
	}
	repo := wt.RepoContext

	hasChanges, changeCount, err := git.HasUncommittedChanges(wt.Path)
	if err != nil {
		return output.Wrap(output.CodeGitFailed, err, "failed to inspect %s", wt.Path)
	}
	if hasChanges && !removeForce {
		return output.Errorf(output.CodeWorktreeDirty,
			"%s has %d uncommitted change(s); commit, stash, or pass --force", wt.Path, changeCount).
			WithDetail("path", wt.Path).
			WithDetail("changes", changeCount).
			WithDetail("branch", wt.Branch)
	}

	// Verify branch deletion is possible BEFORE removing the worktree. Removing the
	// worktree first and only then discovering the branch is unmerged left an
	// orphaned branch with no worktree — and no way to reach it through hydra, since
	// remove resolves its target through the worktree list.
	//
	// mergeVerified records that hydra positively confirmed the branch is merged, in
	// which case it deletes forcibly: `git branch -d` in a BARE repo judges only
	// against HEAD and the upstream, so it refuses branches that are demonstrably
	// merged into origin. hydra can see the remote-tracking refs, so it decides.
	mergeVerified := false
	if removeDeleteBranch && !removeForce && wt.Branch != "" {
		verified, err := ensureBranchDeletable(repo, wt.Branch)
		if err != nil {
			return err
		}
		mergeVerified = verified
	}

	if !removeYes && interactive() {
		confirm := false
		title := fmt.Sprintf("Remove %s (%s)?", wt.Qualified(), wt.BranchLabel())
		if err := huh.NewConfirm().Title(title).Value(&confirm).Run(); err != nil {
			return output.Wrap(output.CodeCancelled, err, "cancelled")
		}
		if !confirm {
			return output.Errorf(output.CodeCancelled, "cancelled")
		}
	}

	var warnings []*output.Diagnostic
	preResult, preErr := runHookEvent("pre_remove", hooksContextFor(repo, wt.Branch, wt.Path), wt.Path)
	warnings = append(warnings, preResult.Warnings...)
	if preErr != nil {
		return preErr
	}

	if err := git.RemoveWorktree(repo.BareRepo, wt.Path, removeForce); err != nil {
		return output.Wrap(output.CodeGitFailed, err, "failed to remove worktree %s", wt.Path)
	}

	result := removeResult{
		Group:  repo.Group,
		Repo:   repo.Alias,
		Branch: wt.Branch,
		Path:   wt.Path,
	}

	// Detach AFTER the worktree is gone, never before.
	//
	// Detach-first would, on an interrupted run, leave a live worktree that looks
	// unassigned — indistinguishable from genuinely ad-hoc work, so nothing reports
	// it. Detach-after leaves a member whose worktree is missing, which `doctor`
	// names as topic_dangling_member and fixes. A loud, findable inconsistency
	// beats a silent, invisible one.
	//
	// Failure to detach is a warning, not an error: the removal already succeeded,
	// and returning an error here would report a failure for work that was done.
	if wt.Branch != "" {
		if id, ok, err := topicStore().TopicOf(repo.Alias, wt.Branch); err != nil {
			warnings = append(warnings, output.Warnf(output.CodeTopicConflict, "topic membership not updated: %v", classifyTopicErr(err)).
				WithCause(err.Error()))
		} else if ok {
			result.Topic = &id
			if err := topicStore().Detach(id, repo.Alias, wt.Branch); err != nil {
				warnings = append(warnings, output.Warnf(output.CodeTopicConflict,
					"worktree removed but it is still recorded in topic %q; run \"hydra doctor --fix\": %v",
					id, classifyTopicErr(err)).
					WithSubject("topic", id).
					WithCause(err.Error()))
			}
		}
	}

	if removeDeleteBranch {
		if wt.Branch == "" {
			warnings = append(warnings, output.Notef("", "worktree was detached; there is no branch to delete"))
		} else if err := git.DeleteBranch(repo.BareRepo, wt.Branch, removeForce || mergeVerified); err != nil {
			// Report the real failure. Never print success for work not done.
			return output.Wrap(output.CodeGitFailed, err,
				"worktree removed, but branch %q was not deleted (pass --force to delete an unmerged branch)", wt.Branch).
				WithDetail("branch", wt.Branch).
				WithDetail("worktree_removed", true)
		} else {
			result.BranchDeleted = true
		}
	}

	if removed, err := removeGroupDirIfEmpty(projectRoot, repo.Group); err != nil {
		warnings = append(warnings, output.Classify(err))
	} else if removed {
		warnings = append(warnings, output.Notef("", "removed empty group directory %s", repo.Group))
	}

	postResult, postErr := runHookEvent("post_remove", hooksContextFor(repo, wt.Branch, wt.Path), projectRoot)
	warnings = append(warnings, postResult.Warnings...)

	emitErr := emit(cmd, removeSummaryLine(result), result, warnings, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Worktree removed"))
		fmt.Printf("  Path:   %s\n", result.Path)
		fmt.Printf("  Branch: %s\n", wt.BranchLabel())
		if removeDeleteBranch {
			if result.BranchDeleted {
				fmt.Printf("  Branch %s deleted\n", result.Branch)
			} else {
				fmt.Println("  Branch not deleted")
			}
		}
	})
	if emitErr != nil {
		return emitErr
	}
	return postErr
}

// removeSummaryLine names what actually happened, including the topic detach, so a
// caller does not have to diff state to find out.
func removeSummaryLine(result removeResult) string {
	summary := fmt.Sprintf("removed %s/%s", result.Group, result.Repo)
	if result.Branch != "" {
		summary += " on " + result.Branch
	}
	if result.BranchDeleted {
		summary += ", branch deleted"
	}
	if result.Topic != nil {
		summary += fmt.Sprintf(", detached from topic %q", *result.Topic)
	}
	return summary
}

// resolveRemoveTarget accepts "<alias> <branch>", a worktree handle, or prompts.
func resolveRemoveTarget(args []string) (worktreeContext, error) {
	switch len(args) {
	case 2:
		alias, branch := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		repo, err := resolveRepoByAlias(cfg, projectRoot, alias)
		if err != nil {
			return worktreeContext{}, err
		}
		wt, ok := findRepoWorktreeByBranch(repo, branch)
		if !ok {
			return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
				"no worktree for branch %q in %q", branch, alias).
				WithDetail("branch", branch).
				WithDetail("repo", alias).
				WithDetail("did_you_mean", findSimilarWorktreesByName(cfg, projectRoot, branch))
		}
		return wt, nil

	case 1:
		name := strings.TrimSpace(args[0])
		items, _ := collectWorktrees(cfg, projectRoot)
		wt, err := resolveOneWorktree(items, name)
		if err == nil {
			return wt, nil
		}
		// Ambiguity matters most here: removing the wrong worktree destroys work.
		// The two-argument form `remove <alias> <branch>` is the unambiguous escape.
		if output.Classify(err).Code == output.CodeWorktreeUnknown {
			return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
				"no worktree named %q", name).
				WithDetail("name", name).
				WithDetail("did_you_mean", findSimilarWorktreesByName(cfg, projectRoot, name))
		}
		return worktreeContext{}, err
	}

	if !interactive() {
		// needs_input, not worktree_unknown: no worktree was named and a prompt cannot
		// be shown, so the caller is told exactly which argument is missing.
		return worktreeContext{}, output.Errorf(output.CodeNeedsInput,
			"a worktree is required: hydra remove <alias> <branch>, or hydra remove <worktree>").
			WithDetail("missing", []string{"<worktree>"}).
			WithDetail("available", worktreeHandles(mustCollectWorktrees()))
	}

	items, _ := collectWorktrees(cfg, projectRoot)
	if len(items) == 0 {
		return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown, "this project has no worktrees")
	}
	options := make([]huh.Option[string], 0, len(items))
	for _, item := range items {
		options = append(options, huh.NewOption(
			fmt.Sprintf("%s (%s)", item.Qualified(), item.BranchLabel()), item.Qualified()))
	}
	selected := items[0].Qualified()
	if err := huh.NewSelect[string]().Title("Worktree to remove").Options(options...).Value(&selected).Run(); err != nil {
		return worktreeContext{}, output.Wrap(output.CodeCancelled, err, "cancelled")
	}
	return resolveOneWorktree(items, selected)
}

// removeGroupDirIfEmpty drops a group directory that has no entries left.
func removeGroupDirIfEmpty(root, group string) (bool, error) {
	dir := groupDir(root, group)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to inspect group directory %s: %w", group, err)
	}
	if len(entries) > 0 {
		return false, nil
	}
	if err := os.Remove(dir); err != nil {
		return false, fmt.Errorf("failed to remove empty group directory %s: %w", group, err)
	}
	return true, nil
}

// ensureBranchDeletable decides whether a branch's work is already integrated, and
// reports whether that was positively verified.
//
// The safety target is the repo's DEFAULT branch — never origin/<same-branch>.
// Comparing a branch against its own remote tip only proves it was pushed, not that
// the work landed; treating that as "safe to delete" would hard-delete live feature
// branches. The question is "is this work in the mainline", and only the default
// branch answers it.
//
// Checking before the worktree is removed is what keeps `remove --delete-branch`
// atomic: either both happen, or neither does.
func ensureBranchDeletable(repo repoContext, branch string) (bool, error) {
	if repo.DefaultBranch != "" && branch == repo.DefaultBranch {
		return false, output.Errorf(output.CodeGitFailed,
			"refusing to delete %q, the default branch of %q; nothing was removed", branch, repo.Alias).
			WithDetail("branch", branch).
			WithDetail("worktree_removed", false)
	}

	into := ""
	if repo.DefaultBranch != "" {
		for _, candidate := range []string{
			"refs/remotes/origin/" + repo.DefaultBranch,
			"refs/heads/" + repo.DefaultBranch,
		} {
			if git.RefExists(repo.BareRepo, candidate) {
				into = candidate
				break
			}
		}
	}
	if into == "" {
		// No mainline to judge against. Do not claim verification: fall through to
		// git's own conservative `-d`, which refuses anything it cannot prove safe.
		return false, nil
	}
	if git.IsBranchMerged(repo.BareRepo, branch, into) {
		return true, nil
	}

	return false, output.Errorf(output.CodeGitFailed,
		"branch %q is not merged into %s; nothing was removed. Re-run with --force to delete both the worktree and the branch, or drop --delete-branch to keep the branch",
		branch, strings.TrimPrefix(strings.TrimPrefix(into, "refs/remotes/"), "refs/heads/")).
		WithDetail("branch", branch).
		WithDetail("merge_target", into).
		WithDetail("worktree_removed", false)
}

package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	topicWithWorktrees bool
	topicForce         bool
	topicYes           bool
	topicDryRun        bool
	topicDeleteBranch  bool
)

var topicCmd = &cobra.Command{
	Use:   "topic",
	Short: "Inspect and manage topics (units of work spanning repositories)",
	Long: `Manage topics: units of work that span repositories.

DESCRIPTION
  A topic groups the worktrees that belong to one piece of work, across any number of
  repositories. Membership is EXPLICIT: it is recorded when you create or attach a
  worktree, and never inferred from branch names. Two repos may use differently
  named branches for the same topic, and two topics may share a branch name.

  A worktree belongs to at most one topic. Not belonging to one is a permanent,
  first-class state, so an unassigned worktree keeps working exactly as before.

  There is no "topic create": a topic exists because work was put in it, so identity
  and work cannot drift apart. "hydra start" and "hydra topic attach" create one;
  every other command requires it to exist already. A topic disappears when its last
  member is detached — that garbage collection is the only path that removes one.

SUBCOMMANDS
  list    List active topics with their member counts
  show    Show one topic's members, joined to the worktrees on disk
  attach  Record that a worktree belongs to a topic
  detach  Drop a worktree's membership (the worktree itself is untouched)
  remove  Detach every member, optionally removing their worktrees too

EXAMPLES
  $ hydra topic list
  $ hydra topic show 2072958
  $ hydra topic attach 2072958 backend/api-login
  $ hydra topic detach 2072958 backend/api-login
  $ hydra topic remove 2072958 --with-worktrees --yes

EXIT CODES
  0  Success
  1  topic_unknown (details.known lists valid ids), topic_conflict, git_failed
  2  not_in_project, state_version_unsupported
  4  partial_failure (some worktrees failed to be removed)
  5  worktree_dirty (a target has uncommitted changes; pass --force)
  6  busy (state or git lock held; retry)

SEE ALSO
  • hydra list --topic <id>   - the worktrees in a topic
  • hydra status --topic <id> - their git state`,
}

var topicListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List active topics",
	Args:    cobra.NoArgs,
	RunE:    runTopicList,
}

var topicShowCmd = &cobra.Command{
	Use:     "show <id>",
	Aliases: []string{"view"},
	Short:   "Show one topic and its members",
	Args:    cobra.ExactArgs(1),
	RunE:    runTopicShow,
}

var topicAttachCmd = &cobra.Command{
	Use:   "attach <id> <worktree>",
	Short: "Record that a worktree belongs to a topic",
	Args:  cobra.ExactArgs(2),
	RunE:  runTopicAttach,
}

var topicDetachCmd = &cobra.Command{
	Use:   "detach <id> <worktree>",
	Short: "Drop a worktree's membership without touching the worktree",
	Args:  cobra.ExactArgs(2),
	RunE:  runTopicDetach,
}

var topicRemoveCmd = &cobra.Command{
	Use:     "remove <id>",
	Aliases: []string{"rm"},
	Short:   "Detach every member, optionally removing their worktrees",
	Args:    cobra.ExactArgs(1),
	RunE:    runTopicRemove,
}

func init() {
	rootCmd.AddCommand(topicCmd)
	topicCmd.AddCommand(topicListCmd, topicShowCmd, topicAttachCmd, topicDetachCmd, topicRemoveCmd)

	topicRemoveCmd.Flags().BoolVar(&topicWithWorktrees, "with-worktrees", false,
		"Also remove each member's worktree from disk")
	topicRemoveCmd.Flags().BoolVar(&topicDeleteBranch, "delete-branch", false,
		"With --with-worktrees, also delete each branch when it is merged")
	topicRemoveCmd.Flags().BoolVarP(&topicForce, "force", "f", false,
		"Proceed even when a target has uncommitted changes")
	topicRemoveCmd.Flags().BoolVar(&topicYes, "yes", false, "Skip the confirmation prompt")
	topicRemoveCmd.Flags().BoolVar(&topicDryRun, "dry-run", false,
		"Report what would happen and change nothing")

	topicShowCmd.ValidArgsFunction = completeTopicIDs
	topicRemoveCmd.ValidArgsFunction = completeTopicIDs
	topicAttachCmd.ValidArgsFunction = completeTopicAttachArgs
	topicDetachCmd.ValidArgsFunction = completeTopicDetachArgs
}

type topicMemberJSON struct {
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	// Path and Present join recorded membership to what is actually on disk. A member
	// with no worktree is drift, and a caller must be able to see it here rather than
	// discovering it during a removal.
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

type topicJSON struct {
	ID      string            `json:"id"`
	Members []topicMemberJSON `json:"members"`
	// Dangling counts members whose worktree is gone, which "hydra doctor --fix"
	// clears.
	Dangling int `json:"dangling"`

	// Parent is containment when declared, absent when the topic is flat — which stays the
	// default. Reported because a recorded relationship nobody can read is indistinguishable from
	// no relationship: `--parent` wrote it correctly and `topic list` showed nothing, so the
	// feature looked broken.
	Parent string `json:"parent,omitempty"`

	// Closed is the declaration that the work is finished. Whether it MAY be closed is derived on
	// demand by `topic close`, never stored.
	Closed bool `json:"closed,omitempty"`
}

type topicListJSON struct {
	Topics []topicJSON `json:"topics"`
	Total  int         `json:"total"`
}

func runTopicList(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	topics, err := topicStore().List()
	if err != nil {
		return classifyTopicErr(err)
	}

	payload := topicListJSON{Topics: make([]topicJSON, 0, len(topics)), Total: len(topics)}
	for _, t := range topics {
		payload.Topics = append(payload.Topics, describeTopic(t))
	}

	return emit(cmd, fmt.Sprintf("%d active topic(s)", len(topics)), payload, nil, func() {
		if len(payload.Topics) == 0 {
			fmt.Println("No active topics. \"hydra start <branch> --topic <id>\" creates one.")
			return
		}
		fmt.Println()
		for _, t := range payload.Topics {
			label := fmt.Sprintf("%d worktree(s)", len(t.Members))
			if t.Dangling > 0 {
				label += fmt.Sprintf(", %d missing", t.Dangling)
			}
			fmt.Printf("  %s  %s\n", styles.Label.Render(t.ID), label)
		}
		fmt.Println()
	})
}

func runTopicShow(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	t, err := requireTopicRecorded(strings.TrimSpace(args[0]))
	if err != nil {
		return err
	}
	payload := describeTopic(t)

	return emit(cmd, topicShowSummary(payload), payload, nil, func() {
		fmt.Println()
		fmt.Println(styles.Title.Render("Topic " + payload.ID))
		fmt.Println()
		for _, member := range payload.Members {
			state := styles.Success.Render("present")
			if !member.Present {
				state = styles.Error.Render("missing")
			}
			fmt.Printf("  %-24s %-28s %s\n", member.Repo, member.Branch, state)
		}
		fmt.Println()
		if payload.Dangling > 0 {
			fmt.Println(styles.Error.Render(
				fmt.Sprintf("  %d member(s) have no worktree; run \"hydra doctor --fix\"", payload.Dangling)))
			fmt.Println()
		}
	})
}

func topicShowSummary(payload topicJSON) string {
	if payload.Dangling == 0 {
		return fmt.Sprintf("topic %s: %d worktree(s)", payload.ID, len(payload.Members))
	}
	return fmt.Sprintf("topic %s: %d worktree(s), %d missing",
		payload.ID, len(payload.Members), payload.Dangling)
}

// describeTopic joins recorded membership to the worktrees on disk.
func describeTopic(t topic.Topic) topicJSON {
	live := liveWorktreesByKey()

	out := topicJSON{ID: t.ID, Parent: t.Parent, Closed: t.Closed, Members: make([]topicMemberJSON, 0, len(t.Members))}
	for _, member := range t.Members {
		entry := topicMemberJSON{Repo: member.Repo, Branch: member.Branch}
		if wt, ok := live[topicKey(member.Repo, member.Branch)]; ok {
			entry.Path = wt.Path
			// Being in `git worktree list` is not the same as existing. Git keeps the
			// registration after the directory is deleted, so `present` was true for a
			// member whose worktree had been rm -rf'd, and `dangling` counted zero — the
			// one field a caller reads to find exactly that. `present` names a fact about
			// disk, so it has to look at disk.
			if _, err := os.Stat(wt.Path); err == nil {
				entry.Present = true
			} else {
				out.Dangling++
			}
		} else {
			out.Dangling++
		}
		out.Members = append(out.Members, entry)
	}
	return out
}

// liveWorktreesByKey indexes the worktrees on disk by (repo, branch).
func liveWorktreesByKey() map[string]worktreeContext {
	items, _ := collectWorktrees(cfg, projectRoot)
	live := make(map[string]worktreeContext, len(items))
	for _, item := range items {
		if item.Branch == "" {
			continue
		}
		live[topicKey(item.RepoContext.Alias, item.Branch)] = item
	}
	return live
}

// requireTopicRecorded resolves an id against RECORDED membership only.
//
// show, detach and remove are CONSUMERS of a topic, so an unknown id is an error
// carrying the valid ids — never a branch-name match, and never an auto-create.
// Only start and attach may bring a topic into existence.
func requireTopicRecorded(id string) (topic.Topic, error) {
	if id == "" {
		return topic.Topic{}, output.Errorf(output.CodeNeedsInput, "a topic id is required").
			WithDetail("missing", []string{"<id>"})
	}

	t, ok, err := topicStore().Get(id)
	if err != nil {
		return topic.Topic{}, classifyTopicErr(err)
	}
	if !ok {
		known, listErr := topicStore().Names()
		if listErr != nil {
			return topic.Topic{}, classifyTopicErr(listErr)
		}
		return topic.Topic{}, output.Errorf(output.CodeTopicUnknown,
			"topic %q is not known; run \"hydra topic list\" to see active topics", id).
			WithDetail("topic", id).
			WithDetail("known", known)
	}
	return t, nil
}

type topicMembershipJSON struct {
	Topic  string `json:"topic"`
	Repo   string `json:"repo"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

func runTopicAttach(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	id := strings.TrimSpace(args[0])
	if id == "" {
		return output.Errorf(output.CodeNeedsInput, "a topic id is required").
			WithDetail("missing", []string{"<id>"})
	}

	items, _ := collectWorktrees(cfg, projectRoot)
	wt, err := resolveOneWorktree(items, args[1])
	if err != nil {
		return err
	}
	if wt.Branch == "" {
		// Membership is keyed by branch, so a detached HEAD has nothing to record.
		return output.Errorf(output.CodeInternal,
			"%s is detached; membership is recorded per branch", wt.Qualified()).
			WithDetail("worktree", wt.Qualified())
	}

	// attach UPSERTS: this is one of only two commands that may create a topic, which
	// is what lets ad-hoc work be promoted with no migration step.
	member := topic.Member{Repo: wt.RepoContext.Alias, Branch: wt.Branch}
	if err := topicStore().Attach(id, member); err != nil {
		return classifyTopicErr(err)
	}

	payload := topicMembershipJSON{Topic: id, Repo: member.Repo, Branch: member.Branch, Path: wt.Path}
	return emit(cmd, fmt.Sprintf("attached %s to topic %s", wt.Qualified(), id), payload, nil, func() {
		fmt.Printf("Attached %s (%s) to topic %s\n", wt.Qualified(), wt.BranchLabel(), id)
	})
}

func runTopicDetach(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	t, err := requireTopicRecorded(strings.TrimSpace(args[0]))
	if err != nil {
		return err
	}

	// Resolve the handle against THIS TOPIC'S members only.
	//
	// Matching every worktree would let a handle name something that is not a member
	// and then fail with a confusing "not in this topic" — narrowing first makes the
	// error "no such member", which is the real problem.
	member, err := resolveTopicMember(t, args[1])
	if err != nil {
		return err
	}

	if err := topicStore().Detach(t.ID, member.Repo, member.Branch); err != nil {
		return classifyTopicErr(err)
	}

	payload := topicMembershipJSON{Topic: t.ID, Repo: member.Repo, Branch: member.Branch}
	return emit(cmd, fmt.Sprintf("detached %s@%s from topic %s", member.Repo, member.Branch, t.ID),
		payload, nil, func() {
			fmt.Printf("Detached %s on %s from topic %s\n", member.Repo, member.Branch, t.ID)
		})
}

// resolveTopicMember matches a handle against one topic's members.
func resolveTopicMember(t topic.Topic, query string) (topic.Member, error) {
	query = strings.TrimSpace(query)
	live := liveWorktreesByKey()

	var matches []topic.Member
	var handles []string
	for _, member := range t.Members {
		handle := member.Repo + "@" + member.Branch
		if wt, ok := live[topicKey(member.Repo, member.Branch)]; ok {
			handle = wt.Qualified()
			if strings.EqualFold(wt.Qualified(), query) || strings.EqualFold(wt.DirName, query) {
				matches = append(matches, member)
				continue
			}
		}
		handles = append(handles, handle)
		if strings.EqualFold(member.Branch, query) || strings.EqualFold(member.Repo, query) {
			matches = append(matches, member)
		}
	}

	sort.Strings(handles)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return topic.Member{}, output.Errorf(output.CodeWorktreeUnknown,
			"%q is not a member of topic %s", query, t.ID).
			WithDetail("worktree", query).
			WithDetail("topic", t.ID).
			WithDetail("members", handles)
	default:
		return topic.Member{}, output.Errorf(output.CodeWorktreeNameConflict,
			"%q matches %d members of topic %s; name one exactly", query, len(matches), t.ID).
			WithDetail("worktree", query).
			WithDetail("topic", t.ID)
	}
}

type topicRemoveTargetJSON struct {
	Repo          string `json:"repo"`
	Branch        string `json:"branch"`
	Path          string `json:"path"`
	Present       bool   `json:"present"`
	Detached      bool   `json:"detached"`
	WorktreeGone  bool   `json:"worktree_removed"`
	BranchDeleted bool   `json:"branch_deleted"`
	Error         string `json:"error,omitempty"`
}

type topicRemoveJSON struct {
	Topic   string                  `json:"topic"`
	DryRun  bool                    `json:"dry_run"`
	Targets []topicRemoveTargetJSON `json:"targets"`
	Removed bool                    `json:"topic_removed"`
}

func runTopicRemove(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	// 1. Resolve the topic. An unknown id is topic_unknown with the known ids, never
	//    a branch-name fallback.
	t, err := requireTopicRecorded(strings.TrimSpace(args[0]))
	if err != nil {
		return err
	}

	// 2. Enumerate targets, joining membership to the worktrees on disk. A member with
	//    no worktree is detach-only rather than an error: that is exactly the drift an
	//    interrupted removal leaves, and refusing here would make it unclearable.
	// pre_topic_remove fires ONCE, before the first member is touched. Teardown detaches and
	// commits per member, so a hook firing mid-loop would see a half-dismantled set — and this is
	// the only point at which a check can still veto losing the whole unit of work.
	preRemove, preErr := runHookEvent("pre_topic_remove", topicHookContext(t.ID), projectRoot)
	if preErr != nil {
		return preErr
	}
	topicRemoveWarnings := preRemove.Warnings

	described := describeTopic(t)
	live := liveWorktreesByKey()

	// 3 and 4. AGGREGATE gates, before ANY mutation.
	//
	// This is the whole reason a multi-target removal is not a loop over the
	// single-worktree path: checking per item would remove the first three worktrees
	// and then refuse the fourth, leaving the topic half-destroyed with no record of
	// intent. Both gates inspect every target first and refuse the whole operation.
	if topicWithWorktrees {
		if err := ensureNoDirtyTargets(described, live); err != nil {
			return err
		}
		if topicDeleteBranch {
			if err := ensureAllBranchesDeletable(described, live); err != nil {
				return err
			}
		}
	}

	// 5. Preview and confirm once, listing every target.
	if topicDryRun {
		return emitTopicRemovePreview(cmd, described)
	}
	if err := confirmTopicRemoval(described); err != nil {
		return err
	}

	// 6. Per member, in stable order.
	payload, failures := performTopicRemoval(t, described, live)

	// 7. The topic is gone once its last member detached. Garbage collection is the
	//    only path that removes the identity, so there is no race between removing
	//    membership and removing the topic.
	_, stillExists, err := topicStore().Get(t.ID)
	if err != nil {
		return classifyTopicErr(err)
	}
	payload.Removed = !stillExists

	// 8. Report. partial_failure when any git step failed.
	summary := topicRemoveSummary(payload, failures)
	if emitErr := emitResult(cmd, output.Result{
		Outcome:  topicRemoveOutcome(failures),
		Summary:  summary,
		Data:     payload,
		Warnings: topicRemoveWarnings,
	}, func() { printTopicRemoveText(payload, summary) }); emitErr != nil {
		return emitErr
	}
	if failures > 0 {
		return output.Errorf(output.CodePartialFailure,
			"%d of %d member(s) failed", failures, len(payload.Targets)).
			WithDetail("topic", payload.Topic)
	}
	return nil
}

func topicRemoveOutcome(failures int) output.Outcome {
	if failures > 0 {
		return output.OutcomePartial
	}
	return output.OutcomeSuccess
}

// ensureNoDirtyTargets refuses the whole removal if ANY target has uncommitted work.
func ensureNoDirtyTargets(described topicJSON, live map[string]worktreeContext) error {
	if topicForce {
		return nil
	}

	var dirty []map[string]any
	for _, member := range described.Members {
		wt, ok := live[topicKey(member.Repo, member.Branch)]
		if !ok {
			continue
		}
		hasChanges, count, err := git.HasUncommittedChanges(wt.Path)
		if err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to inspect %s", wt.Path)
		}
		if hasChanges {
			dirty = append(dirty, map[string]any{
				"repo": member.Repo, "branch": member.Branch,
				"path": wt.Path, "changes": count,
			})
		}
	}
	if len(dirty) == 0 {
		return nil
	}
	return output.Errorf(output.CodeWorktreeDirty,
		"%d worktree(s) in topic %s have uncommitted changes; commit, stash, or pass --force",
		len(dirty), described.ID).
		WithDetail("topic", described.ID).
		WithDetail("dirty", dirty)
}

// ensureAllBranchesDeletable checks every branch before the first removal, so
// --delete-branch cannot strand half the topic.
func ensureAllBranchesDeletable(described topicJSON, live map[string]worktreeContext) error {
	if topicForce {
		return nil
	}

	var blocked []map[string]any
	for _, member := range described.Members {
		wt, ok := live[topicKey(member.Repo, member.Branch)]
		if !ok || wt.Branch == "" {
			continue
		}
		if _, err := ensureBranchDeletable(wt.RepoContext, wt.Branch); err != nil {
			blocked = append(blocked, map[string]any{
				"repo": member.Repo, "branch": member.Branch,
				"error": output.Classify(err).Message,
			})
		}
	}
	if len(blocked) == 0 {
		return nil
	}
	return output.Errorf(output.CodeGitFailed,
		"%d branch(es) in topic %s are not merged; nothing was removed", len(blocked), described.ID).
		WithDetail("topic", described.ID).
		WithDetail("branches", blocked)
}

func emitTopicRemovePreview(cmd *cobra.Command, described topicJSON) error {
	payload := topicRemoveJSON{Topic: described.ID, DryRun: true}
	for _, member := range described.Members {
		payload.Targets = append(payload.Targets, topicRemoveTargetJSON{
			Repo: member.Repo, Branch: member.Branch,
			Path: member.Path, Present: member.Present,
		})
	}

	verb := "detach"
	if topicWithWorktrees {
		verb = "remove"
	}
	summary := fmt.Sprintf("dry run: would %s %d member(s) of topic %s",
		verb, len(payload.Targets), described.ID)

	return emit(cmd, summary, payload, nil, func() {
		fmt.Println()
		fmt.Println(styles.Title.Render(summary))
		fmt.Println()
		for _, target := range payload.Targets {
			fmt.Printf("  %-20s %-28s %s\n", target.Repo, target.Branch, target.Path)
		}
		fmt.Println()
	})
}

// confirmTopicRemoval asks once, listing every target.
//
// A non-TTY invocation without --yes REFUSES rather than proceeding: a destructive
// multi-worktree operation must never be implied, and a prompt that cannot be shown
// is not consent.
func confirmTopicRemoval(described topicJSON) error {
	if topicYes {
		return nil
	}

	verb := "Detach"
	if topicWithWorktrees {
		verb = "Remove the worktrees of"
	}

	if !interactive() {
		return output.Errorf(output.CodeNeedsInput,
			"%s %d member(s) of topic %s requires confirmation; pass --yes",
			strings.ToLower(verb), len(described.Members), described.ID).
			WithDetail("missing", []string{"--yes"}).
			WithDetail("topic", described.ID).
			WithDetail("targets", len(described.Members))
	}

	fmt.Println()
	for _, member := range described.Members {
		state := ""
		if !member.Present {
			state = "  (worktree missing)"
		}
		fmt.Printf("  %-20s %-28s%s\n", member.Repo, member.Branch, state)
	}
	fmt.Println()

	confirm := false
	title := fmt.Sprintf("%s %d member(s) of topic %s?", verb, len(described.Members), described.ID)
	if err := huh.NewConfirm().Title(title).Value(&confirm).Run(); err != nil {
		return output.Wrap(output.CodeInternal, err, "cancelled")
	}
	if !confirm {
		return output.Errorf(output.CodeInternal, "cancelled")
	}
	return nil
}

// performTopicRemoval processes members one at a time in stable order.
func performTopicRemoval(t topic.Topic, described topicJSON, live map[string]worktreeContext) (topicRemoveJSON, int) {
	payload := topicRemoveJSON{Topic: t.ID}
	failures := 0

	for _, member := range described.Members {
		target := topicRemoveTargetJSON{
			Repo: member.Repo, Branch: member.Branch,
			Path: member.Path, Present: member.Present,
		}
		wt, present := live[topicKey(member.Repo, member.Branch)]

		if topicWithWorktrees && present {
			hctx := hooksContextFor(wt.RepoContext, wt.Branch, wt.Path)
			if _, err := runHookEvent("pre_remove", hctx, wt.Path); err != nil {
				target.Error = output.Classify(err).Message
				failures++
				payload.Targets = append(payload.Targets, target)
				continue
			}

			if err := git.RemoveWorktree(wt.RepoContext.BareRepo, wt.Path, topicForce); err != nil {
				target.Error = output.Classify(
					output.Wrap(output.CodeGitFailed, err, "failed to remove %s", wt.Path)).Message
				failures++
				payload.Targets = append(payload.Targets, target)
				continue
			}
			target.WorktreeGone = true
		}

		// Detach strictly AFTER the removal succeeded.
		//
		// Detaching first and then dying leaves a live worktree that looks like
		// ordinary unassigned work, so nothing reports it and the user never learns it
		// was meant to go. Detaching after leaves a topic_dangling_member, which
		// doctor names and --fix clears. Loud and recoverable beats silent and wrong.
		if err := topicStore().Detach(t.ID, member.Repo, member.Branch); err != nil {
			target.Error = output.Classify(classifyTopicErr(err)).Message
			failures++
			payload.Targets = append(payload.Targets, target)
			continue
		}
		target.Detached = true

		if topicWithWorktrees && present && topicDeleteBranch && wt.Branch != "" {
			if err := git.DeleteBranch(wt.RepoContext.BareRepo, wt.Branch, true); err != nil {
				target.Error = output.Classify(
					output.Wrap(output.CodeGitFailed, err, "branch %q was not deleted", wt.Branch)).Message
				failures++
			} else {
				target.BranchDeleted = true
			}
		}

		payload.Targets = append(payload.Targets, target)
	}

	if topicWithWorktrees {
		// Clean each affected group directory ONCE. Several members of a topic often
		// share a group, so keying by group avoids repeated stat calls and makes the
		// intent obvious.
		groups := make(map[string]struct{})
		for _, target := range payload.Targets {
			if !target.WorktreeGone {
				continue
			}
			if wt, ok := live[topicKey(target.Repo, target.Branch)]; ok {
				groups[wt.RepoContext.Group] = struct{}{}
			}
		}
		for group := range groups {
			// Failure is not reported: an empty group directory left behind is
			// cosmetic, and the removal itself already succeeded.
			_, _ = removeGroupDirIfEmpty(projectRoot, group)
		}
		// post_remove is a PER-WORKTREE event, so it fires once for each worktree that went, with
		// the same context `hydra remove` gives it. One firing with an empty context told a hook
		// something had been removed without saying what, and disagreed with the other command
		// that raises the same event. The once-per-operation need is pre_topic_remove's job.
		for _, target := range payload.Targets {
			if !target.WorktreeGone {
				continue
			}
			wt, ok := live[topicKey(target.Repo, target.Branch)]
			if !ok {
				continue
			}
			_, _ = runHookEvent("post_remove",
				hooksContextFor(wt.RepoContext, wt.Branch, wt.Path), projectRoot)
		}
	}

	return payload, failures
}

func topicRemoveSummary(payload topicRemoveJSON, failures int) string {
	var detached, removed, branches int
	for _, target := range payload.Targets {
		if target.Detached {
			detached++
		}
		if target.WorktreeGone {
			removed++
		}
		if target.BranchDeleted {
			branches++
		}
	}

	parts := []string{fmt.Sprintf("%d detached", detached)}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d worktree(s) removed", removed))
	}
	if branches > 0 {
		parts = append(parts, fmt.Sprintf("%d branch(es) deleted", branches))
	}
	if failures > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failures))
	}
	if payload.Removed {
		parts = append(parts, fmt.Sprintf("topic %s is gone", payload.Topic))
	}
	return strings.Join(parts, ", ")
}

func printTopicRemoveText(payload topicRemoveJSON, summary string) {
	fmt.Println()
	fmt.Println(styles.Success.Render("✓ " + summary))
	fmt.Println()
	for _, target := range payload.Targets {
		if target.Error != "" {
			fmt.Printf("  %s %-18s %s\n", styles.Error.Render("fail"), target.Repo, target.Error)
			continue
		}
		fmt.Printf("  %s %-18s %s\n", styles.Success.Render("ok  "), target.Repo, target.Branch)
	}
	fmt.Println()
}

// completeTopicIDs completes recorded topic ids.
func completeTopicIDs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	names, err := topicStore().Names()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeTopicAttachArgs completes the topic id, then any worktree handle.
func completeTopicAttachArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return completeTopicIDs(cmd, args, toComplete)
	case 1:
		return completeWorktreeNames(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completeTopicDetachArgs completes the topic id, then only THAT topic's members —
// completing every worktree would suggest values detach must reject.
func completeTopicDetachArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return completeTopicIDs(cmd, args, toComplete)
	}
	if len(args) > 1 || cfg == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	t, ok, err := topicStore().Get(strings.TrimSpace(args[0]))
	if err != nil || !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	live := liveWorktreesByKey()
	handles := make([]string, 0, len(t.Members))
	for _, member := range t.Members {
		if wt, present := live[topicKey(member.Repo, member.Branch)]; present {
			handles = append(handles, wt.Qualified())
			continue
		}
		handles = append(handles, member.Repo+"@"+member.Branch)
	}
	sort.Strings(handles)
	return handles, cobra.ShellCompDirectiveNoFileComp
}

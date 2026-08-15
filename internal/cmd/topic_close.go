package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/hooks"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	topicCloseReopen bool
	topicCloseForce  bool
)

var topicCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Declare a topic's work finished, if its children are in",
	Long: `Record that a topic's work is done.

A topic with nothing depending on it and nothing inside it closes immediately: whether its
own work reached anywhere is its parent's question, not its own.

Otherwise closing is GATED, and every blocker is reported at once rather than one per
invocation:

  part_of      every topic inside this one must be closed, and each of their branches must
               have reached this topic's branch in the same repository. That second half is
               derived from git on every call, never stored — a stored answer would be wrong
               the moment someone rebases, the same way "behind: 0" is wrong without a fetch.
  depends_on   every topic this one waits on must be closed. Peers share no integration
               branch, so merged-ness is not checkable and is not pretended.

--force closes anyway and reports what it overrode as warnings. The gate is a default, not
a policy: hydra tells you what is unfinished, you decide whether it matters.

Merging is not hydra's job. It reports whether you CAN close; you merge.`,
	Example: `  # Close a leaf
  $ hydra topic close feat-social-auth

  # Refuses while something inside is open or unmerged, or a dependency is open
  $ hydra topic close epic-login

  # Close over the objections, which are still reported
  $ hydra topic close epic-login --force

  # Reopen
  $ hydra topic close epic-login --reopen`,
	Args:              cobra.ExactArgs(1),
	RunE:              runTopicClose,
	ValidArgsFunction: completeTopicIDs,
}

func init() {
	topicCloseCmd.Flags().BoolVar(&topicCloseReopen, "reopen", false,
		"reopen a closed topic instead of closing it")
	topicCloseCmd.Flags().BoolVar(&topicCloseForce, "force", false,
		"close even when children or dependencies are unfinished; blockers become warnings")
	topicCmd.AddCommand(topicCloseCmd)
}

// blocker is one reason a topic cannot close yet. Every blocker names the child and, where the
// problem is a specific worktree, the repo and branch — "not closeable" without a subject is a
// message that sends someone reading state by hand.
type blocker struct {
	Topic  string `json:"topic"`
	Repo   string `json:"repo,omitempty"`
	Branch string `json:"branch,omitempty"`
	Reason string `json:"reason"`
}

const (
	// reasonOpen: a child has not been closed.
	reasonOpen = "open"
	// reasonNotMerged: a child's branch has not reached the parent's branch in that repo.
	reasonNotMerged = "not_merged"
	// reasonNoTarget: the child has a member in a repository the parent does not cover, so there
	// is nowhere for that work to integrate. Treating a missing target as satisfied would report
	// done over stranded work, which is the one outcome worse than a false refusal.
	reasonNoTarget = "no_integration_target"
	// reasonDependencyOpen: a topic this one declares depends_on is still open. Peers share no
	// integration branch, so being closed is the only thing that can be checked — and inventing
	// a merge target between peers is exactly the kind of pretending this gate exists to avoid.
	reasonDependencyOpen = "dependency_open"
	// reasonDependencyMissing: a depends_on edge naming a topic that does not exist. Unreachable
	// through the CLI, which sweeps inbound edges when a topic dies, so it means hand-edited
	// state — reported rather than skipped, because silently treating it as satisfied would let
	// a typo close the gate. `hydra doctor --fix` drops the edge.
	reasonDependencyMissing = "dependency_missing"
)

type topicCloseJSON struct {
	Topic     string    `json:"topic"`
	Closed    bool      `json:"closed"`
	Children  int       `json:"children"`
	BlockedBy []blocker `json:"blocked_by,omitempty"`
	// Forced records that the gate was overridden, so an audit of closed topics can tell a
	// clean close from one that was declared over open work.
	Forced bool `json:"forced,omitempty"`
}

func runTopicClose(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	id := strings.TrimSpace(args[0])
	store := topicStore()

	parent, ok, err := store.Get(id)
	if err != nil {
		return classifyTopicErr(err)
	}
	if !ok {
		return unknownTopicError(store, id)
	}

	if topicCloseReopen {
		if err := store.SetClosed(id, false); err != nil {
			return classifyTopicErr(err)
		}
		return emitResult(cmd, output.Result{
			Summary: fmt.Sprintf("topic %s reopened", id),
			Data:    topicCloseJSON{Topic: id, Closed: false},
		}, func() {
			fmt.Println()
			fmt.Println(styles.Success.Render("✓ Topic reopened"))
			fmt.Printf("  Topic: %s\n", id)
		})
	}

	children, err := store.Children(id)
	if err != nil {
		return classifyTopicErr(err)
	}
	blockers := closeBlockers(store, parent, children)

	payload := topicCloseJSON{Topic: id, Children: len(children), BlockedBy: blockers}
	var warnings []*output.Diagnostic
	if len(blockers) > 0 {
		if !topicCloseForce {
			return output.Errorf(output.CodeTopicNotCloseable,
				"topic %s cannot close yet: %d blocker(s)", id, len(blockers)).
				WithDetail("topic", id).
				WithDetail("blocked_by", blockers).
				WithNext(output.Next{
					Argv: []string{"hydra", "topic", "close", id, "--force"},
					Why:  "close anyway; every blocker is still reported",
				})
		}
		// Forced: the blockers are still the truth, so they ride the envelope rather than
		// being dropped — a forced close that reported nothing would be indistinguishable
		// from a clean one in a log.
		//
		// NOTES, not warnings. A warning degrades the outcome to `partial`, which exits 4,
		// and an override that still fails the invocation has not overridden anything:
		// `hydra topic close X --force && deploy` would never run deploy. This is the same
		// severity an `optional: true` hook failure gets, for the same reason — the user
		// declared this outcome acceptable, so the request WAS satisfied. The code stays on
		// each note so an agent can still find them.
		payload.Forced = true
		for _, b := range blockers {
			warnings = append(warnings, output.Notef(output.CodeTopicNotCloseable,
				"closed over blocker: %s", describeBlocker(b)).
				WithSubject("topic", b.Topic))
		}
	}

	// The veto point. A quality gate belongs here and nowhere else: post_add fires before any work
	// exists, and pre_remove fires when it is being thrown away.
	//
	// It runs under --force too: --force overrides HYDRA's gate, and a hook is the user's own
	// code. Skipping their check because they overrode ours would be hydra deciding which of
	// their rules counts; --no-hooks is how they skip their own.
	hookResult, hookErr := runHookEvent("pre_topic_close", topicHookContext(id), projectRoot)
	if hookErr != nil {
		return hookErr
	}
	warnings = append(warnings, hookResult.Warnings...)

	if err := store.SetClosed(id, true); err != nil {
		return classifyTopicErr(err)
	}
	payload.Closed = true

	after, afterErr := runHookEvent("post_topic_close", topicHookContext(id), projectRoot)
	warnings = append(warnings, after.Warnings...)
	var closeErr *output.Error
	if afterErr != nil {
		closeErr = output.Classify(afterErr)
	}

	return emitResult(cmd, output.Result{
		Summary:  fmt.Sprintf("topic %s closed", id),
		Data:     payload,
		Warnings: warnings,
		Err:      closeErr,
	}, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Topic closed"))
		fmt.Printf("  Topic:    %s\n", id)
		fmt.Printf("  Children: %d\n", len(children))
		if payload.Forced {
			fmt.Println()
			fmt.Println(styles.Error.Render(
				fmt.Sprintf("  forced over %d blocker(s)", len(payload.BlockedBy))))
		}
	})
}

// closeBlockers derives why a topic cannot close: from what is inside it, and from what it
// waits on.
//
// Containment is checked at MEMBER granularity, because "is the child merged into the parent"
// is ambiguous the moment the two span different repositories: for each child member
// (repo, branch), the parent must have a member in that same repo to merge into. A child reaching
// into a repository its parent does not cover has nowhere to integrate, and reporting that as
// satisfied would claim done over stranded work.
//
// Dependencies are checked at TOPIC granularity, and only for being closed. Two peers share no
// integration branch, so there is no merge to verify — asking git anyway would mean inventing a
// target, which is the same false claim in the opposite direction.
func closeBlockers(store *topic.Store, parent topic.Topic, children []topic.Topic) []blocker {
	var out []blocker
	parentBranch := map[string]string{}
	for _, m := range parent.Members {
		parentBranch[m.Repo] = m.Branch
	}

	for _, l := range parent.Links {
		if l.Kind != topic.KindDependsOn {
			continue
		}
		target, ok, err := store.Get(l.To)
		switch {
		case err != nil:
			// The store is unreadable for this edge. Reporting it as a blocker is the safe
			// direction: the alternative is closing because a check could not run.
			out = append(out, blocker{Topic: l.To, Reason: reasonDependencyMissing})
		case !ok:
			out = append(out, blocker{Topic: l.To, Reason: reasonDependencyMissing})
		case !target.Closed:
			out = append(out, blocker{Topic: l.To, Reason: reasonDependencyOpen})
		}
	}
	for _, child := range children {
		if !child.Closed {
			out = append(out, blocker{Topic: child.ID, Reason: reasonOpen})
			// Still check its members: reporting every reason at once beats a caller fixing one
			// blocker per invocation.
		}
		for _, m := range child.Members {
			into, ok := parentBranch[m.Repo]
			if !ok {
				out = append(out, blocker{
					Topic: child.ID, Repo: m.Repo, Branch: m.Branch, Reason: reasonNoTarget,
				})
				continue
			}
			ref, ok := cfg.FindRepo(m.Repo)
			if !ok {
				out = append(out, blocker{
					Topic: child.ID, Repo: m.Repo, Branch: m.Branch, Reason: reasonNoTarget,
				})
				continue
			}
			bare := cfg.BarePath(projectRoot, ref.Alias)
			if !git.IsBranchMerged(bare, m.Branch, into) {
				out = append(out, blocker{
					Topic: child.ID, Repo: m.Repo, Branch: m.Branch, Reason: reasonNotMerged,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Topic != out[j].Topic {
			return out[i].Topic < out[j].Topic
		}
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

// describeBlocker renders one blocker as a sentence, for the warnings a forced close carries.
//
// The JSON keeps the machine-readable reason; a warning is read by a person, and
// "dependency_open" alone does not say which topic or what to do about it.
func describeBlocker(b blocker) string {
	switch b.Reason {
	case reasonOpen:
		return fmt.Sprintf("topic %s is still open", b.Topic)
	case reasonNotMerged:
		return fmt.Sprintf("%s %s@%s has not reached this topic's branch", b.Topic, b.Repo, b.Branch)
	case reasonNoTarget:
		return fmt.Sprintf("%s %s@%s has no branch here to integrate into", b.Topic, b.Repo, b.Branch)
	case reasonDependencyOpen:
		return fmt.Sprintf("dependency %s is still open", b.Topic)
	case reasonDependencyMissing:
		return fmt.Sprintf("dependency %s is not recorded; run \"hydra doctor --fix\"", b.Topic)
	}
	return fmt.Sprintf("%s: %s", b.Topic, b.Reason)
}

// unknownTopicError mirrors the shape topic commands already use: the id, and every real id, so a
// caller can self-correct instead of guessing. Guessing a topic from a branch name is the exact
// thing this package refuses to do.
func unknownTopicError(store *topic.Store, id string) error {
	err := output.Errorf(output.CodeTopicUnknown, "topic %q is not recorded", id).
		WithDetail("topic", id)
	if names, listErr := store.Names(); listErr == nil {
		err = err.WithDetail("known", names)
	}
	return err
}

// topicHookContext builds the environment for a once-per-topic event.
//
// These events are NOT per-worktree: `post_add` fires once per created worktree, so wiring a
// notification to it posts N times for one piece of work. Repo and branch are deliberately empty —
// a topic-level event has no single one, and inventing one would make a hook look per-worktree.
func topicHookContext(id string) hooks.Context {
	return hooks.Context{
		Event:       "",
		Project:     cfg.Project,
		ProjectRoot: projectRoot,
		Topic:       id,
	}
}

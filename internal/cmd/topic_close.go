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

var topicCloseReopen bool

var topicCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Declare a topic's work finished, if its children are in",
	Long: `Record that a topic's work is done.

A topic with no children closes immediately: whether its own work reached anywhere is its
parent's question, not its own.

A topic WITH children closes only when every child is closed and every child's branch has
reached this topic's branch in the same repository. That second half is derived from git on
every call, never stored — a stored answer would be wrong the moment someone rebases, the
same way "behind: 0" is wrong without a fetch.

Merging is not hydra's job. It reports whether you CAN close; you merge.`,
	Example: `  # Close a leaf
  $ hydra topic close feat-social-auth

  # Refuses while a child is open or unmerged, naming which
  $ hydra topic close epic-login

  # Reopen
  $ hydra topic close epic-login --reopen`,
	Args:              cobra.ExactArgs(1),
	RunE:              runTopicClose,
	ValidArgsFunction: completeTopicIDs,
}

func init() {
	topicCloseCmd.Flags().BoolVar(&topicCloseReopen, "reopen", false,
		"reopen a closed topic instead of closing it")
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
)

type topicCloseJSON struct {
	Topic     string    `json:"topic"`
	Closed    bool      `json:"closed"`
	Children  int       `json:"children"`
	BlockedBy []blocker `json:"blocked_by,omitempty"`
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
	blockers := closeBlockers(parent, children)

	payload := topicCloseJSON{Topic: id, Children: len(children), BlockedBy: blockers}
	if len(blockers) > 0 {
		return output.Errorf(output.CodeTopicNotCloseable,
			"topic %s cannot close yet: %d blocker(s)", id, len(blockers)).
			WithDetail("topic", id).
			WithDetail("blocked_by", blockers)
	}

	// The veto point. A quality gate belongs here and nowhere else: post_add fires before any work
	// exists, and pre_remove fires when it is being thrown away.
	hookResult, hookErr := runHookEvent("pre_topic_close", topicHookContext(id), projectRoot)
	if hookErr != nil {
		return hookErr
	}
	warnings := hookResult.Warnings

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
	})
}

// closeBlockers derives why a topic cannot close, per child and per member.
//
// "Is the child merged into the parent" is ambiguous the moment the two span different
// repositories, so the question is asked at MEMBER granularity: for each child member
// (repo, branch), the parent must have a member in that same repo to merge into. A child reaching
// into a repository its parent does not cover has nowhere to integrate, and reporting that as
// satisfied would claim done over stranded work.
func closeBlockers(parent topic.Topic, children []topic.Topic) []blocker {
	var out []blocker
	parentBranch := map[string]string{}
	for _, m := range parent.Members {
		parentBranch[m.Repo] = m.Branch
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

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var topicLinkForce bool

var topicLinkCmd = &cobra.Command{
	Use:   "link <id> <kind> <target>",
	Short: "Record a relationship between two topics",
	Long: `Record one typed, directed relationship from a topic to another.

Two kinds carry meaning to hydra:

  part_of      containment. "hydra topic close" checks that this topic's branches reached
               the target's branch in the same repository, which it derives from git.
  depends_on   a peer dependency. There is no integration branch between peers, so
               merged-ness is not checkable and is not pretended: the gate is whether the
               target is closed.

Any other kind must be NAMESPACED — it has to contain a dot, as in acme.tested-by. hydra
stores and reports those and gates on nothing in them, so an extension can build its own
semantics on the same primitive. Bare words stay reserved for kinds hydra may define later.

Recording the same relationship twice changes nothing and exits 0. A relationship that
would close a loop in part_of or depends_on is refused, as is a topic pointed at itself in
any kind; --force records it anyway.`,
	Example: `  # This work integrates into an epic
  $ hydra topic link feat-social part_of epic-login

  # And it waits on another piece of work first
  $ hydra topic link feat-social depends_on feat-auth-tokens

  # Your own vocabulary, which hydra stores and never interprets
  $ hydra topic link feat-social acme.tested-by qa-suite

  # Record a cycle deliberately
  $ hydra topic link a depends_on b --force`,
	Args:              cobra.ExactArgs(3),
	RunE:              runTopicLink,
	ValidArgsFunction: completeTopicLinkArgs,
}

var topicUnlinkCmd = &cobra.Command{
	Use:   "unlink <id> <kind> <target>",
	Short: "Remove a recorded relationship",
	Long: `Remove one relationship. Nothing about the worktrees changes.

A relationship that is not recorded is an error carrying the ones that are: the usual cause
is a mistyped kind or target, and exiting 0 would hide it.`,
	Example:           `  $ hydra topic unlink feat-social depends_on feat-auth-tokens`,
	Args:              cobra.ExactArgs(3),
	RunE:              runTopicUnlink,
	ValidArgsFunction: completeTopicLinkArgs,
}

func init() {
	topicCmd.AddCommand(topicLinkCmd, topicUnlinkCmd)
	topicLinkCmd.Flags().BoolVar(&topicLinkForce, "force", false,
		"Record the relationship even when it closes a cycle or points at itself")
}

type topicLinkJSON struct {
	Topic string `json:"topic"`
	Kind  string `json:"kind"`
	To    string `json:"to"`
	// Recorded is false when the relationship already held: creation is convergent, so a
	// script that ensures a link is safe to re-run and can still tell whether it acted.
	Recorded bool `json:"recorded"`
	Forced   bool `json:"forced,omitempty"`
	Removed  bool `json:"removed,omitempty"`
}

func runTopicLink(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	id, kind, target := strings.TrimSpace(args[0]), strings.TrimSpace(args[1]), strings.TrimSpace(args[2])
	if err := topic.ValidateKind(kind); err != nil {
		return classifyTopicErr(err)
	}

	recorded, err := topicStore().AddLink(id, topic.Link{Kind: kind, To: target}, topicLinkForce)
	if err != nil {
		return classifyTopicErr(err)
	}

	payload := topicLinkJSON{Topic: id, Kind: kind, To: target, Recorded: recorded, Forced: topicLinkForce && recorded}
	summary := fmt.Sprintf("%s %s %s recorded", id, kind, target)
	var warnings []*output.Diagnostic
	if !recorded {
		summary = fmt.Sprintf("%s %s %s was already recorded", id, kind, target)
	} else if topicLinkForce {
		// A NOTE, not a warning: a warning degrades the envelope to `partial` and exits 4,
		// so `hydra topic link a depends_on b --force && next` would never reach next — an
		// override that fails the invocation has overridden nothing. The user asked for this
		// edge and got it. The code stays so an agent can still find the forced ones.
		warnings = append(warnings, output.Notef(output.CodeTopicCycle,
			"%s %s %s was recorded with --force; it may close a cycle", id, kind, target).
			WithSubject("topic", id))
	}
	return emitResult(cmd, output.Result{Summary: summary, Data: payload, Warnings: warnings}, func() {
		fmt.Println()
		if recorded {
			fmt.Println(styles.Success.Render("✓ Relationship recorded"))
		} else {
			fmt.Println(styles.Success.Render("✓ Already recorded"))
		}
		fmt.Printf("  %s  %s → %s\n", styles.Label.Render(id), kind, target)
	})
}

func runTopicUnlink(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return errNotInProject()
	}
	id, kind, target := strings.TrimSpace(args[0]), strings.TrimSpace(args[1]), strings.TrimSpace(args[2])

	if err := topicStore().RemoveLink(id, topic.Link{Kind: kind, To: target}); err != nil {
		return classifyTopicErr(err)
	}

	payload := topicLinkJSON{Topic: id, Kind: kind, To: target, Removed: true}
	return emitResult(cmd, output.Result{
		Summary: fmt.Sprintf("%s %s %s removed", id, kind, target),
		Data:    payload,
	}, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Relationship removed"))
		fmt.Printf("  %s  %s → %s\n", styles.Label.Render(id), kind, target)
	})
}

// completeTopicLinkArgs completes the topic, then the kind, then the target.
//
// The kind position offers the two reserved kinds only. A custom kind is by definition
// something hydra does not know, and suggesting a namespace prefix nobody uses would be
// noise; the two that carry behaviour are exactly the two worth completing.
func completeTopicLinkArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0, 2:
		return topicIDCandidates(), cobra.ShellCompDirectiveNoFileComp
	case 1:
		return []string{topic.KindPartOf, topic.KindDependsOn}, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

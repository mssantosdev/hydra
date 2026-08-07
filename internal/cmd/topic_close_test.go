package cmd

import (
	"testing"

	"github.com/mssantosdev/hydra/internal/topic"
)

// closeBlockers is where the design lives, so it is tested directly: the git half is covered by
// the e2e run, and what needs pinning here is the member-granular rule.
//
// "Is the child merged into the parent" is ambiguous the moment the two span different
// repositories, so the question is asked per member: the parent must have a member in the SAME
// repo to merge into. A child reaching into a repository its parent does not cover has nowhere to
// integrate, and reporting that as satisfied would claim done over stranded work.

func TestCloseBlockers_OpenChildBlocks(t *testing.T) {
	parent := topic.Topic{ID: "epic", Members: []topic.Member{{Repo: "api", Branch: "epic/x"}}}
	children := []topic.Topic{{ID: "feat", Closed: false}}

	got := closeBlockers(parent, children)
	if len(got) != 1 || got[0].Reason != reasonOpen || got[0].Topic != "feat" {
		t.Fatalf("blockers = %+v, want one open blocker naming feat", got)
	}
}

// A child member in a repo the parent does not cover must be reported, never treated as satisfied.
// This is the case a naive "is it merged" check passes vacuously, because there is nothing to
// compare against.
func TestCloseBlockers_MemberWithNoIntegrationTarget(t *testing.T) {
	parent := topic.Topic{ID: "epic", Members: []topic.Member{{Repo: "api", Branch: "epic/x"}}}
	children := []topic.Topic{{
		ID:     "feat",
		Closed: true,
		// `web` is not a repository the parent has a member in.
		Members: []topic.Member{{Repo: "web", Branch: "feat/y"}},
	}}

	got := closeBlockers(parent, children)
	if len(got) != 1 {
		t.Fatalf("blockers = %+v, want exactly one", got)
	}
	if got[0].Reason != reasonNoTarget {
		t.Errorf("reason = %q, want %q — a missing target must never pass silently", got[0].Reason, reasonNoTarget)
	}
	if got[0].Repo != "web" || got[0].Branch != "feat/y" {
		t.Errorf("blocker must name the stranded member, got %+v", got[0])
	}
}

// A leaf closes immediately: whether its own work reached anywhere is its parent's question.
func TestCloseBlockers_LeafHasNoBlockers(t *testing.T) {
	parent := topic.Topic{ID: "feat", Members: []topic.Member{{Repo: "api", Branch: "feat/x"}}}
	if got := closeBlockers(parent, nil); len(got) != 0 {
		t.Errorf("a topic with no children must be closeable, got %+v", got)
	}
}

// Every reason at once, so a caller fixes them in one pass rather than one refusal at a time.
func TestCloseBlockers_ReportsEveryReasonAtOnce(t *testing.T) {
	parent := topic.Topic{ID: "epic", Members: []topic.Member{{Repo: "api", Branch: "epic/x"}}}
	children := []topic.Topic{
		{ID: "b-open", Closed: false},
		{ID: "a-stranded", Closed: true, Members: []topic.Member{{Repo: "web", Branch: "feat/y"}}},
	}

	got := closeBlockers(parent, children)
	if len(got) != 2 {
		t.Fatalf("blockers = %+v, want two", got)
	}
	// Ordered, so two runs of the same broken state produce the same envelope.
	if got[0].Topic != "a-stranded" || got[1].Topic != "b-open" {
		t.Errorf("blockers are not ordered by topic: %+v", got)
	}
}

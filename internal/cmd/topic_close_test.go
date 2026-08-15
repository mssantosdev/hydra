package cmd

import (
	"strings"
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

	got := closeBlockers(topic.Open(t.TempDir()), parent, children)
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

	got := closeBlockers(topic.Open(t.TempDir()), parent, children)
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
	if got := closeBlockers(topic.Open(t.TempDir()), parent, nil); len(got) != 0 {
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

	got := closeBlockers(topic.Open(t.TempDir()), parent, children)
	if len(got) != 2 {
		t.Fatalf("blockers = %+v, want two", got)
	}
	// Ordered, so two runs of the same broken state produce the same envelope.
	if got[0].Topic != "a-stranded" || got[1].Topic != "b-open" {
		t.Errorf("blockers are not ordered by topic: %+v", got)
	}
}

// A dependency is gated on being CLOSED, not on being merged. Two peers share no integration
// branch, so there is no merge to verify — asking git anyway would mean inventing a target.
func TestCloseBlockers_OpenDependencyBlocks(t *testing.T) {
	root := t.TempDir()
	store := topic.Open(root)
	for _, id := range []string{"waiter", "blocker-topic"} {
		if err := store.Attach(id, topic.Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := store.AddLink("waiter", topic.Link{Kind: topic.KindDependsOn, To: "blocker-topic"}, false); err != nil {
		t.Fatalf("link: %v", err)
	}
	waiter, _, err := store.Get("waiter")
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	got := closeBlockers(store, waiter, nil)
	if len(got) != 1 || got[0].Reason != reasonDependencyOpen || got[0].Topic != "blocker-topic" {
		t.Fatalf("blockers = %+v, want one dependency_open naming blocker-topic", got)
	}

	// Closing the dependency clears the gate — and no git question was ever asked, so a
	// dependency whose branches went nowhere still counts as satisfied once declared done.
	if err := store.SetClosed("blocker-topic", true); err != nil {
		t.Fatalf("close dependency: %v", err)
	}
	if got := closeBlockers(store, waiter, nil); len(got) != 0 {
		t.Fatalf("a closed dependency must not block, got %+v", got)
	}
}

// A depends_on edge naming a topic that does not exist is reported, never skipped: treating
// an unresolvable edge as satisfied would let a typo open the gate. Only hand-edited state can
// produce it, because removing a topic sweeps the edges naming it.
func TestCloseBlockers_MissingDependencyIsReported(t *testing.T) {
	parent := topic.Topic{
		ID:      "waiter",
		Members: []topic.Member{{Repo: "api", Branch: "waiter"}},
		Links:   []topic.Link{{Kind: topic.KindDependsOn, To: "ghost"}},
	}

	got := closeBlockers(topic.Open(t.TempDir()), parent, nil)
	if len(got) != 1 || got[0].Reason != reasonDependencyMissing || got[0].Topic != "ghost" {
		t.Fatalf("blockers = %+v, want one dependency_missing naming ghost", got)
	}
}

// part_of is the containment kind; a custom kind must not gate anything, or hydra would be
// enforcing semantics it explicitly does not assign.
func TestCloseBlockers_CustomKindsNeverGate(t *testing.T) {
	root := t.TempDir()
	store := topic.Open(root)
	for _, id := range []string{"subject", "other"} {
		if err := store.Attach(id, topic.Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	// `other` stays open, and the edge to it is a custom kind.
	if _, err := store.AddLink("subject", topic.Link{Kind: "acme.blocks", To: "other"}, false); err != nil {
		t.Fatalf("link: %v", err)
	}
	subject, _, err := store.Get("subject")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := closeBlockers(store, subject, nil); len(got) != 0 {
		t.Fatalf("a custom kind must not gate close, got %+v", got)
	}
}

// Every blocker reason renders as a sentence naming its subject: a forced close reports them
// as warnings, and "dependency_open" alone does not say which topic or what to do.
func TestDescribeBlockerNamesItsSubject(t *testing.T) {
	for _, tc := range []struct {
		b    blocker
		want string
	}{
		{blocker{Topic: "feat", Reason: reasonOpen}, "feat"},
		{blocker{Topic: "feat", Repo: "api", Branch: "feat/x", Reason: reasonNotMerged}, "api@feat/x"},
		{blocker{Topic: "feat", Repo: "web", Branch: "feat/y", Reason: reasonNoTarget}, "web@feat/y"},
		{blocker{Topic: "dep", Reason: reasonDependencyOpen}, "dep"},
		{blocker{Topic: "ghost", Reason: reasonDependencyMissing}, "doctor"},
		{blocker{Topic: "x", Reason: "invented"}, "invented"},
	} {
		if got := describeBlocker(tc.b); !strings.Contains(got, tc.want) {
			t.Errorf("describeBlocker(%+v) = %q, want it to mention %q", tc.b, got, tc.want)
		}
	}
}

package topic

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A v1 file held containment in a scalar `parent:`. Reading one must not lose it, and the
// next write must persist the graph shape — otherwise an upgrade silently drops every
// declared relationship, which is the worst outcome this migration can have.
func TestV1FileMigratesParentIntoALink(t *testing.T) {
	root := t.TempDir()
	seedState(t, root, `version: "1"
topics:
  epic:
    members:
      - repo: api
        branch: epic
  feat:
    members:
      - repo: api
        branch: feat
    parent: epic
`)

	s := Open(root)
	feat, ok, err := s.Get("feat")
	if err != nil || !ok {
		t.Fatalf("Get(feat) = (%v, %v)", ok, err)
	}
	if len(feat.Links) != 1 || feat.Links[0] != (Link{Kind: KindPartOf, To: "epic"}) {
		t.Fatalf("v1 parent did not become a part_of link: %+v", feat.Links)
	}
	// The derived view agrees, so a v1 workspace answers "what is in this epic" without
	// being rewritten first.
	kids, err := s.Children("epic")
	if err != nil || len(kids) != 1 || kids[0].ID != "feat" {
		t.Fatalf("children over a v1 file = (%+v, %v)", kids, err)
	}

	// The first mutation persists v2 and drops the old key.
	if err := s.UpdateMeta("feat", map[string]string{"acme.x": "1"}, nil); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `version: "2"`) {
		t.Errorf("state was not rewritten as v2:\n%s", body)
	}
	if strings.Contains(body, "parent:") {
		t.Errorf("the v1 scalar survived the rewrite:\n%s", body)
	}
	if !strings.Contains(body, "kind: part_of") {
		t.Errorf("the migrated edge was not written:\n%s", body)
	}
}

// A file with no version key at all predates the field and can only be v1 shaped.
func TestVersionlessFileIsReadAsV1(t *testing.T) {
	root := t.TempDir()
	seedState(t, root, `topics:
  a:
    members:
      - repo: api
        branch: a
  b:
    members:
      - repo: api
        branch: b
    parent: a
`)
	b, ok, err := Open(root).Get("b")
	if err != nil || !ok || len(b.Links) != 1 {
		t.Fatalf("Get(b) = (%+v, %v, %v)", b, ok, err)
	}
}

// Deleting a topic must delete the references to it in the SAME write. Leaving them turns
// every removal into recorded drift, and a dependency gate would then block on a topic
// that no longer exists.
func TestRemoveSweepsInboundLinks(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	for _, from := range []string{"b", "c"} {
		if _, err := s.AddLink(from, Link{Kind: KindDependsOn, To: "a"}, false); err != nil {
			t.Fatalf("link %s: %v", from, err)
		}
	}
	if _, err := s.AddLink("c", Link{Kind: KindPartOf, To: "b"}, false); err != nil {
		t.Fatalf("link c part_of b: %v", err)
	}

	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, id := range []string{"b", "c"} {
		got, _, err := s.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		for _, l := range got.Links {
			if l.To == "a" {
				t.Errorf("%s still links to the removed topic: %+v", id, got.Links)
			}
		}
	}
	// The unrelated edge survives: the sweep is targeted, not a reset.
	if c, _, _ := s.Get("c"); len(c.Links) != 1 || c.Links[0].To != "b" {
		t.Errorf("the sweep took an unrelated edge: %+v", c.Links)
	}
}

// The last member leaving deletes the topic, so it must sweep too — that GC is the only
// other path that removes identity.
func TestDetachGCSweepsInboundLinks(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"gone", "keeper"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := s.AddLink("keeper", Link{Kind: KindDependsOn, To: "gone"}, false); err != nil {
		t.Fatalf("link: %v", err)
	}

	if err := s.Detach("gone", "api", "gone"); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	keeper, _, err := s.Get("keeper")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(keeper.Links) != 0 {
		t.Errorf("GC left a dangling edge: %+v", keeper.Links)
	}
}

// An edge may only name a live topic: identity without membership is what the GC removes,
// so allowing it would record an edge that is dangling the moment it is written.
func TestLinkEndpointsMustBeLive(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("real", Member{Repo: "api", Branch: "real"}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	var unknown *ErrUnknown
	if _, err := s.AddLink("real", Link{Kind: KindDependsOn, To: "ghost"}, false); !errors.As(err, &unknown) {
		t.Fatalf("unknown target = %v, want ErrUnknown", err)
	}
	if _, err := s.AddLink("ghost", Link{Kind: KindDependsOn, To: "real"}, false); !errors.As(err, &unknown) {
		t.Fatalf("unknown source = %v, want ErrUnknown", err)
	}
	// Even with force: force overrides the CYCLE gate, not referential integrity. An edge
	// to a topic that does not exist has nothing to override — there is no user intent it
	// could be expressing.
	if _, err := s.AddLink("real", Link{Kind: KindDependsOn, To: "ghost"}, true); !errors.As(err, &unknown) {
		t.Fatalf("forced unknown target = %v, want ErrUnknown", err)
	}
}

// Inbound is derived, never stored: a second copy of an edge is a second thing that can
// be wrong.
func TestInboundReportsEveryKind(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"target", "child", "waiter"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := s.AddLink("child", Link{Kind: KindPartOf, To: "target"}, false); err != nil {
		t.Fatalf("link child: %v", err)
	}
	if _, err := s.AddLink("waiter", Link{Kind: KindDependsOn, To: "target"}, false); err != nil {
		t.Fatalf("link waiter: %v", err)
	}

	in, err := s.Inbound("target")
	if err != nil {
		t.Fatalf("Inbound: %v", err)
	}
	if len(in) != 2 {
		t.Fatalf("Inbound = %+v, want two", in)
	}
	// Ordered by from, then kind, so output is stable for a diff.
	if in[0] != (InboundLink{From: "child", Kind: KindPartOf}) ||
		in[1] != (InboundLink{From: "waiter", Kind: KindDependsOn}) {
		t.Fatalf("Inbound not ordered: %+v", in)
	}
	if empty, err := s.Inbound("child"); err != nil || len(empty) != 0 {
		t.Fatalf("Inbound(child) = (%+v, %v), want empty", empty, err)
	}
}

// Meta is the user's own space: set, unset, and unsetting what was never there. The last
// one is a no-op on purpose — a value bag has no identity to be wrong about, and a strict
// version would force every plugin to read-then-guard a delete that is idempotent anyway.
func TestUpdateMetaSetsUnsetsAndTolerates(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("t", Member{Repo: "api", Branch: "t"}); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if err := s.UpdateMeta("t", map[string]string{"acme.pbi": "2072958", "ui.color": "red"}, nil); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, _, err := s.Get("t")
	if err != nil || got.Meta["acme.pbi"] != "2072958" || got.Meta["ui.color"] != "red" {
		t.Fatalf("meta = %+v (%v)", got.Meta, err)
	}

	if err := s.UpdateMeta("t", nil, []string{"ui.color", "never.set"}); err != nil {
		t.Fatalf("unset: %v", err)
	}
	got, _, _ = s.Get("t")
	if _, still := got.Meta["ui.color"]; still {
		t.Errorf("unset left the key: %+v", got.Meta)
	}
	if got.Meta["acme.pbi"] != "2072958" {
		t.Errorf("unset took an unrelated key: %+v", got.Meta)
	}

	// A returned map must not alias stored state: mutating it must not change the store.
	got.Meta["acme.pbi"] = "tampered"
	fresh, _, _ := s.Get("t")
	if fresh.Meta["acme.pbi"] != "2072958" {
		t.Error("the returned meta map aliased stored state")
	}

	// Emptying meta clears the key entirely rather than leaving an empty map on disk.
	if err := s.UpdateMeta("t", nil, []string{"acme.pbi"}); err != nil {
		t.Fatalf("final unset: %v", err)
	}
	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(raw), "meta:") {
		t.Errorf("empty meta was written:\n%s", raw)
	}

	// Values are unrestricted; keys are not.
	var keyErr *ErrMetaKey
	if err := s.UpdateMeta("t", map[string]string{"": "x"}, nil); !errors.As(err, &keyErr) {
		t.Fatalf("empty key = %v, want ErrMetaKey", err)
	}
	if err := s.UpdateMeta("t", map[string]string{"a\nb": "x"}, nil); !errors.As(err, &keyErr) {
		t.Fatalf("control char in key = %v, want ErrMetaKey", err)
	}
	if err := s.UpdateMeta("t", nil, []string{"a\nb"}); !errors.As(err, &keyErr) {
		t.Fatalf("control char in unset key = %v, want ErrMetaKey", err)
	}
	if err := s.UpdateMeta("t", map[string]string{"ok.multiline": "a\nb"}, nil); err != nil {
		t.Fatalf("a multiline VALUE is legitimate: %v", err)
	}
}

// The declarative path replaces wholesale, which is what makes a document the source of
// truth rather than a patch with hidden merge rules.
func TestReplaceLinksAndMetaAreWholesale(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := s.AddLink("a", Link{Kind: KindDependsOn, To: "b"}, false); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	if err := s.UpdateMeta("a", map[string]string{"old": "1"}, nil); err != nil {
		t.Fatalf("seed meta: %v", err)
	}

	if err := s.ReplaceLinks("a", []Link{{Kind: KindPartOf, To: "c"}}, false); err != nil {
		t.Fatalf("ReplaceLinks: %v", err)
	}
	if err := s.ReplaceMeta("a", map[string]string{"new": "2"}); err != nil {
		t.Fatalf("ReplaceMeta: %v", err)
	}
	got, _, err := s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(got.Links) != 1 || got.Links[0] != (Link{Kind: KindPartOf, To: "c"}) {
		t.Fatalf("links = %+v, want only part_of c", got.Links)
	}
	if len(got.Meta) != 1 || got.Meta["new"] != "2" {
		t.Fatalf("meta = %+v, want only new=2", got.Meta)
	}

	// An empty set clears both.
	if err := s.ReplaceLinks("a", nil, false); err != nil {
		t.Fatalf("clear links: %v", err)
	}
	if err := s.ReplaceMeta("a", nil); err != nil {
		t.Fatalf("clear meta: %v", err)
	}
	got, _, _ = s.Get("a")
	if len(got.Links) != 0 || len(got.Meta) != 0 {
		t.Fatalf("clear left %+v / %+v", got.Links, got.Meta)
	}
}

// A replacement set is validated against the document as it WILL be, so a cycle inside the
// new set is caught rather than written.
func TestReplaceLinksValidatesTheNewSet(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"a", "b"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if _, err := s.AddLink("b", Link{Kind: KindPartOf, To: "a"}, false); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := s.AddLink("a", Link{Kind: KindDependsOn, To: "b"}, false); err != nil {
		t.Fatalf("seed keeper: %v", err)
	}

	// a part_of b closes a → b → a.
	var cycle *ErrCycle
	err := s.ReplaceLinks("a", []Link{{Kind: KindPartOf, To: "b"}}, false)
	if !errors.As(err, &cycle) {
		t.Fatalf("ReplaceLinks with a cycle = %v, want ErrCycle", err)
	}
	// The refusal restored the previous set instead of leaving the topic stripped.
	got, _, _ := s.Get("a")
	if len(got.Links) != 1 || got.Links[0] != (Link{Kind: KindDependsOn, To: "b"}) {
		t.Fatalf("a refused replacement clobbered the links: %+v", got.Links)
	}
	// A self-edge in the document is refused for the same reason AddLink refuses it.
	if err := s.ReplaceLinks("a", []Link{{Kind: "acme.x", To: "a"}}, false); !errors.As(err, &cycle) {
		t.Fatalf("self edge in a document = %v, want ErrCycle", err)
	}
	// Unknown targets are refused the same way.
	var unknown *ErrUnknown
	if err := s.ReplaceLinks("a", []Link{{Kind: KindPartOf, To: "ghost"}}, false); !errors.As(err, &unknown) {
		t.Fatalf("unknown target = %v, want ErrUnknown", err)
	}
	// A bare custom kind never reaches the store's write path.
	var kindErr *ErrKind
	if err := s.ReplaceLinks("a", []Link{{Kind: "blocks", To: "b"}}, false); !errors.As(err, &kindErr) {
		t.Fatalf("bare kind = %v, want ErrKind", err)
	}
	// Force records the cycle, same override as AddLink.
	if err := s.ReplaceLinks("a", []Link{{Kind: KindPartOf, To: "b"}}, true); err != nil {
		t.Fatalf("forced replacement: %v", err)
	}
	// And a duplicate inside one document collapses instead of being stored twice.
	if err := s.ReplaceLinks("a", []Link{
		{Kind: KindDependsOn, To: "b"},
		{Kind: KindDependsOn, To: "b"},
	}, false); err != nil {
		t.Fatalf("duplicate in document: %v", err)
	}
	got, _, _ = s.Get("a")
	if len(got.Links) != 1 {
		t.Fatalf("duplicate was stored twice: %+v", got.Links)
	}
}

// Every mutation must refuse an id that is not recorded, or a typo would create a topic
// with relationships and no worktrees — identity hydra's GC would then delete.
func TestGraphMutationsRefuseUnknownTopics(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	var unknown *ErrUnknown
	for name, err := range map[string]error{
		"UpdateMeta":   s.UpdateMeta("ghost", map[string]string{"a": "b"}, nil),
		"ReplaceMeta":  s.ReplaceMeta("ghost", map[string]string{"a": "b"}),
		"ReplaceLinks": s.ReplaceLinks("ghost", nil, false),
		"RemoveLink":   s.RemoveLink("ghost", Link{Kind: KindPartOf, To: "x"}),
	} {
		if !errors.As(err, &unknown) {
			t.Errorf("%s on an unknown topic = %v, want ErrUnknown", name, err)
		}
	}
}

// Links are reported in a stable order regardless of the order they were recorded in, so
// JSON output is diffable and no consumer depends on insertion order.
func TestLinksAreReportedSorted(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"a", "m", "z"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	for _, l := range []Link{
		{Kind: KindPartOf, To: "z"},
		{Kind: "acme.x", To: "m"},
		{Kind: KindPartOf, To: "m"},
	} {
		if _, err := s.AddLink("a", l, false); err != nil {
			t.Fatalf("link %+v: %v", l, err)
		}
	}
	got, _, err := s.Get("a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []Link{
		{Kind: "acme.x", To: "m"},
		{Kind: KindPartOf, To: "m"},
		{Kind: KindPartOf, To: "z"},
	}
	if len(got.Links) != len(want) {
		t.Fatalf("links = %+v, want %+v", got.Links, want)
	}
	for i := range want {
		if got.Links[i] != want[i] {
			t.Fatalf("links = %+v, want %+v", got.Links, want)
		}
	}
}

// ValidateKind is the published rule; it is asserted directly because commands map its
// failure onto `usage` and a plugin author reads it before choosing a kind.
func TestValidateKind(t *testing.T) {
	for _, kind := range []string{KindPartOf, KindDependsOn, "acme.blocks", "a.b", "x9.y-z_1", "a.b.c"} {
		if err := ValidateKind(kind); err != nil {
			t.Errorf("ValidateKind(%q) = %v, want nil", kind, err)
		}
	}
	for _, kind := range []string{"", "blocks", "Acme.Blocks", "acme.", ".blocks", "acme..b", "acme b", "acme/b"} {
		if err := ValidateKind(kind); err == nil {
			t.Errorf("ValidateKind(%q) = nil, want refusal", kind)
		}
	}
}

package topic

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestOpenTouchesNothing(t *testing.T) {
	root := t.TempDir()
	s := Open(root)

	if _, ok, err := s.Get("1"); err != nil || ok {
		t.Fatalf("Get on empty store = (%v, %v), want (false, nil)", ok, err)
	}
	if list, err := s.List(); err != nil || len(list) != 0 {
		t.Fatalf("List = (%d, %v), want (0, nil)", len(list), err)
	}
	if names, err := s.Names(); err != nil || len(names) != 0 {
		t.Fatalf("Names = (%d, %v), want (0, nil)", len(names), err)
	}
	if _, ok, err := s.TopicOf("api", "main"); err != nil || ok {
		t.Fatalf("TopicOf = (%v, %v), want (false, nil)", ok, err)
	}

	// A read-only command in a fresh workspace must litter nothing.
	if _, err := os.Stat(Dir(root)); !os.IsNotExist(err) {
		t.Fatalf("reading an empty store created %s", Dir(root))
	}
}

func TestFirstAttachInitialisesState(t *testing.T) {
	root := t.TempDir()
	if err := Open(root).Attach("2072958", Member{Repo: "api", Branch: "feat/login"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if _, err := os.Stat(Path(root)); err != nil {
		t.Fatalf("state.yaml not created: %v", err)
	}

	// The guard keeps local state out of git while leaving the shared manifest
	// committable: a blanket "*" would make config.yaml uncommittable once the
	// manifest moves into this directory.
	body, err := os.ReadFile(filepath.Join(Dir(root), ".gitignore"))
	if err != nil {
		t.Fatalf("gitignore not written: %v", err)
	}
	const want = "*\n!.gitignore\n!config.yaml\n"
	if string(body) != want {
		t.Fatalf("gitignore = %q, want %q", body, want)
	}
}

func TestNumericIDRoundTripsAsString(t *testing.T) {
	root := t.TempDir()
	if err := Open(root).Attach("2072958", Member{Repo: "api", Branch: "feat/login"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	raw, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	// A numeric-looking id must not become a YAML integer.
	if !strings.Contains(string(raw), `"2072958"`) {
		t.Errorf("id was not quoted in state.yaml:\n%s", raw)
	}
	names, err := Open(root).Names()
	if err != nil || len(names) != 1 || names[0] != "2072958" {
		t.Fatalf("Names = (%v, %v), want [2072958]", names, err)
	}
}

func TestAttachIsIdempotent(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	m := Member{Repo: "api", Branch: "feat/login"}
	for range 3 {
		if err := s.Attach("1", m); err != nil {
			t.Fatalf("Attach: %v", err)
		}
	}

	got, ok, err := s.Get("1")
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v)", ok, err)
	}
	if len(got.Members) != 1 {
		t.Fatalf("members = %d, want 1 — attach must not duplicate", len(got.Members))
	}
}

func TestOneTopicPerWorktree(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	m := Member{Repo: "api", Branch: "feat/login"}
	if err := s.Attach("1", m); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	err := s.Attach("2", m)
	var claimed *ErrClaimed
	if !errors.As(err, &claimed) {
		t.Fatalf("second Attach = %v, want ErrClaimed", err)
	}
	if claimed.Existing != "1" || claimed.Requested != "2" {
		t.Fatalf("claimed = %+v, want existing 1 / requested 2", claimed)
	}

	// The rejected attach must leave nothing behind.
	names, err := s.Names()
	if err != nil || len(names) != 1 || names[0] != "1" {
		t.Fatalf("topics = (%v, %v), want [1]", names, err)
	}
}

func TestDetachOfLastMemberDeletesTopic(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("1", Member{Repo: "api", Branch: "b"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := s.Attach("1", Member{Repo: "web", Branch: "b"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	if err := s.Detach("1", "api", "b"); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if _, ok, _ := s.Get("1"); !ok {
		t.Fatal("topic vanished while it still had a member")
	}

	if err := s.Detach("1", "web", "b"); err != nil {
		t.Fatalf("Detach last: %v", err)
	}
	// GC on last detach is the only path that removes a topic.
	if _, ok, err := s.Get("1"); err != nil || ok {
		t.Fatalf("emptied topic survived: (%v, %v)", ok, err)
	}
}

func TestTopicOfFindsMembership(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("42", Member{Repo: "api", Branch: "feat/x"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}

	id, ok, err := s.TopicOf("api", "feat/x")
	if err != nil || !ok || id != "42" {
		t.Fatalf("TopicOf = (%q, %v, %v), want (42, true, nil)", id, ok, err)
	}
	if _, ok, _ := s.TopicOf("api", "other"); ok {
		t.Error("TopicOf matched a branch that is not a member")
	}
}

func TestRemoveDropsAllMembers(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("1", Member{Repo: "api", Branch: "b"}); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if err := s.Remove("1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, ok, err := s.TopicOf("api", "b"); err != nil || ok {
		t.Fatalf("membership survived Remove: (%v, %v)", ok, err)
	}
}

func TestNewerSchemaIsRefused(t *testing.T) {
	root := t.TempDir()
	seedState(t, root, "version: \"99\"\ntopics: {}\n")

	_, _, err := Open(root).Get("1")
	var ver *ErrVersion
	if !errors.As(err, &ver) {
		t.Fatalf("Get on a newer schema = %v, want ErrVersion", err)
	}
	if ver.Found != "99" || ver.Supported != SchemaVersion {
		t.Fatalf("version error = %+v", ver)
	}
}

func TestRefusesWhenDirIsAFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(Dir(root), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := Open(root).Attach("1", Member{Repo: "api", Branch: "b"})
	if err == nil {
		t.Fatal("Attach succeeded with .hydra as a file; want refusal")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("error = %v, want it to name the collision", err)
	}
}

// TestConcurrentAttachesAllLand is why the store locks at all: several hydra
// processes attaching different worktrees to one topic must all survive. Under an
// unlocked read-modify-write, writes are lost.
func TestConcurrentAttachesAllLand(t *testing.T) {
	root := t.TempDir()
	if err := Open(root).Attach("1", Member{Repo: "seed", Branch: "main"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = Open(root).Attach("1", Member{
				Repo:   fmt.Sprintf("repo%d", i),
				Branch: "feat/x",
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}

	got, ok, err := Open(root).Get("1")
	if err != nil || !ok {
		t.Fatalf("Get = (%v, %v)", ok, err)
	}
	if len(got.Members) != workers+1 { // +1 for the seed
		t.Fatalf("members = %d, want %d — a write was lost", len(got.Members), workers+1)
	}
}

// TestNoFileSprawl guards an explicit requirement: .hydra holds only the known
// entries, with no temp files left behind after repeated writes.
func TestNoFileSprawl(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for i := range 5 {
		if err := s.Attach("1", Member{Repo: fmt.Sprintf("r%d", i), Branch: "b"}); err != nil {
			t.Fatalf("Attach: %v", err)
		}
	}

	entries, err := os.ReadDir(Dir(root))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	allowed := map[string]bool{".gitignore": true, "state.yaml": true, "state.lock": true}
	for _, e := range entries {
		if !allowed[e.Name()] {
			t.Errorf("unexpected file in .hydra: %s", e.Name())
		}
	}
}

func TestMembersAreOrderedDeterministically(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, m := range []Member{
		{Repo: "web", Branch: "b"},
		{Repo: "api", Branch: "z"},
		{Repo: "api", Branch: "a"},
	} {
		if err := s.Attach("1", m); err != nil {
			t.Fatalf("Attach: %v", err)
		}
	}

	got, _, err := s.Get("1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	want := []Member{{Repo: "api", Branch: "a"}, {Repo: "api", Branch: "z"}, {Repo: "web", Branch: "b"}}
	for i := range want {
		if got.Members[i] != want[i] {
			t.Fatalf("members = %v, want %v (stable order by repo, branch)", got.Members, want)
		}
	}
}

func seedState(t *testing.T, root, body string) {
	t.Helper()
	if err := os.MkdirAll(Dir(root), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(Path(root), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// Containment is opt-in and recorded, never inferred. The refusal in this package's doc is about
// deriving membership from branch STEMS — a fuzzy query used as a destructive handle — which a
// declared parent has nothing to do with.
func TestSetParentAndChildren(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"epic", "feat-a", "feat-b"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	for _, child := range []string{"feat-a", "feat-b"} {
		if err := s.SetParent(child, "epic"); err != nil {
			t.Fatalf("set parent on %s: %v", child, err)
		}
	}

	kids, err := s.Children("epic")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 2 {
		t.Fatalf("children = %+v, want two", kids)
	}
	// A topic with no parent is flat, which stays the default.
	if top, _, _ := s.Get("epic"); top.Parent != "" {
		t.Errorf("epic should have no parent, got %q", top.Parent)
	}
}

// A cycle would make closeability non-terminating and force every walk to carry a visited set.
// Refusing the edge that closes it keeps every reader simple.
func TestSetParentRefusesCycles(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"a", "b", "c"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach: %v", err)
		}
	}
	if err := s.SetParent("b", "a"); err != nil {
		t.Fatalf("b under a: %v", err)
	}
	if err := s.SetParent("c", "b"); err != nil {
		t.Fatalf("c under b: %v", err)
	}

	// a under c would close a → b → c → a.
	if err := s.SetParent("a", "c"); err == nil {
		t.Error("a cycle must be refused")
	}
	// And the trivial self-parent.
	if err := s.SetParent("a", "a"); err == nil {
		t.Error("a topic must not be its own parent")
	}
	// Nothing was written by either refusal.
	if top, _, _ := s.Get("a"); top.Parent != "" {
		t.Errorf("a refused edge was recorded: %q", top.Parent)
	}
}

// Closed is stored because closing is an act. Closeability is not, because a stored answer would be
// wrong the moment someone rebases.
func TestSetClosedRoundTrips(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("feat", Member{Repo: "api", Branch: "feat/x"}); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if top, _, _ := s.Get("feat"); top.Closed {
		t.Error("a new topic is open")
	}
	if err := s.SetClosed("feat", true); err != nil {
		t.Fatalf("close: %v", err)
	}
	if top, _, _ := s.Get("feat"); !top.Closed {
		t.Error("close did not persist")
	}
	if err := s.SetClosed("feat", false); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if top, _, _ := s.Get("feat"); top.Closed {
		t.Error("reopen did not persist")
	}
	if err := s.SetClosed("nope", true); err == nil {
		t.Error("closing an unrecorded topic must fail rather than create one")
	}
}

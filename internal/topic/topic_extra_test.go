package topic

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gofrs/flock"
	"gopkg.in/yaml.v3"
)

func TestErrBusyWhenLockHeld(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(Dir(root), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	lock := flock.New(LockPath(root))
	if err := lock.Lock(); err != nil {
		t.Fatalf("lock: %v", err)
	}
	defer func() { _ = lock.Unlock() }()

	err := Open(root).Attach("1", Member{Repo: "api", Branch: "main"})
	var busy *ErrBusy
	if !errors.As(err, &busy) {
		t.Fatalf("Attach while locked = %v, want ErrBusy", err)
	}
	if busy.Path != LockPath(root) {
		t.Errorf("busy path = %q, want %q", busy.Path, LockPath(root))
	}
	if !IsBusy(err) {
		t.Error("IsBusy must recognise lock contention")
	}
	if busy.Error() == "" {
		t.Error("ErrBusy.Error() must name the lock path")
	}
}

func TestWriteAtomicNeverLeavesTempOrPartialState(t *testing.T) {
	root := t.TempDir()
	dir := Dir(root)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := "version: \"1\"\ntopics:\n  \"seed\":\n    members:\n      - repo: api\n        branch: main\n"
	if err := os.WriteFile(Path(root), []byte(old), 0o600); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	s := Open(root)
	doc, err := s.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	doc.Topics["added"] = &topicEntry{Members: []Member{{Repo: "web", Branch: "feat"}}}
	if err := s.writeAtomic(doc); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	assertValidStateFile(t, root, "")

	// 0500 on purpose: readable and traversable but NOT writable, which is how a write
	// failure is provoked. A directory cannot drop its execute bit and still be entered.
	//nolint:gosec // G302: a traversable read-only directory is the condition under test
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir: %v", err)
	}
	//nolint:gosec // G302: restoring the fixture directory to its original mode
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	failDoc := newDocument()
	failDoc.Topics["broken"] = &topicEntry{Members: []Member{{Repo: "x", Branch: "y"}}}
	if err := s.writeAtomic(failDoc); err == nil {
		t.Fatal("writeAtomic with unwritable dir must fail before replacing state")
	}
	assertNoTempFiles(t, dir)
	assertValidStateFile(t, root, old)
}

func TestParseRejectsInvalidYAML(t *testing.T) {
	_, err := parse([]byte(":\n- not: [valid"), Path(t.TempDir()))
	if err == nil {
		t.Fatal("invalid yaml must fail parse")
	}
}

func TestParseFillsMissingVersionAndTopics(t *testing.T) {
	doc, err := parse([]byte("topics: {}\n"), filepath.Join(t.TempDir(), "state.yaml"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if doc.Version != SchemaVersion {
		t.Errorf("version = %q, want %q", doc.Version, SchemaVersion)
	}
	if doc.Topics == nil {
		t.Fatal("topics map must be initialised")
	}
}

func TestErrorTypesFormatUsefully(t *testing.T) {
	claimed := &ErrClaimed{Repo: "api", Branch: "main", Existing: "1"}
	if !strings.Contains(claimed.Error(), "api@main") || !strings.Contains(claimed.Error(), "topic 1") {
		t.Errorf("ErrClaimed = %q", claimed.Error())
	}
	version := &ErrVersion{Path: "/p/state.yaml", Found: "99", Supported: "1"}
	if !strings.Contains(version.Error(), "99") || !strings.Contains(version.Error(), "upgrade hydra") {
		t.Errorf("ErrVersion = %q", version.Error())
	}
	unknown := &ErrUnknown{ID: "nope"}
	if !strings.Contains(unknown.Error(), "nope") {
		t.Errorf("ErrUnknown = %q", unknown.Error())
	}
	cycle := &ErrCycle{ID: "a", Parent: "c"}
	if !strings.Contains(cycle.Error(), "cycle") {
		t.Errorf("ErrCycle = %q", cycle.Error())
	}
}

func TestSetParentClearsContainment(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"parent", "child"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	if err := s.SetParent("child", "parent"); err != nil {
		t.Fatalf("set parent: %v", err)
	}
	if err := s.SetParent("child", ""); err != nil {
		t.Fatalf("clear parent: %v", err)
	}
	child, ok, err := s.Get("child")
	if err != nil || !ok || child.Parent != "" {
		t.Fatalf("child = (%+v, %v, %v), want flat topic", child, ok, err)
	}
}

func TestChildrenReturnsOrderedMatchesOnly(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	for _, id := range []string{"root", "a", "b", "solo"} {
		if err := s.Attach(id, Member{Repo: "api", Branch: id}); err != nil {
			t.Fatalf("attach %s: %v", id, err)
		}
	}
	for _, child := range []string{"a", "b"} {
		if err := s.SetParent(child, "root"); err != nil {
			t.Fatalf("parent %s: %v", child, err)
		}
	}

	kids, err := s.Children("root")
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(kids) != 2 || kids[0].ID != "a" || kids[1].ID != "b" {
		t.Fatalf("children = %+v, want [a b]", kids)
	}
	if empty, err := s.Children("solo"); err != nil || len(empty) != 0 {
		t.Fatalf("solo children = (%v, %v), want empty", empty, err)
	}
}

func TestSetParentReturnsErrCycle(t *testing.T) {
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

	err := s.SetParent("a", "c")
	var cycle *ErrCycle
	if !errors.As(err, &cycle) {
		t.Fatalf("cycle = %v, want ErrCycle", err)
	}
}

func TestAttachValidatesInputs(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if err := s.Attach("", Member{Repo: "api", Branch: "main"}); err == nil {
		t.Fatal("empty topic id must be refused")
	}
	if err := s.Attach("1", Member{Repo: "", Branch: "main"}); err == nil {
		t.Fatal("empty repo must be refused")
	}
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "state.yaml.tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func assertValidStateFile(t *testing.T, root, previous string) {
	t.Helper()
	data, err := os.ReadFile(Path(root))
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("state.yaml must never be empty")
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("state.yaml is not valid yaml: %v\n%s", err, data)
	}
	if doc.Version != SchemaVersion {
		t.Fatalf("version = %q, want %q", doc.Version, SchemaVersion)
	}
	if previous != "" && string(data) != previous && len(doc.Topics) == 0 {
		t.Fatalf("replacement state must not be partial: %q", data)
	}
}

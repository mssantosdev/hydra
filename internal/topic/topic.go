// Package topic records which topic each worktree belongs to.
//
// A topic is a unit of work spanning repositories. Membership is EXPLICIT: it is
// written only by the commands that create or attach worktrees, never inferred
// from a branch name. Deriving membership from branch stems was tried and
// rejected — a stem is a fuzzy query, and using one as a destructive handle means
// `remove --topic api` can match every api-* worktree. Identity must be exact.
//
// The store holds only what git cannot know: which topic a (repo, branch) pair
// belongs to. It never caches worktree lists, branch existence, ahead/behind
// counts or dirty status; those are read from git on every call.
package topic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gofrs/flock"

	"github.com/mssantosdev/hydra/internal/config"
	"gopkg.in/yaml.v3"
)

// SchemaVersion is the on-disk format version of state.yaml.
const SchemaVersion = "1"

const (
	stateName  = "state.yaml"
	lockName   = "state.lock"
	ignoreName = ".gitignore"
)

// lockTimeout bounds how long a writer waits for the lock before reporting a
// retryable busy error.
const lockTimeout = 5 * time.Second

// gitignoreBody keeps local state out of git while leaving the shared manifest
// committable. A blanket "*" would make config.yaml uncommittable, which is the
// whole reason the manifest lives in this directory.
const gitignoreBody = "*\n!.gitignore\n!config.yaml\n"

// Member is one worktree's participation in a topic.
type Member struct {
	Repo   string `yaml:"repo" json:"repo"`
	Branch string `yaml:"branch" json:"branch"`
}

// Topic is a unit of work and the worktrees that make it up.
type Topic struct {
	ID      string   `yaml:"-" json:"id"`
	Members []Member `yaml:"members" json:"members"`

	// Parent is CONTAINMENT: the topic this one integrates into. It is opt-in, and a topic
	// without one is flat — which stays the default, because depth must not tax anyone who did
	// not ask for it.
	//
	// This is a recorded relationship, not one inferred from a name. The refusal in this
	// package's doc is about deriving membership from branch STEMS, which made a fuzzy query
	// into a destructive handle; a declared parent has nothing to do with that.
	Parent string `yaml:"parent,omitempty" json:"parent,omitempty"`

	// Closed records that the work was declared finished. It is stored because closing is an
	// act, where CLOSEABILITY is derived — from whether children are closed and whether their
	// branches reached this topic's, which git answers and a stored flag would lie about after
	// a rebase.
	Closed bool `yaml:"closed,omitempty" json:"closed,omitempty"`
}

// document is the literal shape of state.yaml.
//
// It deliberately does NOT preserve unknown keys: the version gate refuses any
// schema this binary does not know, so an older hydra errors rather than
// rewriting a newer file. Preserving unknown keys would be dead code guarded by
// an unreachable case.
type document struct {
	Version string                 `yaml:"version"`
	Topics  map[string]*topicEntry `yaml:"topics"`
}

type topicEntry struct {
	Members []Member `yaml:"members"`
	Parent  string   `yaml:"parent,omitempty"`
	Closed  bool     `yaml:"closed,omitempty"`
}

// ErrClaimed reports that a worktree already belongs to a different topic.
type ErrClaimed struct {
	Repo      string
	Branch    string
	Existing  string
	Requested string
}

func (e *ErrClaimed) Error() string {
	return fmt.Sprintf("worktree %s@%s already belongs to topic %s", e.Repo, e.Branch, e.Existing)
}

// ErrVersion reports state written by a newer hydra.
type ErrVersion struct {
	Path      string
	Found     string
	Supported string
}

func (e *ErrVersion) Error() string {
	return fmt.Sprintf("%s has schema version %q, but this hydra supports %q; upgrade hydra",
		e.Path, e.Found, e.Supported)
}

// ErrBusy reports that the state lock could not be acquired. It is the only
// retryable failure this package produces.
type ErrBusy struct{ Path string }

func (e *ErrBusy) Error() string {
	return fmt.Sprintf("another hydra process holds the state lock %s; retry", e.Path)
}

// IsBusy reports whether err is lock contention, so callers can map it to the
// retryable `busy` code without depending on this package's error types.
func IsBusy(err error) bool {
	var busy *ErrBusy
	return errors.As(err, &busy)
}

// Dir returns the local state directory for a project root.
func Dir(root string) string { return config.ManifestDir(root) }

// Path returns the state file location for a project root.
func Path(root string) string { return filepath.Join(Dir(root), stateName) }

// LockPath returns the lock file location.
//
// The lock is a SEPARATE file on purpose: writes replace state.yaml via rename,
// which swaps the inode, so a lock held on state.yaml itself would guard a file
// that no longer exists. state.lock is never renamed.
func LockPath(root string) string { return filepath.Join(Dir(root), lockName) }

// Store is the per-project topic store. It holds no open handles: every
// operation opens, locks, reads, and closes, so there is nothing to leak and
// nothing to close.
type Store struct{ root string }

// Open returns a store for a project root. It touches the filesystem only when
// a mutation happens, so a read-only command in a fresh workspace creates
// nothing.
func Open(root string) *Store { return &Store{root: root} }

// Get returns one topic and whether it exists.
func (s *Store) Get(id string) (Topic, bool, error) {
	doc, err := s.read()
	if err != nil {
		return Topic{}, false, err
	}
	entry, ok := doc.Topics[id]
	if !ok || entry == nil || len(entry.Members) == 0 {
		return Topic{}, false, nil
	}
	return topicFrom(id, entry), true, nil
}

// List returns every topic ordered by id.
func (s *Store) List() ([]Topic, error) {
	doc, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]Topic, 0, len(doc.Topics))
	for _, id := range sortedKeys(doc.Topics) {
		entry := doc.Topics[id]
		if entry == nil || len(entry.Members) == 0 {
			continue
		}
		out = append(out, topicFrom(id, entry))
	}
	return out, nil
}

// Names returns every topic id in order.
func (s *Store) Names() ([]string, error) {
	doc, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(doc.Topics))
	for _, id := range sortedKeys(doc.Topics) {
		if entry := doc.Topics[id]; entry != nil && len(entry.Members) > 0 {
			out = append(out, id)
		}
	}
	return out, nil
}

// TopicOf returns the topic a worktree belongs to, if any.
func (s *Store) TopicOf(repo, branch string) (string, bool, error) {
	doc, err := s.read()
	if err != nil {
		return "", false, err
	}
	for _, id := range sortedKeys(doc.Topics) {
		entry := doc.Topics[id]
		if entry == nil {
			continue
		}
		for _, m := range entry.Members {
			if m.Repo == repo && m.Branch == branch {
				return id, true, nil
			}
		}
	}
	return "", false, nil
}

// Attach records a worktree as a member of a topic, creating the topic if
// needed. It is idempotent. A worktree already held by a different topic yields
// *ErrClaimed: a worktree belongs to at most one topic, because one checkout
// cannot represent two units of work and N-target deletion would be ambiguous.
func (s *Store) Attach(id string, m Member) error {
	if id == "" {
		return errors.New("topic id is required")
	}
	if m.Repo == "" || m.Branch == "" {
		return errors.New("member repo and branch are required")
	}
	return s.mutate(func(doc *document) error {
		for otherID, entry := range doc.Topics {
			if entry == nil {
				continue
			}
			for _, existing := range entry.Members {
				if existing.Repo != m.Repo || existing.Branch != m.Branch {
					continue
				}
				if otherID == id {
					return nil // already a member; nothing to do
				}
				return &ErrClaimed{Repo: m.Repo, Branch: m.Branch, Existing: otherID, Requested: id}
			}
		}
		entry := doc.Topics[id]
		if entry == nil {
			entry = &topicEntry{}
			doc.Topics[id] = entry
		}
		entry.Members = append(entry.Members, m)
		return nil
	})
}

// Detach removes a worktree from a topic. A topic whose last member leaves is
// deleted in the same write: this garbage collection is the only path that
// removes a topic, so no caller has to delete identity separately.
func (s *Store) Detach(id, repo, branch string) error {
	return s.mutate(func(doc *document) error {
		entry := doc.Topics[id]
		if entry == nil {
			return nil
		}
		kept := entry.Members[:0]
		for _, m := range entry.Members {
			if m.Repo == repo && m.Branch == branch {
				continue
			}
			kept = append(kept, m)
		}
		entry.Members = kept
		if len(entry.Members) == 0 {
			delete(doc.Topics, id)
		}
		return nil
	})
}

// Remove deletes a topic and all of its membership records.
func (s *Store) Remove(id string) error {
	return s.mutate(func(doc *document) error {
		delete(doc.Topics, id)
		return nil
	})
}

// read loads the document without locking. Writes land via rename, so a reader
// always observes one complete version — never a torn file.
func (s *Store) read() (*document, error) {
	path := Path(s.root)
	//nolint:gosec // G304: path derives from the resolved project root, not caller input
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newDocument(), nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	return parse(data, path)
}

func parse(data []byte, path string) (*document, error) {
	doc := newDocument()
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if doc.Version == "" {
		doc.Version = SchemaVersion
	}
	if doc.Version != SchemaVersion {
		return nil, &ErrVersion{Path: path, Found: doc.Version, Supported: SchemaVersion}
	}
	if doc.Topics == nil {
		doc.Topics = map[string]*topicEntry{}
	}
	return doc, nil
}

func newDocument() *document {
	return &document{Version: SchemaVersion, Topics: map[string]*topicEntry{}}
}

// mutate performs a locked read-modify-write. The lock is held across the whole
// sequence, and the new content is written to a temp file in the same directory
// then renamed, which is atomic on POSIX.
func (s *Store) mutate(apply func(*document) error) error {
	dir := Dir(s.root)
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}
	if err := writeIgnore(dir); err != nil {
		return err
	}

	lockPath := LockPath(s.root)
	ctx, cancel := context.WithTimeout(context.Background(), lockTimeout)
	defer cancel()

	lock := flock.New(lockPath)
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("failed to acquire %s: %w", lockPath, err)
	}
	if !locked {
		return &ErrBusy{Path: lockPath}
	}
	defer func() { _ = lock.Unlock() }()

	doc, err := s.read()
	if err != nil {
		return err
	}
	if err := apply(doc); err != nil {
		return err
	}
	return s.writeAtomic(doc)
}

// writeAtomic serialises the document to a temp file in the SAME directory and
// renames it over state.yaml. The caller must already hold the lock.
func (s *Store) writeAtomic(doc *document) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("failed to encode topic state: %w", err)
	}

	dir := Dir(s.root)
	tmp, err := os.CreateTemp(dir, stateName+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp state in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmpName, err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, Path(s.root)); err != nil {
		return fmt.Errorf("failed to replace %s: %w", Path(s.root), err)
	}
	return syncDir(dir)
}

func writeIgnore(dir string) error {
	path := filepath.Join(dir, ignoreName)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(gitignoreBody), 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}
	return nil
}

func sortedKeys(m map[string]*topicEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedMembers(in []Member) []Member {
	out := make([]Member, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Repo != out[j].Repo {
			return out[i].Repo < out[j].Repo
		}
		return out[i].Branch < out[j].Branch
	})
	return out
}

// topicFrom builds the public shape from a stored entry, so every read path reports the same
// fields — a construction that forgets `parent` reads as a flat topic, which is exactly the kind
// of half-applied hierarchy that is hard to see.
func topicFrom(id string, entry *topicEntry) Topic {
	return Topic{
		ID:      id,
		Members: sortedMembers(entry.Members),
		Parent:  entry.Parent,
		Closed:  entry.Closed,
	}
}

// SetParent records containment. An empty parent clears it, making the topic flat again.
//
// A topic cannot be its own ancestor: a cycle would make closeability non-terminating, and every
// walk over the tree would need a visited set to avoid hanging. Refusing the edge that closes the
// cycle keeps every reader simple.
func (s *Store) SetParent(id, parent string) error {
	if id == parent && id != "" {
		return &ErrCycle{ID: id, Parent: parent}
	}
	return s.mutate(func(doc *document) error {
		entry, ok := doc.Topics[id]
		if !ok {
			return &ErrUnknown{ID: id}
		}
		if parent != "" {
			if _, ok := doc.Topics[parent]; !ok {
				return &ErrUnknown{ID: parent}
			}
			// Walk up from the proposed parent: if this topic is already above it, the edge
			// would close a loop.
			for at := parent; at != ""; {
				next, ok := doc.Topics[at]
				if !ok {
					break
				}
				if next.Parent == id {
					return &ErrCycle{ID: id, Parent: parent}
				}
				at = next.Parent
			}
		}
		entry.Parent = parent
		return nil
	})
}

// SetClosed records that a topic's work is finished, or reopens it.
//
// It stores only the declaration. Whether a topic MAY be closed is derived by the caller from
// git and from its children, because a stored answer would be wrong the moment someone rebases.
func (s *Store) SetClosed(id string, closed bool) error {
	return s.mutate(func(doc *document) error {
		entry, ok := doc.Topics[id]
		if !ok {
			return &ErrUnknown{ID: id}
		}
		entry.Closed = closed
		return nil
	})
}

// Children returns the topics whose parent is id, ordered.
func (s *Store) Children(id string) ([]Topic, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Topic
	for _, t := range all {
		if t.Parent == id {
			out = append(out, t)
		}
	}
	return out, nil
}

// ErrUnknown reports a topic id that was never recorded.
type ErrUnknown struct{ ID string }

func (e *ErrUnknown) Error() string { return fmt.Sprintf("topic %s is not recorded", e.ID) }

// ErrCycle reports a parent edge that would make a topic its own ancestor.
type ErrCycle struct{ ID, Parent string }

func (e *ErrCycle) Error() string {
	return fmt.Sprintf("topic %s cannot have %s as its parent: that would close a cycle", e.ID, e.Parent)
}

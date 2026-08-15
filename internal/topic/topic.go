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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gofrs/flock"

	"github.com/mssantosdev/hydra/internal/config"
	"gopkg.in/yaml.v3"
)

// SchemaVersion is the on-disk format version state.yaml is WRITTEN as.
const SchemaVersion = "2"

// supportedVersions are the formats this binary can READ. v1 held containment in a
// scalar `parent:`; v2 holds every relationship in `links:`. A v1 file is normalized
// on read and rewritten as v2 by the next mutation, so the upgrade needs no command.
var supportedVersions = []string{"1", "2"}

// Reserved link kinds — the two hydra assigns meaning to.
//
// Custom kinds are namespaced (they contain a dot) and hydra NEVER gates on them: it
// stores and reports them so plugins, UIs and scripts can build their own semantics on
// the same primitive. Reserving the bare identifiers is what lets hydra add a kind later
// without breaking a workspace that already used that word.
const (
	// KindPartOf is containment: the topic this one integrates into. `topic close`
	// derives closeability from it against git.
	KindPartOf = "part_of"
	// KindDependsOn is a peer dependency. There is no integration branch between peers,
	// so merged-ness is not checkable and is not pretended: the gate is whether the
	// target is CLOSED.
	KindDependsOn = "depends_on"
)

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

// Link is one typed, directed edge from a topic to another topic.
//
// The graph replaced a scalar `parent`, because a unit of work relates to others in more
// than one way: it integrates into an epic, it waits on a peer, and a plugin may assert
// anything else. One edge list with a kind expresses all three; a field per relationship
// would need a schema change per idea.
type Link struct {
	Kind string `yaml:"kind" json:"kind"`
	To   string `yaml:"to" json:"to"`
}

// Topic is a unit of work, the worktrees that make it up, and how it relates to others.
type Topic struct {
	ID      string   `yaml:"-" json:"id"`
	Members []Member `yaml:"members" json:"members"`

	// Links are this topic's OUTGOING edges. Opt-in: a topic with none is flat, which
	// stays the default, because depth must not tax anyone who did not ask for it.
	//
	// These are recorded relationships, never inferred from a name. The refusal in this
	// package's doc is about deriving membership from branch STEMS, which made a fuzzy
	// query into a destructive handle; a declared edge has nothing to do with that.
	Links []Link `yaml:"links,omitempty" json:"links,omitempty"`

	// Meta is user-owned key/value data. hydra assigns it NO meaning, imposes no limits,
	// and never branches on it — it exists so an extension can carry its own state
	// (a tracker id, a UI's grouping, a plugin's cache key) on the topic that owns it
	// instead of in a sidecar file that drifts.
	Meta map[string]string `yaml:"meta,omitempty" json:"meta,omitempty"`

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
	Members []Member          `yaml:"members"`
	Links   []Link            `yaml:"links,omitempty"`
	Meta    map[string]string `yaml:"meta,omitempty"`
	Closed  bool              `yaml:"closed,omitempty"`

	// Parent is a v1 DECODE TARGET and nothing else: parse normalizes it into a
	// part_of link and clears it, and writeAtomic therefore never emits it. It stays
	// declared because removing it would make a v1 file's containment silently vanish
	// on the read that upgrades it — the worst possible way to lose a relationship.
	Parent string `yaml:"parent,omitempty"`
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

// ErrVersion reports state this binary cannot read — in practice, written by a newer hydra.
type ErrVersion struct {
	Path      string
	Found     string
	Supported []string
}

func (e *ErrVersion) Error() string {
	return fmt.Sprintf("%s has schema version %q, but this hydra supports %s; upgrade hydra",
		e.Path, e.Found, strings.Join(quoteAll(e.Supported), ", "))
}

func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = strconv.Quote(s)
	}
	return out
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
			deleteTopic(doc, id)
		}
		return nil
	})
}

// Remove deletes a topic, its membership records, and every edge pointing AT it.
func (s *Store) Remove(id string) error {
	return s.mutate(func(doc *document) error {
		deleteTopic(doc, id)
		return nil
	})
}

// deleteTopic removes a topic and sweeps the inbound edges naming it.
//
// The sweep is not tidiness: leaving them turns every removal into recorded drift that
// only `doctor --fix` can clear, and a dependency gate would then block on a topic that
// no longer exists. Identity and the references to it die in ONE write, under the lock
// the caller already holds.
func deleteTopic(doc *document, id string) {
	delete(doc.Topics, id)
	for _, entry := range doc.Topics {
		if entry == nil || len(entry.Links) == 0 {
			continue
		}
		kept := make([]Link, 0, len(entry.Links))
		for _, l := range entry.Links {
			if l.To == id {
				continue
			}
			kept = append(kept, l)
		}
		if len(kept) == 0 {
			kept = nil
		}
		entry.Links = kept
	}
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
		// A file with no version predates the field; it can only be v1 shaped.
		doc.Version = supportedVersions[0]
	}
	if !isSupportedVersion(doc.Version) {
		return nil, &ErrVersion{Path: path, Found: doc.Version, Supported: supportedVersions}
	}
	if doc.Topics == nil {
		doc.Topics = map[string]*topicEntry{}
	}
	// v1 → v2: containment moves from the scalar into the edge list. Done on READ so
	// every query answers the same way regardless of what is on disk, and the next
	// mutation persists it as v2. Prepended, so a hand-written file that already has
	// both keeps the declared parent first.
	for _, entry := range doc.Topics {
		if entry == nil || entry.Parent == "" {
			continue
		}
		entry.Links = append([]Link{{Kind: KindPartOf, To: entry.Parent}}, entry.Links...)
		entry.Parent = ""
	}
	doc.Version = SchemaVersion
	return doc, nil
}

func isSupportedVersion(v string) bool {
	for _, s := range supportedVersions {
		if s == v {
			return true
		}
	}
	return false
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

func sortedLinks(in []Link) []Link {
	if len(in) == 0 {
		return nil
	}
	out := make([]Link, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].To < out[j].To
	})
	return out
}

func copyMeta(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// topicFrom builds the public shape from a stored entry, so every read path reports the same
// fields — a construction that forgets `links` reads as a flat topic, which is exactly the kind
// of half-applied relationship that is hard to see.
//
// Meta is COPIED: handing out the stored map would let a caller mutate state that only
// passes through the lock, and the escaping alias would make the next write unexplainable.
func topicFrom(id string, entry *topicEntry) Topic {
	return Topic{
		ID:      id,
		Members: sortedMembers(entry.Members),
		Links:   sortedLinks(entry.Links),
		Meta:    copyMeta(entry.Meta),
		Closed:  entry.Closed,
	}
}

// liveEntry resolves an id using the same liveness rule Get and List use: a topic exists
// when it has at least one member. Identity without membership is what the last-member GC
// removes, so no edge may point at one.
func liveEntry(doc *document, id string) (*topicEntry, bool) {
	entry, ok := doc.Topics[id]
	if !ok || entry == nil || len(entry.Members) == 0 {
		return nil, false
	}
	return entry, true
}

func isReservedKind(kind string) bool {
	return kind == KindPartOf || kind == KindDependsOn
}

// linkKindPattern is the shape a CUSTOM kind must take: dot-namespaced, lowercase. Bare
// identifiers are refused because they are the space hydra reserves for kinds it may
// define later — refusing them now is what keeps adding one non-breaking.
var linkKindPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*(\.[a-z0-9][a-z0-9_-]*)+$`)

// ValidateKind reports whether a kind may be recorded.
func ValidateKind(kind string) error {
	if isReservedKind(kind) || linkKindPattern.MatchString(kind) {
		return nil
	}
	return &ErrKind{Kind: kind}
}

// ValidateMetaKey refuses keys that cannot be read back safely. Values are unrestricted:
// meta is the user's own space and hydra assigns it no meaning.
func ValidateMetaKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return &ErrMetaKey{Key: key, Reason: "a meta key cannot be empty"}
	}
	for _, r := range key {
		if unicode.IsControl(r) {
			return &ErrMetaKey{Key: key, Reason: "a meta key cannot contain control characters"}
		}
	}
	return nil
}

// AddLink records one edge, and reports whether it changed anything.
//
// Recording the same edge twice is CONVERGENT: recorded=false at exit 0, not an error,
// because a script that ensures a relationship must be safe to re-run. Both endpoints
// must be live topics.
//
// Two refusals, both overridable with force. A SELF-edge is refused for every kind,
// reserved or custom: "a part_of a" and "a acme.blocks a" are equally degenerate, and no
// vocabulary makes a topic its own relatum. A multi-hop CYCLE is refused only in the
// reserved kinds, whose semantics it would break; a custom kind may be legitimately
// mutual (a plugin's "relates-to" is symmetric by design), and hydra assigns it no
// meaning to protect. Either way force records it: every walk here carries a visited set,
// so a recorded cycle costs mutual close-blocking — breakable with `close --force` — and
// never a hang.
func (s *Store) AddLink(from string, l Link, force bool) (bool, error) {
	if err := ValidateKind(l.Kind); err != nil {
		return false, err
	}
	if from == "" || l.To == "" {
		return false, errors.New("link source and target are required")
	}
	recorded := false
	err := s.mutate(func(doc *document) error {
		recorded = false
		entry, ok := liveEntry(doc, from)
		if !ok {
			return &ErrUnknown{ID: from}
		}
		if _, ok := liveEntry(doc, l.To); !ok {
			return &ErrUnknown{ID: l.To}
		}
		for _, existing := range entry.Links {
			if existing == l {
				return nil
			}
		}
		if !force {
			if from == l.To {
				return &ErrCycle{From: from, Kind: l.Kind, To: l.To, Path: []string{from, l.To}}
			}
			if isReservedKind(l.Kind) {
				if path, closes := cyclePath(doc, from, l); closes {
					return &ErrCycle{From: from, Kind: l.Kind, To: l.To, Path: path}
				}
			}
		}
		entry.Links = append(entry.Links, l)
		recorded = true
		return nil
	})
	return recorded, err
}

// cyclePath reports whether adding l to from closes a loop, and names the loop.
//
// Breadth-first, so the reported path is the SHORTEST one — an error that names a
// twelve-hop walk when a two-hop one exists is an error nobody reads. The visited set is
// what makes this safe against state that is already cyclic.
func cyclePath(doc *document, from string, l Link) ([]string, bool) {
	if from == l.To {
		return []string{from, l.To}, true
	}
	type step struct {
		id   string
		path []string
	}
	visited := map[string]bool{l.To: true}
	queue := []step{{id: l.To, path: []string{from, l.To}}}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		entry, ok := doc.Topics[cur.id]
		if !ok || entry == nil {
			continue
		}
		for _, edge := range entry.Links {
			if edge.Kind != l.Kind {
				continue
			}
			next := make([]string, 0, len(cur.path)+1)
			next = append(append(next, cur.path...), edge.To)
			if edge.To == from {
				return next, true
			}
			if visited[edge.To] {
				continue
			}
			visited[edge.To] = true
			queue = append(queue, step{id: edge.To, path: next})
		}
	}
	return nil, false
}

// RemoveLink deletes one edge. An edge that is not recorded is *ErrLinkUnknown carrying
// what IS recorded: the desired state already holds, but silently succeeding would hide a
// typo in the kind or the target, and there is nothing to converge on.
func (s *Store) RemoveLink(from string, l Link) error {
	return s.mutate(func(doc *document) error {
		entry, ok := liveEntry(doc, from)
		if !ok {
			return &ErrUnknown{ID: from}
		}
		kept := make([]Link, 0, len(entry.Links))
		found := false
		for _, existing := range entry.Links {
			if existing == l && !found {
				found = true
				continue
			}
			kept = append(kept, existing)
		}
		if !found {
			return &ErrLinkUnknown{From: from, Kind: l.Kind, To: l.To, Recorded: sortedLinks(entry.Links)}
		}
		if len(kept) == 0 {
			kept = nil
		}
		entry.Links = kept
		return nil
	})
}

// ReplaceLinks sets a topic's outgoing edges wholesale, for the declarative document path.
func (s *Store) ReplaceLinks(id string, links []Link, force bool) error {
	for _, l := range links {
		if err := ValidateKind(l.Kind); err != nil {
			return err
		}
		if l.To == "" {
			return errors.New("every link needs a target")
		}
	}
	return s.mutate(func(doc *document) error {
		entry, ok := liveEntry(doc, id)
		if !ok {
			return &ErrUnknown{ID: id}
		}
		// Validate against the document as it will be, not as it was: the replacement set
		// is applied to a scratch entry so a cycle WITHIN the new set is caught too.
		saved := entry.Links
		entry.Links = nil
		for _, l := range links {
			if _, ok := liveEntry(doc, l.To); !ok {
				entry.Links = saved
				return &ErrUnknown{ID: l.To}
			}
			if !force {
				if id == l.To {
					entry.Links = saved
					return &ErrCycle{From: id, Kind: l.Kind, To: l.To, Path: []string{id, l.To}}
				}
				if isReservedKind(l.Kind) {
					if path, closes := cyclePath(doc, id, l); closes {
						entry.Links = saved
						return &ErrCycle{From: id, Kind: l.Kind, To: l.To, Path: path}
					}
				}
			}
			duplicate := false
			for _, existing := range entry.Links {
				if existing == l {
					duplicate = true
					break
				}
			}
			if !duplicate {
				entry.Links = append(entry.Links, l)
			}
		}
		return nil
	})
}

// UpdateMeta applies set then unset in one write.
//
// Unsetting an absent key is a NO-OP, deliberately unlike removing an absent link: meta is
// a value bag with no identity to be wrong about, and a strict version would force every
// plugin to read-then-guard for a delete that is already idempotent.
func (s *Store) UpdateMeta(id string, set map[string]string, unset []string) error {
	for key := range set {
		if err := ValidateMetaKey(key); err != nil {
			return err
		}
	}
	for _, key := range unset {
		if err := ValidateMetaKey(key); err != nil {
			return err
		}
	}
	return s.mutate(func(doc *document) error {
		entry, ok := liveEntry(doc, id)
		if !ok {
			return &ErrUnknown{ID: id}
		}
		if len(set) > 0 && entry.Meta == nil {
			entry.Meta = make(map[string]string, len(set))
		}
		for key, value := range set {
			entry.Meta[key] = value
		}
		for _, key := range unset {
			delete(entry.Meta, key)
		}
		if len(entry.Meta) == 0 {
			entry.Meta = nil
		}
		return nil
	})
}

// ReplaceMeta sets a topic's meta wholesale. An empty map clears it.
func (s *Store) ReplaceMeta(id string, meta map[string]string) error {
	for key := range meta {
		if err := ValidateMetaKey(key); err != nil {
			return err
		}
	}
	return s.mutate(func(doc *document) error {
		entry, ok := liveEntry(doc, id)
		if !ok {
			return &ErrUnknown{ID: id}
		}
		entry.Meta = copyMeta(meta)
		return nil
	})
}

// InboundLink is one edge pointing AT a topic.
type InboundLink struct {
	From string `json:"from"`
	Kind string `json:"kind"`
}

// Inbound returns every edge naming id, ordered. Derived on demand and never stored: a
// second copy of the same edge is a second thing that can be wrong.
func (s *Store) Inbound(id string) ([]InboundLink, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []InboundLink
	for _, t := range all {
		for _, l := range t.Links {
			if l.To == id {
				out = append(out, InboundLink{From: t.ID, Kind: l.Kind})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].From != out[j].From {
			return out[i].From < out[j].From
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
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

// Children returns the topics holding a part_of link to id, ordered.
//
// Containment is now one kind among several, and a topic may declare more than one parent:
// each parent gates its own close independently, so nothing here has to choose between them.
func (s *Store) Children(id string) ([]Topic, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Topic
	for _, t := range all {
		for _, l := range t.Links {
			if l.Kind == KindPartOf && l.To == id {
				out = append(out, t)
				break
			}
		}
	}
	return out, nil
}

// ErrUnknown reports a topic id that was never recorded.
type ErrUnknown struct{ ID string }

func (e *ErrUnknown) Error() string { return fmt.Sprintf("topic %s is not recorded", e.ID) }

// ErrCycle reports an edge that would close a loop in a reserved kind.
type ErrCycle struct {
	From, Kind, To string
	// Path is the loop the edge would close, shortest-first, for a message that names it.
	Path []string
}

func (e *ErrCycle) Error() string {
	if e.From == e.To {
		return fmt.Sprintf("topic %s cannot %s itself", e.From, e.Kind)
	}
	return fmt.Sprintf("%s %s %s would close a cycle: %s",
		e.From, e.Kind, e.To, strings.Join(e.Path, " → "))
}

// ErrLinkUnknown reports an edge that was asked to be removed but is not recorded.
type ErrLinkUnknown struct {
	From, Kind, To string
	Recorded       []Link
}

func (e *ErrLinkUnknown) Error() string {
	return fmt.Sprintf("topic %s has no %s link to %s", e.From, e.Kind, e.To)
}

// ErrKind reports a link kind hydra will not record.
type ErrKind struct{ Kind string }

func (e *ErrKind) Error() string {
	return fmt.Sprintf("link kind %q is not valid: use %s, %s, or a namespaced custom kind "+
		"containing a dot (for example acme.tested-by)", e.Kind, KindPartOf, KindDependsOn)
}

// ErrMetaKey reports a meta key that cannot be stored.
type ErrMetaKey struct{ Key, Reason string }

func (e *ErrMetaKey) Error() string { return fmt.Sprintf("meta key %q: %s", e.Key, e.Reason) }

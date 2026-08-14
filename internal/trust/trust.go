// Package trust decides whether hydra may execute what a manifest says.
//
// `.hydra/config.yaml` is designed to be shared: the docs call it the committable half of
// `.hydra/`, `repo restore` rebuilds a workspace from it, and the level model assumes a team
// edits it together. So it arrives on a machine the way any source file does — you pull a
// branch. Without a gate, the next hydra command then executes whatever that branch says, as
// you, with your credentials, with no prompt and no record that the executable content
// changed.
//
// Prior art: git refuses to share hooks at all. direnv shares `.envrc` and gates it on
// approval of the file's content, re-blocking on any change. Both treat this as a trust
// boundary; the gate here is the direnv shape, because hydra's manifest has to be shared for
// the tool to do its job.
package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/mssantosdev/hydra/internal/config"
)

// SchemaVersion is the trust store's own schema, independent of the manifest's.
const SchemaVersion = "1"

// storeName is the file under the global config directory.
//
// Not in the workspace: a manifest cannot certify its own hooks, and a trust record inside
// the repository is a record an attacker can forge in the same commit that adds the hook.
// Not in `projects.yaml` either — that file has a known non-atomic read-modify-write, and
// security state does not live in a file with a race.
const storeName = "trust.yaml"

// Reason says why a workspace is not trusted. All three share one envelope shape at the
// command layer, because a caller forced to special-case three variants of one error will
// special-case two and mishandle the third.
const (
	ReasonNeverTrusted = "never_trusted"
	ReasonChanged      = "changed"
	ReasonMismatch     = "fingerprint_mismatch"
)

// Entry is one approved workspace.
type Entry struct {
	Fingerprint string `yaml:"fingerprint"`
	ApprovedAt  string `yaml:"approved_at"`

	// Entries maps each executable manifest path to a digest of its value at approval
	// time. It exists so a refusal can name exactly WHICH entries changed while revealing
	// none of them: a hook line is where a credential ends up, and the envelope is a
	// logging surface. A digest is also all that is needed — the reviewer reads the real
	// diff in git, which is the point of the gate.
	Entries map[string]string `yaml:"entries,omitempty"`
}

type document struct {
	Version    string           `yaml:"version"`
	Workspaces map[string]Entry `yaml:"workspaces"`
}

// Fingerprint hashes a manifest's executable surface.
//
// Canonical over the surface, NOT over the file, because adding a repository, editing
// base_branch, reformatting or adding a comment must not cost trust. A gate that re-blocks on
// unrelated edits trains people to approve reflexively — the direnv complaint, and worse than
// no gate. Hook ORDER does change execution, so it is part of the rendering via the entry
// path, which carries the index.
func Fingerprint(cfg *config.Config) string {
	surface := config.ExecutableSurface(cfg)
	h := sha256.New()
	for _, entry := range surface {
		// NUL-separated: a path or value containing the separator cannot forge a different
		// surface that hashes the same, which \n or : would allow.
		_, _ = h.Write([]byte(entry.Path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(entry.Value))
		_, _ = h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// entryDigests renders the per-path digests of a manifest's executable surface.
func entryDigests(cfg *config.Config) map[string]string {
	surface := config.ExecutableSurface(cfg)
	if len(surface) == 0 {
		return nil
	}
	out := make(map[string]string, len(surface))
	for _, e := range surface {
		sum := sha256.Sum256([]byte(e.Value))
		// Truncated: this only has to detect change, and a short digest keeps the store
		// readable, which matters for a security control you are meant to be able to cat.
		out[e.Path] = hex.EncodeToString(sum[:])[:16]
	}
	return out
}

// Path returns the trust store location for a config directory.
func Path(configDir string) string { return filepath.Join(configDir, storeName) }

// ErrUnsafeStore is a trust store hydra refuses to honour.
//
// Refusing is the whole point: if anything that can write the config directory can also edit
// the store, the gate is forgeable and the containment work it sits on top of is bypassed.
type ErrUnsafeStore struct {
	Path   string
	Reason string
}

func (e *ErrUnsafeStore) Error() string {
	return fmt.Sprintf("refusing to use trust store %s: %s", e.Path, e.Reason)
}

// Load reads the store, returning an empty document when the file is absent.
//
// An absent store is not an error: trust is ABSENT BY DEFAULT, so a machine that has never
// approved anything is the normal starting state and every workspace with hooks is inert
// until someone says otherwise.
func Load(configDir string) (map[string]Entry, error) {
	path := Path(configDir)
	if err := checkSafe(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // the trust store path is hydra's own, checked above
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]Entry{}, nil
		}
		return nil, fmt.Errorf("failed to read trust store: %w", err)
	}
	var doc document
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse trust store %s: %w", path, err)
	}
	if doc.Version != "" && doc.Version != SchemaVersion {
		return nil, &ErrUnsafeStore{
			Path:   path,
			Reason: fmt.Sprintf("schema version %q is not %q", doc.Version, SchemaVersion),
		}
	}
	if doc.Workspaces == nil {
		doc.Workspaces = map[string]Entry{}
	}
	return doc.Workspaces, nil
}

// checkSafe refuses a store that something other than its owner could have written, and
// refuses a symlink outright: honouring one would let anything able to create a link in the
// config directory point trust at a file it controls.
func checkSafe(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return checkParent(filepath.Dir(path))
		}
		return fmt.Errorf("failed to stat trust store: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return &ErrUnsafeStore{Path: path, Reason: "it is a symlink"}
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return &ErrUnsafeStore{
			Path:   path,
			Reason: fmt.Sprintf("mode %04o is group- or world-writable; chmod 600 it", perm),
		}
	}
	return checkParent(filepath.Dir(path))
}

func checkParent(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // created on first write, with the right mode
		}
		return fmt.Errorf("failed to stat %s: %w", dir, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return &ErrUnsafeStore{Path: dir, Reason: "the config directory is a symlink"}
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return &ErrUnsafeStore{
			Path:   dir,
			Reason: fmt.Sprintf("directory mode %04o is group- or world-writable; chmod 700 it", perm),
		}
	}
	return nil
}

// Status is the answer the gate needs.
type Status struct {
	Trusted     bool
	Reason      string // one of the Reason constants when not trusted
	Fingerprint string // the fingerprint observed NOW
	Approved    string // the fingerprint on record, empty when never trusted
	ApprovedAt  string
}

// Check reports whether a workspace may execute its manifest's content.
func Check(configDir, workspace string, cfg *config.Config) (Status, error) {
	observed := Fingerprint(cfg)
	status := Status{Fingerprint: observed}

	entries, err := Load(configDir)
	if err != nil {
		return status, err
	}
	entry, ok := entries[key(workspace)]
	if !ok {
		status.Reason = ReasonNeverTrusted
		return status, nil
	}
	status.Approved = entry.Fingerprint
	status.ApprovedAt = entry.ApprovedAt
	if entry.Fingerprint != observed {
		status.Reason = ReasonChanged
		return status, nil
	}
	status.Trusted = true
	return status, nil
}

// Approve records a workspace's current fingerprint.
//
// When expected is non-empty it must equal what is observed, and nothing is written on a
// mismatch. That is the unattended path: the expected value lives in CI configuration, a file
// the team controls, rather than in the repository being checked out — so a hostile branch
// cannot approve itself by editing its own pinned hash.
func Approve(configDir, workspace string, cfg *config.Config, expected string) (Status, error) {
	status, err := Check(configDir, workspace, cfg)
	if err != nil {
		return status, err
	}
	if expected != "" && expected != status.Fingerprint {
		status.Trusted = false
		status.Reason = ReasonMismatch
		return status, nil
	}
	entries, err := Load(configDir)
	if err != nil {
		return status, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entries[key(workspace)] = Entry{
		Fingerprint: status.Fingerprint,
		ApprovedAt:  now,
		Entries:     entryDigests(cfg),
	}
	if err := save(configDir, entries); err != nil {
		return status, err
	}
	status.Trusted = true
	status.Reason = ""
	status.Approved = status.Fingerprint
	status.ApprovedAt = now
	return status, nil
}

// Revoke removes a workspace, reporting whether it was there.
func Revoke(configDir, workspace string) (bool, error) {
	entries, err := Load(configDir)
	if err != nil {
		return false, err
	}
	k := key(workspace)
	if _, ok := entries[k]; !ok {
		return false, nil
	}
	delete(entries, k)
	return true, save(configDir, entries)
}

// Prune drops entries whose workspace no longer exists, returning them sorted.
//
// `hydra prune` already exists for exactly this kind of staleness, so this needs no command
// of its own.
func Prune(configDir string) ([]string, error) {
	entries, err := Load(configDir)
	if err != nil {
		return nil, err
	}
	var gone []string
	for path := range entries {
		if _, statErr := os.Stat(filepath.Join(path, config.StateDir, config.ManifestName)); statErr != nil {
			gone = append(gone, path)
			delete(entries, path)
		}
	}
	if len(gone) == 0 {
		return nil, nil
	}
	sort.Strings(gone)
	return gone, save(configDir, entries)
}

// save writes the store atomically, 0600 inside a 0700 directory.
func save(configDir string, entries map[string]Entry) error {
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := yaml.Marshal(document{Version: SchemaVersion, Workspaces: entries})
	if err != nil {
		return fmt.Errorf("failed to marshal trust store: %w", err)
	}
	path := Path(configDir)
	tmp, err := os.CreateTemp(configDir, storeName+".*")
	if err != nil {
		return fmt.Errorf("failed to create temp trust store: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("failed to replace %s: %w", path, err)
	}
	return nil
}

// key normalises a workspace path. Trust is keyed by absolute path, so moving a workspace
// loses it — the directory approved is not the directory that now exists. direnv behaves the
// same way.
func key(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return filepath.Clean(workspace)
	}
	return filepath.Clean(abs)
}

// ChangedPaths names the manifest paths whose executable value differs from what was
// approved, WITHOUT revealing any value.
//
// A trust refusal must not echo hook text: the envelope is a logging surface and a hook line
// is where a credential ends up. The paths are enough to review the real diff in git, which is
// what the gate exists to make you do.
func ChangedPaths(cfg *config.Config, approved Entry) []string {
	was := approved.Entries
	now := entryDigests(cfg)

	var changed []string
	for path, digest := range now {
		if prev, ok := was[path]; !ok || prev != digest {
			changed = append(changed, path)
		}
	}
	for path := range was {
		if _, ok := now[path]; !ok {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

// AsUnsafe extracts an ErrUnsafeStore from an error chain.
func AsUnsafe(err error) (*ErrUnsafeStore, bool) {
	var unsafe *ErrUnsafeStore
	if errors.As(err, &unsafe) {
		return unsafe, true
	}
	return nil, false
}

// NormalizeExpected accepts a pinned fingerprint with or without the algorithm prefix, so a
// CI variable copied from either `hydra trust --show` or a bare hash both work.
func NormalizeExpected(expected string) string {
	expected = strings.TrimSpace(expected)
	if expected == "" || strings.HasPrefix(expected, "sha256:") {
		return expected
	}
	return "sha256:" + expected
}

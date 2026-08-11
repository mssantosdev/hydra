package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// manifestLockName sits beside the manifest inside .hydra/.
//
// It is a SEPARATE file from the manifest, because Update writes by renaming a temp file
// over config.yaml. That swaps the inode, so a lock held on the manifest itself would
// guard a file that no longer exists.
const manifestLockName = "config.lock"

// manifestLockTimeout bounds the wait before reporting contention. Registering a
// repository is quick once the clone is done, so a caller that waits this long is behind
// something stuck rather than merely behind someone else.
const manifestLockTimeout = 10 * time.Second

// ManifestLockPath returns the manifest lock location for a workspace root.
func ManifestLockPath(root string) string {
	return filepath.Join(ManifestDir(root), manifestLockName)
}

// ErrManifestBusy reports that another process holds the manifest lock.
type ErrManifestBusy struct{ Path string }

func (e *ErrManifestBusy) Error() string {
	return fmt.Sprintf("another hydra is writing the manifest (%s)", e.Path)
}

// Update applies a mutation to the manifest under a lock, re-reading it from disk first.
//
// Save marshals a caller's in-memory copy over the whole file. Concurrent read-modify-write
// without a lock lets each writer overwrite another's entries from a stale snapshot; slow
// operations (e.g. cloning) widen that window.
//
// The mutation receives the manifest as it is on disk RIGHT NOW, not as the caller last
// saw it. A caller that needs to reject a conflicting state must therefore check inside
// the mutation, where the check and the write are atomic with respect to other processes.
func Update(root string, mutate func(*Config) error) error {
	dir := ManifestDir(root)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create %s: %w", dir, err)
	}

	lockPath := ManifestLockPath(root)
	ctx, cancel := context.WithTimeout(context.Background(), manifestLockTimeout)
	defer cancel()

	lock := flock.New(lockPath)
	locked, err := lock.TryLockContext(ctx, 20*time.Millisecond)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("failed to acquire %s: %w", lockPath, err)
	}
	if !locked {
		return &ErrManifestBusy{Path: lockPath}
	}
	defer func() { _ = lock.Unlock() }()

	path := ManifestPath(root)
	current, err := Load(path)
	if err != nil {
		return err
	}
	if err := mutate(current); err != nil {
		return err
	}
	return current.Save(path)
}

package config_test

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
)

// Pins that concurrent Update registrations all survive. Unlocked Save would let the last
// writer overwrite earlier entries from a stale snapshot.
func TestUpdateMergesConcurrentRegistrations(t *testing.T) {
	root := t.TempDir()
	seedManifest(t, root)

	const writers = 8
	var wg sync.WaitGroup
	errs := make([]error, writers)

	for i := range writers {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			errs[n] = config.Update(root, func(live *config.Config) error {
				live.SetRepo("g", aliasFor(n), config.Repo{
					Remote:        "git@example.com:acme/" + aliasFor(n) + ".git",
					DefaultBranch: "main",
				})
				return nil
			})
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("writer %d: %v", i, err)
		}
	}

	// Every registration from every writer must appear after reload.
	reloaded, err := config.Load(config.ManifestPath(root))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	for i := range writers {
		if _, ok := reloaded.Groups["g"].Repos[aliasFor(i)]; !ok {
			t.Errorf("%s is missing: a concurrent write erased it", aliasFor(i))
		}
	}
}

// The mutation must see the manifest as it is on disk, not as the caller last loaded it —
// that is the whole point of taking the lock before reading.
func TestUpdateSeesWritesItDidNotMake(t *testing.T) {
	root := t.TempDir()
	seedManifest(t, root)

	if err := config.Update(root, func(live *config.Config) error {
		live.SetRepo("g", "first", config.Repo{DefaultBranch: "main"})
		return nil
	}); err != nil {
		t.Fatalf("first update: %v", err)
	}

	var sawFirst bool
	if err := config.Update(root, func(live *config.Config) error {
		_, sawFirst = live.Groups["g"].Repos["first"]
		return nil
	}); err != nil {
		t.Fatalf("second update: %v", err)
	}
	if !sawFirst {
		t.Error("the mutation was handed a stale manifest")
	}
}

// A mutation that fails must not write, so a caller can reject a conflicting state inside
// the lock and leave the manifest untouched.
func TestUpdateDoesNotWriteWhenTheMutationFails(t *testing.T) {
	root := t.TempDir()
	seedManifest(t, root)

	before, err := os.ReadFile(config.ManifestPath(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	wantErr := os.ErrInvalid
	if err := config.Update(root, func(live *config.Config) error {
		live.SetRepo("g", "doomed", config.Repo{DefaultBranch: "main"})
		return wantErr
	}); err == nil {
		t.Fatal("expected the mutation's error to surface")
	}

	after, err := os.ReadFile(config.ManifestPath(root))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(before) != string(after) {
		t.Error("a failed mutation still wrote to the manifest")
	}
}

// The lock is a separate file because Update replaces the manifest, swapping its inode: a
// lock held on the manifest itself would guard a file that no longer exists.
func TestManifestLockIsNotTheManifest(t *testing.T) {
	root := t.TempDir()
	if got, other := config.ManifestLockPath(root), config.ManifestPath(root); got == other {
		t.Fatalf("lock path %q must differ from the manifest", got)
	}
	if dir := filepath.Dir(config.ManifestLockPath(root)); dir != config.ManifestDir(root) {
		t.Errorf("lock lives in %q, want it beside the manifest", dir)
	}
}

func seedManifest(t *testing.T, root string) {
	t.Helper()
	cfg := &config.Config{Version: config.SchemaVersion, Project: "t"}
	if err := cfg.Save(config.ManifestPath(root)); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// aliasFor names a writer without an integer-to-rune conversion, which gosec flags as a
// possible overflow.
func aliasFor(n int) string {
	return "repo-" + strconv.Itoa(n)
}

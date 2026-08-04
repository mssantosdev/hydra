//go:build !windows

package topic

import (
	"fmt"
	"os"
)

// syncDir fsyncs the directory so the rename that replaces state.yaml survives a
// crash. Directories cannot be opened for sync on Windows, hence the build split.
func syncDir(dir string) error {
	f, err := os.Open(dir) //nolint:gosec // G304: dir derives from the resolved project root
	if err != nil {
		return fmt.Errorf("failed to open %s for sync: %w", dir, err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("failed to sync %s: %w", dir, err)
	}
	return nil
}

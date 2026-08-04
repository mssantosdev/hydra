//go:build windows

package topic

// syncDir is a no-op on Windows: directory handles cannot be opened for sync.
// The rename itself is the durability boundary there.
func syncDir(string) error { return nil }

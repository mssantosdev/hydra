package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `paths.bare_dir` and each group's `path:` are documented as project-relative, and both are joined
// to the project root to decide where hydra creates, checks out and REMOVES directories.
// filepath.Join RESOLVES `..` rather than rejecting it, so an unchecked value escapes silently — and
// a manifest is a shared, committed file, which makes "check out this branch" a way to write
// anywhere the user can write.

func manifestAt(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".hydra", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	//nolint:gosec // G306: fixture inside this test's own t.TempDir()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func TestLoadRefusesPathsThatLeaveTheWorkspace(t *testing.T) {
	for _, tc := range []struct{ name, body, field string }{
		{
			"group path escapes",
			"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: ../../../../tmp/pwned\n",
			"groups.backend.path",
		},
		{
			"group path is exactly ..",
			"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: ..\n",
			"groups.backend.path",
		},
		{
			"group path is absolute",
			"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: /etc\n",
			"groups.backend.path",
		},
		{
			"bare_dir escapes",
			"version: \"3\"\nproject: p\npaths:\n  bare_dir: ../../outside\n",
			"paths.bare_dir",
		},
		{
			"bare_dir is absolute",
			"version: \"3\"\nproject: p\npaths:\n  bare_dir: /var/tmp/bare\n",
			"paths.bare_dir",
		},
		{
			"escape hidden behind a real segment",
			"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: ok/../../../out\n",
			"groups.backend.path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(manifestAt(t, tc.body))
			var unsafe *ErrUnsafePath
			if !errors.As(err, &unsafe) {
				t.Fatalf("Load accepted a path that leaves the workspace: err = %v", err)
			}
			if unsafe.Field != tc.field {
				t.Errorf("field = %q, want %q", unsafe.Field, tc.field)
			}
			if !strings.Contains(unsafe.Error(), "leaves the workspace") {
				t.Errorf("message does not say what is wrong: %q", unsafe.Error())
			}
		})
	}
}

// Relative paths that stay inside are the documented, supported case and must keep working —
// including one whose `..` is cancelled by an earlier segment.
func TestLoadAcceptsContainedPaths(t *testing.T) {
	for _, body := range []string{
		"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: services/backend\n",
		"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: services/../backend\n",
		"version: \"3\"\nproject: p\ngroups:\n  backend:\n    path: .\n",
		"version: \"3\"\nproject: p\npaths:\n  bare_dir: .git-bare/./repos\n",
	} {
		if _, err := Load(manifestAt(t, body)); err != nil {
			t.Errorf("a contained path was refused: %v\n%s", err, body)
		}
	}
}

// Save refuses one too: hydra must never author a manifest it would then refuse to read, which
// would leave a workspace only a hand edit could recover.
func TestSaveRefusesAnEscapingPath(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Project: "p",
		Paths:   Paths{BareDir: ".bare"},
		Groups:  map[string]Group{"backend": {Path: "../../escape"}},
	}
	err := c.Save(filepath.Join(t.TempDir(), "config.yaml"))
	var unsafe *ErrUnsafePath
	if !errors.As(err, &unsafe) {
		t.Fatalf("Save wrote a manifest it would refuse to read: err = %v", err)
	}
}

package carry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestApply_CarriesFromSourceWorktreeAndWorkspace(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, ".env"), "DB=dev\n")
	write(t, filepath.Join(root, ".shared", "ca.pem"), "CERT\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{
		{Path: ".env"},
		{From: ".shared/ca.pem", To: "certs/ca.pem"},
	}, Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})

	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if got, err := os.ReadFile(filepath.Join(dst, ".env")); err != nil || string(got) != "DB=dev\n" {
		t.Errorf(".env not carried: %q %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "certs", "ca.pem")); err != nil || string(got) != "CERT\n" {
		t.Errorf("workspace file not carried: %q %v", got, err)
	}
	for _, r := range results {
		if r.Disposition != Placed {
			t.Errorf("%s: disposition = %q, want %q", r.Dest, r.Disposition, Placed)
		}
	}
}

// Carrying is convergent, and it must never overwrite: a file already in the worktree was
// either carried by an earlier run or written by someone since, and clobbering the second
// would destroy work nobody asked us to touch.
func TestApply_NeverOverwritesAnExistingFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, ".env"), "DB=dev\n")
	write(t, filepath.Join(dst, ".env"), "DB=mine\n")

	results, _ := Apply([]config.CarryEntry{{Path: ".env"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})

	if got, _ := os.ReadFile(filepath.Join(dst, ".env")); string(got) != "DB=mine\n" {
		t.Errorf("an existing file was overwritten: %q", got)
	}
	if len(results) != 1 || results[0].Disposition != Skipped {
		t.Errorf("results = %+v, want one skipped", results)
	}
}

// A fresh machine has no source worktree, which is the honest limit of replaying a manifest:
// structure, not secrets. It must warn and name what is missing, never fail the creation —
// failing would make repo restore unusable on exactly the machine that needs it.
func TestApply_MissingSourceWarnsAndNeverFails(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "svc", "api")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{{Path: ".env"}},
		Plan{WorktreePath: dst, WorkspaceRoot: root})

	if len(results) != 1 || results[0].Disposition != Missing {
		t.Fatalf("results = %+v, want one missing", results)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one naming the file", warnings)
	}
	if results[0].Reason == "" {
		t.Error("a missing carry must say why")
	}
}

// A manifest is designed to be handed between people, so "copy John's manifest" must not be
// able to mean "write outside my worktree on the next add". CarryEntry.validate rejects these
// when a manifest is parsed; Apply re-checks because Apply is what writes, and filepath.Join
// cleans `..` away silently.
func TestApply_RefusesPathsThatEscape(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, ".env"), "DB=dev\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	outside := filepath.Join(root, "escaped")

	// Constructed directly, bypassing the manifest parser exactly as a future programmatic
	// caller or a bug in validate would.
	results, warnings := Apply([]config.CarryEntry{
		{Path: ".env", To: "../../escaped"},
	}, Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})

	if _, err := os.Lstat(outside); err == nil {
		t.Fatal("a carry entry wrote outside the worktree")
	}
	if len(results) != 1 || results[0].Disposition != Missing {
		t.Fatalf("results = %+v, want one refused", results)
	}
	if len(warnings) != 1 {
		t.Errorf("an escape attempt must warn, got %v", warnings)
	}
}

func TestWithin(t *testing.T) {
	// A sibling whose name merely starts with the base must not count as inside it, which a
	// string prefix check would get wrong.
	if within("/tmp/ws", "/tmp/ws-evil/x") {
		t.Error("/tmp/ws-evil is not inside /tmp/ws")
	}
	if !within("/tmp/ws", "/tmp/ws/a/b") {
		t.Error("/tmp/ws/a/b is inside /tmp/ws")
	}
	if within("/tmp/ws", "/tmp") {
		t.Error("the parent is not inside the base")
	}
	if within("", "/tmp/x") {
		t.Error("an empty base can never contain anything")
	}
}

func TestApply_LinkModePlacesASymlink(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "svc", "api")
	write(t, filepath.Join(root, ".shared", "ca.pem"), "CERT\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, warnings := Apply([]config.CarryEntry{
		{From: ".shared/ca.pem", To: "certs/ca.pem", Mode: config.CarryLink},
	}, Plan{WorktreePath: dst, WorkspaceRoot: root}); len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}

	info, err := os.Lstat(filepath.Join(dst, "certs", "ca.pem"))
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("mode: link must place a symlink, not a copy")
	}
	// And it resolves, which a bad relative target would not.
	if got, err := os.ReadFile(filepath.Join(dst, "certs", "ca.pem")); err != nil || string(got) != "CERT\n" {
		t.Errorf("symlink does not resolve: %q %v", got, err)
	}
}

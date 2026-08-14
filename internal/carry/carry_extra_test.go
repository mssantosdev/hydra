package carry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

func TestApply_EmptyEntriesIsANoOp(t *testing.T) {
	results, warnings := Apply(nil, Plan{WorktreePath: t.TempDir()})
	if results != nil || warnings != nil {
		t.Fatalf("empty carry = (%v, %v), want (nil, nil)", results, warnings)
	}
}

func TestApply_CopyModeIsTheDefault(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, ".env"), "DB=dev\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{{Path: ".env"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(results) != 1 || results[0].Mode != config.CarryCopy {
		t.Fatalf("results = %+v, want copy mode", results)
	}
	info, err := os.Lstat(filepath.Join(dst, ".env"))
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("default mode must copy, not link")
	}
}

func TestApply_CreatesNestedDestinationParents(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, ".env"), "DB=dev\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{
		{Path: ".env", To: "config/nested/.env"},
	}, Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(results) != 1 || results[0].Disposition != Placed {
		t.Fatalf("results = %+v, want placed", results)
	}
	got, err := os.ReadFile(filepath.Join(dst, "config", "nested", ".env"))
	if err != nil || string(got) != "DB=dev\n" {
		t.Errorf("nested dest not carried: %q %v", got, err)
	}
}

func TestApply_CarriesDirectorySource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	write(t, filepath.Join(src, "config", "app.yml"), "key: val\n")
	write(t, filepath.Join(src, "config", "nested", "deep.txt"), "deep\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{{Path: "config"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})
	if len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}
	if len(results) != 1 || results[0].Disposition != Placed {
		t.Fatalf("results = %+v, want placed directory", results)
	}
	for _, rel := range []string{"config/app.yml", "config/nested/deep.txt"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s not carried: %v", rel, err)
		}
	}
}

func TestApply_CarriedDirectoryPreservesInnerSymlinksWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	inside := filepath.Join(src, "bundle", "real")
	write(t, filepath.Join(inside, "secret"), "INSIDE\n")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(inside, filepath.Join(src, "bundle", "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if _, warnings := Apply([]config.CarryEntry{{Path: "bundle"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root}); len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}

	link := filepath.Join(dst, "bundle", "link")
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat carried symlink: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("inner symlink must be copied as a link, not followed")
	}
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if filepath.Base(target) != "real" {
		t.Errorf("link target = %q, want basename real", target)
	}
}

func TestApply_PreservesSourcePermissions(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	secret := filepath.Join(src, ".secret")
	if err := os.MkdirAll(filepath.Dir(secret), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(secret, []byte("x"), 0o400); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if _, warnings := Apply([]config.CarryEntry{{Path: ".secret"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root}); len(warnings) != 0 {
		t.Fatalf("warnings: %v", warnings)
	}

	info, err := os.Stat(filepath.Join(dst, ".secret"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o400 {
		t.Errorf("mode = %o, want 0400", info.Mode().Perm())
	}
}

func TestApply_MissingWorkspaceRootIsANoteNotAFailure(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply([]config.CarryEntry{{From: ".shared/ca.pem", To: "certs/ca.pem"}},
		Plan{WorktreePath: dst})

	if len(results) != 1 || results[0].Disposition != Missing {
		t.Fatalf("results = %+v, want missing", results)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one note", warnings)
	}
	if warnings[0].Severity != output.SeverityNote {
		t.Errorf("missing workspace source must be a note, got severity %q", warnings[0].Severity)
	}
}

func TestApply_MissingWorkspaceFileNamesThePath(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "wt")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, _ := Apply([]config.CarryEntry{{From: ".shared/missing.pem", To: "certs/missing.pem"}},
		Plan{WorktreePath: dst, WorkspaceRoot: root})

	if len(results) != 1 || results[0].Disposition != Missing {
		t.Fatalf("results = %+v, want missing", results)
	}
	if !strings.Contains(results[0].Reason, ".shared/missing.pem") {
		t.Errorf("reason = %q, want it to name the missing path", results[0].Reason)
	}
}

func TestApply_MissingSourceInWorktreeIsANote(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "svc", "api")
	dst := filepath.Join(root, "svc", "api-stage")
	if err := os.MkdirAll(src, 0o750); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}

	_, warnings := Apply([]config.CarryEntry{{Path: ".env"}},
		Plan{WorktreePath: dst, SourceWorktree: src, WorkspaceRoot: root})
	if len(warnings) != 1 || warnings[0].Severity != output.SeverityNote {
		t.Fatalf("warnings = %v, want one note for absent source", warnings)
	}
}

func TestApply_InvalidWorktreePathWarns(t *testing.T) {
	root := t.TempDir()
	missing := filepath.Join(root, "does-not-exist")

	results, warnings := Apply([]config.CarryEntry{{Path: ".env"}},
		Plan{WorktreePath: missing, SourceWorktree: root, WorkspaceRoot: root})

	if results != nil {
		t.Fatalf("results = %v, want nil when worktree cannot be opened", results)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one internal warning", warnings)
	}
}

func TestRelWithinRejectsEscapes(t *testing.T) {
	for _, rel := range []string{"/abs", "..", "../x"} {
		if relWithin(rel) {
			t.Errorf("relWithin(%q) = true, want false", rel)
		}
	}
	if !relWithin("config/.env") {
		t.Error("config/.env should stay inside the worktree")
	}
}

func TestSummary(t *testing.T) {
	if got := Summary(nil); got != "" {
		t.Errorf("empty results = %q, want empty summary", got)
	}
	got := Summary([]Result{
		{Disposition: Placed},
		{Disposition: Placed},
		{Disposition: Skipped},
		{Disposition: Missing},
	})
	want := "2 carried, 1 already present, 1 missing"
	if got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
	if single := Summary([]Result{{Disposition: Placed}}); single != "1 carried" {
		t.Errorf("single placed = %q, want %q", single, "1 carried")
	}
}

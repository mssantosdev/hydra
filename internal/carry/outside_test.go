package carry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

// outsideFixture returns a worktree to fill and a store OUTSIDE any workspace, with one file.
func outsideFixture(t *testing.T) (dst, storeFile string) {
	t.Helper()
	dst = filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	store := t.TempDir()
	storeFile = filepath.Join(store, "mcp.json")
	if err := os.WriteFile(storeFile, []byte(`{"mcp":true}`), 0o600); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	return dst, storeFile
}

// An absolute source is the explicit spelling of "outside" and works when the caller says the
// manifest is trusted. The bytes must be the store's — this is the machine-local store case
// that motivated the whole change.
func TestApply_OutsideSourcePlacedWhenAllowed(t *testing.T) {
	dst, storeFile := outsideFixture(t)

	results, warnings := Apply(
		[]config.CarryEntry{{From: storeFile, To: ".mcp.json"}},
		Plan{WorktreePath: dst, WorkspaceRoot: t.TempDir(), OutsideAllowed: true},
	)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	if len(results) != 1 || results[0].Disposition != Placed {
		t.Fatalf("results = %+v, want placed", results)
	}
	got, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil || string(got) != `{"mcp":true}` {
		t.Fatalf("placed bytes = (%q, %v), want the store's", got, err)
	}
}

// mode: link keeps one editable copy in the store. The link's target must be the resolved
// absolute source — a link is the pointer form, so pointing anywhere else defeats it.
func TestApply_OutsideLinkTargetsTheStore(t *testing.T) {
	dst, storeFile := outsideFixture(t)

	_, warnings := Apply(
		[]config.CarryEntry{{From: storeFile, To: ".mcp.json", Mode: config.CarryLink}},
		Plan{WorktreePath: dst, WorkspaceRoot: t.TempDir(), OutsideAllowed: true},
	)
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}
	target, err := os.Readlink(filepath.Join(dst, ".mcp.json"))
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if target != storeFile {
		t.Errorf("link target = %q, want %q", target, storeFile)
	}
	// And a normal consumer reads through it.
	got, err := os.ReadFile(filepath.Join(dst, ".mcp.json"))
	if err != nil || string(got) != `{"mcp":true}` {
		t.Fatalf("read through link = (%q, %v)", got, err)
	}
}

// Without trust the entry is SKIPPED with the code that names the recovery — never silently,
// and never by running anything. Inside entries still carry: the refusal is scoped to the
// authority the manifest lacks, not to carry as a whole.
func TestApply_OutsideSourceRefusedWithoutTrust(t *testing.T) {
	dst, storeFile := outsideFixture(t)
	root := t.TempDir()
	insideSrc := filepath.Join(root, "inside.txt")
	if err := os.WriteFile(insideSrc, []byte("in"), 0o600); err != nil {
		t.Fatalf("seed inside: %v", err)
	}

	results, warnings := Apply(
		[]config.CarryEntry{
			{From: storeFile, To: ".mcp.json"},
			{From: "inside.txt", To: "inside.txt"},
		},
		Plan{WorktreePath: dst, WorkspaceRoot: root, OutsideAllowed: false},
	)

	if len(results) != 2 || results[0].Disposition != Missing || results[1].Disposition != Placed {
		t.Fatalf("results = %+v, want [missing placed]", results)
	}
	if _, err := os.Stat(filepath.Join(dst, ".mcp.json")); err == nil {
		t.Fatal("the outside file was placed without trust")
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one", warnings)
	}
	w := warnings[0]
	if w.Code != output.CodeManifestUntrusted || !w.IsFault() {
		t.Errorf("warning = (%q, fault=%v), want a manifest_untrusted fault", w.Code, w.IsFault())
	}
	if !strings.Contains(w.Message, "hydra trust") {
		t.Errorf("the refusal must name its recovery, got %q", w.Message)
	}
}

// ~/ expands against this machine's home — the portable spelling of a machine-local store,
// because /home/<name>/… breaks for every teammate with a different name.
func TestApply_HomeExpansion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".arvia"), 0o700); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".arvia", "ca.pem"), []byte("CA"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply(
		[]config.CarryEntry{{From: "~/.arvia/ca.pem", To: "certs/ca.pem"}},
		Plan{WorktreePath: dst, WorkspaceRoot: t.TempDir(), OutsideAllowed: true},
	)
	if len(warnings) != 0 || len(results) != 1 || results[0].Disposition != Placed {
		t.Fatalf("results = %+v warnings = %v", results, warnings)
	}
	if got, _ := os.ReadFile(filepath.Join(dst, "certs/ca.pem")); string(got) != "CA" {
		t.Errorf("placed = %q, want CA", got)
	}
	// The resolved source is reported absolute, so a caller can see where bytes came from.
	if !filepath.IsAbs(results[0].From) || !strings.HasPrefix(results[0].From, home) {
		t.Errorf("result.From = %q, want under %q", results[0].From, home)
	}
}

// A trusted-but-absent outside file is carry_refused: the machine does not have what the
// manifest declared, and the message names the declared form so the operator knows what to
// provide where.
func TestApply_MissingOutsideFileIsRefused(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	results, warnings := Apply(
		[]config.CarryEntry{{From: "~/.arvia/never-there.pem", To: "x.pem"}},
		Plan{WorktreePath: dst, WorkspaceRoot: t.TempDir(), OutsideAllowed: true},
	)
	if len(results) != 1 || results[0].Disposition != Missing {
		t.Fatalf("results = %+v, want missing", results)
	}
	if len(warnings) != 1 || warnings[0].Code != output.CodeCarryRefused {
		t.Fatalf("warnings = %v, want one carry_refused", warnings)
	}
	if !strings.Contains(warnings[0].Message, "~/.arvia/never-there.pem") {
		t.Errorf("the warning must name the declared source, got %q", warnings[0].Message)
	}
}

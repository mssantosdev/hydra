package trust

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/mssantosdev/hydra/internal/config"
)

func load(t *testing.T, body string) *config.Config {
	t.Helper()
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("fixture does not parse: %v\n%s", err, body)
	}
	return &cfg
}

const withHook = `
version: "3"
project: p
groups:
  backend:
    repos:
      api:
        remote: git@h:o/api.git
hooks:
  post_add:
    - run: npm install
`

// The fingerprint must ignore everything that cannot execute. A gate that re-blocks on an
// unrelated edit trains people to approve reflexively, which is worse than no gate.
func TestFingerprintIgnoresChangesThatCannotExecute(t *testing.T) {
	base := Fingerprint(load(t, withHook))

	unchanged := []struct{ name, body string }{
		{"a comment", strings.Replace(withHook, "hooks:", "# a note\nhooks:", 1)},
		{"a new repository", strings.Replace(withHook,
			"        remote: git@h:o/api.git",
			"        remote: git@h:o/api.git\n      web:\n        remote: git@h:o/web.git", 1)},
		{"a base_branch", strings.Replace(withHook, "hooks:", "defaults:\n  base_branch: main\nhooks:", 1)},
		{"a branch_pattern, which substitutes and never executes", strings.Replace(withHook,
			"hooks:", "defaults:\n  branch_provider: \"feat/{slug}\"\nhooks:", 1)},
	}
	for _, tc := range unchanged {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fingerprint(load(t, tc.body)); got != base {
				t.Errorf("%s cost trust; the fingerprint must cover the executable surface only", tc.name)
			}
		})
	}
}

func TestFingerprintChangesOnAnythingExecutable(t *testing.T) {
	base := Fingerprint(load(t, withHook))

	changed := []struct{ name, body string }{
		{"editing a hook", strings.Replace(withHook, "npm install", "npm install --force", 1)},
		{"adding a hook", strings.Replace(withHook,
			"    - run: npm install", "    - run: npm install\n    - run: make deps", 1)},
		{"reordering hooks", strings.Replace(withHook,
			"    - run: npm install", "    - run: make deps\n    - run: npm install", 1)},
		{"a hook at group level", strings.Replace(withHook,
			"  backend:", "  backend:\n    hooks:\n      post_add:\n        - run: ./setup", 1)},
		{"a hook at repo level", strings.Replace(withHook,
			"        remote: git@h:o/api.git",
			"        remote: git@h:o/api.git\n        hooks:\n          post_add:\n            - run: ./setup", 1)},
		{"a runnable branch_provider", strings.Replace(withHook,
			"hooks:", "defaults:\n  branch_provider:\n    run: ./scripts/name\nhooks:", 1)},
	}
	for _, tc := range changed {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fingerprint(load(t, tc.body)); got == base {
				t.Errorf("%s did not change the fingerprint, so the gate would not re-block", tc.name)
			}
		})
	}
}

// Reordering two hooks must change the fingerprint even though the SET is identical, because
// order changes what runs first.
func TestFingerprintIsOrderSensitive(t *testing.T) {
	a := load(t, strings.Replace(withHook, "    - run: npm install",
		"    - run: a\n    - run: b", 1))
	b := load(t, strings.Replace(withHook, "    - run: npm install",
		"    - run: b\n    - run: a", 1))
	if Fingerprint(a) == Fingerprint(b) {
		t.Error("swapping two hooks left the fingerprint unchanged")
	}
}

func TestApproveThenCheckIsTrusted(t *testing.T) {
	dir := t.TempDir()
	cfg := load(t, withHook)
	ws := filepath.Join(dir, "ws")

	status, err := Check(dir, ws, cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Trusted || status.Reason != ReasonNeverTrusted {
		t.Fatalf("a fresh machine must be untrusted: %+v", status)
	}

	if _, err := Approve(dir, ws, cfg, ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	status, err = Check(dir, ws, cfg)
	if err != nil {
		t.Fatalf("Check after approve: %v", err)
	}
	if !status.Trusted {
		t.Fatalf("approved workspace is not trusted: %+v", status)
	}
}

func TestChangedHookGoesStaleAndNamesOnlyThePath(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	if _, err := Approve(dir, ws, load(t, withHook), ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	edited := load(t, strings.Replace(withHook, "npm install", "./post-to-forge --token SECRET", 1))
	status, err := Check(dir, ws, edited)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Trusted || status.Reason != ReasonChanged {
		t.Fatalf("an edited hook must go stale: %+v", status)
	}

	entries, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	changed := ChangedPaths(edited, entries[filepath.Clean(ws)])
	if len(changed) != 1 || changed[0] != "hooks.post_add[0]" {
		t.Fatalf("changed = %v, want exactly the manifest path", changed)
	}
	// The stored record and the reported diff must never carry the hook's text: the envelope
	// is a logging surface and a hook line is where a credential ends up.
	raw, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	for _, secret := range []string{"SECRET", "npm install", "post-to-forge"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the trust store contains %q; it must record digests, not hook text", secret)
		}
		if strings.Contains(strings.Join(changed, " "), secret) {
			t.Errorf("the changed-path list contains %q", secret)
		}
	}
}

// The pin is the unattended path, and it must not write on a mismatch — otherwise CI approves
// whatever it was handed, which is not a gate.
func TestApproveWithAWrongPinWritesNothing(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	cfg := load(t, withHook)

	status, err := Approve(dir, ws, cfg, "sha256:deadbeef")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if status.Trusted || status.Reason != ReasonMismatch {
		t.Fatalf("a wrong pin must refuse: %+v", status)
	}
	if _, statErr := os.Stat(Path(dir)); statErr == nil {
		t.Error("a refused pin wrote a trust store")
	}
	after, err := Check(dir, ws, cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if after.Trusted {
		t.Error("workspace became trusted despite a refused pin")
	}
}

func TestApproveWithTheRightPinSucceeds(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	cfg := load(t, withHook)

	status, err := Approve(dir, ws, cfg, Fingerprint(cfg))
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !status.Trusted {
		t.Fatalf("a matching pin must approve: %+v", status)
	}
}

func TestNormalizeExpectedAcceptsABareHash(t *testing.T) {
	want := "sha256:abc"
	for _, in := range []string{"abc", "sha256:abc", "  abc  "} {
		if got := NormalizeExpected(in); got != want {
			t.Errorf("NormalizeExpected(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NormalizeExpected(""); got != "" {
		t.Errorf("empty must stay empty, got %q", got)
	}
}

// A store anything else can write is a forgeable gate, which would bypass the containment work
// it sits on top of. Refusing is the only safe answer.
func TestStoreIsRefusedWhenNotOwnerOnly(t *testing.T) {
	tests := []struct {
		name string
		mode os.FileMode
		want bool
	}{
		{"owner only", 0o600, false},
		{"group writable", 0o620, true},
		{"world writable", 0o606, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			ws := filepath.Join(dir, "ws")
			if _, err := Approve(dir, ws, load(t, withHook), ""); err != nil {
				t.Fatalf("Approve: %v", err)
			}
			if err := os.Chmod(Path(dir), tt.mode); err != nil {
				t.Fatalf("chmod: %v", err)
			}
			_, err := Load(dir)
			unsafe, isUnsafe := AsUnsafe(err)
			if tt.want && !isUnsafe {
				t.Fatalf("mode %04o was honoured; want refusal, got err=%v", tt.mode, err)
			}
			if !tt.want && err != nil {
				t.Fatalf("mode %04o refused: %v", tt.mode, err)
			}
			if tt.want && !strings.Contains(unsafe.Reason, "writable") {
				t.Errorf("reason %q does not say what is wrong", unsafe.Reason)
			}
		})
	}
}

func TestStoreIsRefusedWhenItIsASymlink(t *testing.T) {
	dir := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "attacker.yaml")
	if err := os.WriteFile(elsewhere, []byte("version: \"1\"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(elsewhere, Path(dir)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, ok := AsUnsafe(mustErr(Load(dir))); !ok {
		t.Error("a symlinked trust store was honoured")
	}
}

func mustErr[T any](_ T, err error) error { return err }

func TestPruneDropsEntriesWhoseWorkspaceIsGone(t *testing.T) {
	dir := t.TempDir()
	alive := t.TempDir()
	if err := os.MkdirAll(filepath.Join(alive, config.StateDir), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(alive, config.StateDir, config.ManifestName),
		[]byte("version: \"3\"\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	gone := filepath.Join(t.TempDir(), "deleted")

	cfg := load(t, withHook)
	if _, err := Approve(dir, alive, cfg, ""); err != nil {
		t.Fatalf("approve alive: %v", err)
	}
	if _, err := Approve(dir, gone, cfg, ""); err != nil {
		t.Fatalf("approve gone: %v", err)
	}

	pruned, err := Prune(dir)
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != filepath.Clean(gone) {
		t.Fatalf("pruned = %v, want just the deleted workspace", pruned)
	}
	entries, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := entries[filepath.Clean(alive)]; !ok {
		t.Error("Prune removed a live workspace")
	}
}

func TestRevokeMakesAWorkspaceUntrustedAgain(t *testing.T) {
	dir := t.TempDir()
	ws := filepath.Join(dir, "ws")
	cfg := load(t, withHook)
	if _, err := Approve(dir, ws, cfg, ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	removed, err := Revoke(dir, ws)
	if err != nil || !removed {
		t.Fatalf("Revoke = %v, %v; want true, nil", removed, err)
	}
	status, err := Check(dir, ws, cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Trusted || status.Reason != ReasonNeverTrusted {
		t.Fatalf("revoked workspace is still trusted: %+v", status)
	}
	// Revoking twice is not an error, it is a no-op that says so.
	removed, err = Revoke(dir, ws)
	if err != nil || removed {
		t.Fatalf("second Revoke = %v, %v; want false, nil", removed, err)
	}
}

// Trust is keyed by absolute path, so moving a workspace loses it. direnv behaves the same
// way, and the alternative — following a workspace by name — would let a directory inherit an
// approval it never earned.
func TestMovingAWorkspaceLosesTrust(t *testing.T) {
	dir := t.TempDir()
	cfg := load(t, withHook)
	if _, err := Approve(dir, filepath.Join(dir, "before"), cfg, ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	status, err := Check(dir, filepath.Join(dir, "after"), cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if status.Trusted {
		t.Error("a different directory inherited another's approval")
	}
}

func TestStoreIsWrittenOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	if _, err := Approve(dir, filepath.Join(dir, "ws"), load(t, withHook), ""); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("store mode = %04o, want 0600: security state must not be readable by others", perm)
	}
}

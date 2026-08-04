package registry

import (
	"github.com/mssantosdev/hydra/internal/config"
	"os"
	"path/filepath"
	"testing"
)

// sandbox points the registry at a temp config dir so tests never touch the real
// user config.
func sandbox(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", filepath.Join(dir, "config"))
	return dir
}

func TestLoadMissingRegistryIsEmptyNotAnError(t *testing.T) {
	sandbox(t)

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load on a fresh machine must not fail: %v", err)
	}
	if len(reg.Projects) != 0 {
		t.Errorf("Projects = %v, want empty", reg.Projects)
	}
	if reg.Version != SchemaVersion {
		t.Errorf("Version = %q, want %q", reg.Version, SchemaVersion)
	}
}

func TestAddSaveResolveRoundTrip(t *testing.T) {
	root := sandbox(t)

	if err := Register("demo", root); err != nil {
		t.Fatalf("Register: %v", err)
	}

	reg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, ok := reg.Resolve("demo")
	if !ok {
		t.Fatal("Resolve(demo) not found after Register")
	}
	abs, _ := filepath.Abs(root)
	if got != abs {
		t.Errorf("Resolve(demo) = %q, want the absolute root %q", got, abs)
	}
	if _, ok := reg.Resolve("nope"); ok {
		t.Error("Resolve must report unknown names as missing")
	}
}

func TestAddIsIdempotentButRefusesConflictingRoots(t *testing.T) {
	root := sandbox(t)

	reg := &Registry{Projects: map[string]string{}}
	if err := reg.Add("demo", root); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := reg.Add("demo", root); err != nil {
		t.Fatalf("re-adding the same root must be a no-op, got: %v", err)
	}
	if err := reg.Add("demo", filepath.Join(root, "elsewhere")); err == nil {
		t.Error("Add must refuse a different root under an existing name")
	}
	if err := reg.Add("", root); err == nil {
		t.Error("Add must refuse an empty project name")
	}
}

func TestRemoveUnknownIsAnError(t *testing.T) {
	sandbox(t)

	reg := &Registry{Projects: map[string]string{"demo": "/ws"}}
	if err := reg.Remove("demo"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := reg.Remove("demo"); err == nil {
		t.Error("removing an already-removed project must fail loudly")
	}
}

// Prune drops entries whose root no longer holds a .hydra.yaml, which is what
// `hydra doctor` reports as registry_dangling.
func TestPruneDropsDanglingRoots(t *testing.T) {
	base := sandbox(t)

	alive := filepath.Join(base, "alive")
	dead := filepath.Join(base, "dead")
	for _, dir := range []string{alive, dead} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}
	if err := os.MkdirAll(config.ManifestDir(alive), 0o750); err != nil {
		t.Fatalf("mkdir hydra dir: %v", err)
	}
	if err := os.WriteFile(config.ManifestPath(alive), []byte("version: \"2\"\n"), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	reg := &Registry{Projects: map[string]string{"alive": alive, "dead": dead}}
	removed := reg.Prune()

	if len(removed) != 1 || removed[0] != "dead" {
		t.Fatalf("Prune removed %v, want [dead]", removed)
	}
	if _, ok := reg.Resolve("alive"); !ok {
		t.Error("Prune must keep a project whose root still has a .hydra.yaml")
	}
	if _, ok := reg.Resolve("dead"); ok {
		t.Error("Prune must drop the dangling project")
	}
}

func TestNamesIsSorted(t *testing.T) {
	reg := &Registry{Projects: map[string]string{"zeta": "/z", "alpha": "/a", "mid": "/m"}}

	names := reg.Names()
	want := []string{"alpha", "mid", "zeta"}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", names, want)
		}
	}
}

func TestPathHonorsConfigDirOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HYDRA_CONFIG_DIR", dir)

	if got, want := Path(), filepath.Join(dir, "projects.yaml"); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

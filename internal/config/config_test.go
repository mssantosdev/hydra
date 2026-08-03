package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := filepath.Join(dir, ".hydra.yaml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// Load must refuse anything but schema v2: there is deliberately no compat layer,
// and a silently-accepted v1 file would be read with the wrong shape.
func TestLoadRejectsForeignVersions(t *testing.T) {
	for _, body := range []string{
		"version: \"1.0\"\nproject: old\n",
		"project: no-version\n",
		"version: \"3\"\nproject: future\n",
	} {
		path := writeConfig(t, t.TempDir(), body)

		_, err := Load(path)
		if err == nil {
			t.Fatalf("Load(%q) succeeded, want a version rejection", body)
		}
		var unsupported *ErrUnsupportedVersion
		if !errors.As(err, &unsupported) {
			t.Fatalf("Load(%q) error = %T (%v), want *ErrUnsupportedVersion", body, err, err)
		}
		if unsupported.Path != path {
			t.Errorf("error names %q, want %q", unsupported.Path, path)
		}
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	dir := t.TempDir()
	path := writeConfig(t, dir, "version: \"2\"\n")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Paths.BareDir != ".bare" {
		t.Errorf("BareDir = %q, want .bare", cfg.Paths.BareDir)
	}
	if cfg.Groups == nil {
		t.Error("Groups must never be nil after Load")
	}
	if cfg.Project != filepath.Base(dir) {
		t.Errorf("Project = %q, want the workspace directory name %q", cfg.Project, filepath.Base(dir))
	}
	// The base branch deliberately defaults to empty so resolution falls through
	// to the repo's default_branch and then origin/HEAD.
	if cfg.Defaults.BaseBranch != "" {
		t.Errorf("Defaults.BaseBranch = %q, want empty", cfg.Defaults.BaseBranch)
	}
}

func TestDefaultConfigRoundTrips(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultConfig("demo")
	cfg.SetRepo("backend", "api", Repo{Remote: "git@example.com:acme/api.git", DefaultBranch: "prod"})
	cfg.Hooks.PostAdd = []Hook{{Run: "bun install", Optional: true}}
	cfg.Defaults.BaseBranch = "prod"

	path := filepath.Join(dir, ".hydra.yaml")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Project != "demo" {
		t.Errorf("Project = %q, want demo", loaded.Project)
	}
	ref, ok := loaded.FindRepo("api")
	if !ok {
		t.Fatal("FindRepo(api) not found after round trip")
	}
	if ref.Group != "backend" || ref.Repo.DefaultBranch != "prod" {
		t.Errorf("repo = %+v, want group backend and default_branch prod", ref)
	}
	if len(loaded.Hooks.PostAdd) != 1 || !loaded.Hooks.PostAdd[0].Optional {
		t.Errorf("PostAdd = %+v, want one optional hook", loaded.Hooks.PostAdd)
	}
}

// The alias is the single source of the bare path: a separate repo name is what
// made every command fail with "bare repository not found" when the two differed.
func TestBarePathUsesAlias(t *testing.T) {
	cfg := DefaultConfig("demo")
	cfg.SetRepo("backend", "api", Repo{Remote: "r"})

	want := filepath.Join("/ws", ".bare", "api.git")
	if got := cfg.BarePath("/ws", "api"); got != want {
		t.Errorf("BarePath = %q, want %q", got, want)
	}
}

func TestReposIsOrderedAndRemoveDropsEmptyGroups(t *testing.T) {
	cfg := DefaultConfig("demo")
	cfg.SetRepo("frontend", "web", Repo{Remote: "w"})
	cfg.SetRepo("backend", "api", Repo{Remote: "a"})
	cfg.SetRepo("backend", "worker", Repo{Remote: "k"})

	var got []string
	for _, ref := range cfg.Repos() {
		got = append(got, ref.Group+"/"+ref.Alias)
	}
	want := []string{"backend/api", "backend/worker", "frontend/web"}
	if len(got) != len(want) {
		t.Fatalf("Repos() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Repos() = %v, want %v", got, want)
		}
	}

	cfg.RemoveRepo("frontend", "web")
	if _, ok := cfg.Groups["frontend"]; ok {
		t.Error("an emptied group must be dropped, not left behind")
	}
	cfg.RemoveRepo("backend", "api")
	if _, ok := cfg.Groups["backend"]; !ok {
		t.Error("a group with remaining repos must survive")
	}
}

func TestFindConfigWalksUp(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "version: \"2\"\nproject: demo\n")

	deep := filepath.Join(root, "backend", "api", "src")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	path, cfg, err := FindConfig(deep)
	if err != nil {
		t.Fatalf("FindConfig: %v", err)
	}
	if path != filepath.Join(root, ".hydra.yaml") {
		t.Errorf("FindConfig found %q, want the root config", path)
	}
	if cfg.Project != "demo" {
		t.Errorf("Project = %q, want demo", cfg.Project)
	}

	if _, _, err := FindConfig(t.TempDir()); err == nil {
		t.Error("FindConfig must fail when no .hydra.yaml exists anywhere above")
	}
}

func TestHooksForCoversEveryEvent(t *testing.T) {
	cfg := DefaultConfig("demo")
	cfg.Hooks.PostClone = []Hook{{Run: "a"}}
	cfg.Hooks.PostAdd = []Hook{{Run: "b"}}
	cfg.Hooks.PreRemove = []Hook{{Run: "c"}}
	cfg.Hooks.PostRemove = []Hook{{Run: "d"}}
	cfg.Hooks.PostSync = []Hook{{Run: "e"}}

	events := HookEvents()
	if len(events) != 5 {
		t.Fatalf("HookEvents() = %v, want 5 events", events)
	}
	for _, event := range events {
		chain, known := cfg.HooksFor(event)
		if !known {
			t.Errorf("HooksFor(%q) reported unknown, but it is in HookEvents()", event)
		}
		if len(chain) != 1 {
			t.Errorf("HooksFor(%q) = %v, want one hook", event, chain)
		}
	}
	if _, known := cfg.HooksFor("pre_add"); known {
		t.Error("HooksFor must reject an unknown event name")
	}
}

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func writeConfig(t *testing.T, dir, body string) string {
	t.Helper()

	path := writeManifest(t, dir, body)
	return path
}

// Load must refuse anything but schema v2: there is deliberately no compat layer,
// and a silently-accepted v1 file would be read with the wrong shape.
func TestLoadRejectsForeignVersions(t *testing.T) {
	for _, body := range []string{
		"version: \"1.0\"\nproject: old\n",
		"project: no-version\n",
		// Newer than this binary. Version 2 is the one exception, and only because the 2 → 3
		// change renests groups without changing what any field means.
		"version: \"4\"\nproject: future\n",
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

	path := ManifestPath(dir)
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
	if path != ManifestPath(root) {
		t.Errorf("FindConfig found %q, want the root config", path)
	}
	if cfg.Project != "demo" {
		t.Errorf("Project = %q, want demo", cfg.Project)
	}

	if _, _, err := FindConfig(t.TempDir()); err == nil {
		t.Error("FindConfig must fail when no .hydra/config.yaml exists anywhere above")
	}
}

func TestHooksForCoversEveryEvent(t *testing.T) {
	// One hook per event, so the loop below proves HooksFor reaches ALL of them. A fixture that
	// populates only some is how a newly added event ends up with no accessor and nobody notices.
	cfg := DefaultConfig("demo")
	cfg.Hooks.PostClone = []Hook{{Run: "a"}}
	cfg.Hooks.PostAdd = []Hook{{Run: "b"}}
	cfg.Hooks.PreRemove = []Hook{{Run: "c"}}
	cfg.Hooks.PostRemove = []Hook{{Run: "d"}}
	cfg.Hooks.PostSync = []Hook{{Run: "e"}}
	cfg.Hooks.PostTopicStart = []Hook{{Run: "f"}}
	cfg.Hooks.PreTopicClose = []Hook{{Run: "g"}}
	cfg.Hooks.PostTopicClose = []Hook{{Run: "h"}}
	cfg.Hooks.PreTopicRemove = []Hook{{Run: "i"}}

	events := HookEvents()
	// Asserting COVERAGE rather than a count: the point is that no event name can exist without a
	// chain behind it, and a hardcoded number just has to be edited every time one is added.
	if len(events) == 0 {
		t.Fatal("HookEvents() is empty")
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

// writeManifest creates .hydra/ and writes a manifest, mirroring what Save does.
func writeManifest(t *testing.T, root, body string) string {
	t.Helper()
	if err := os.MkdirAll(ManifestDir(root), 0o750); err != nil {
		t.Fatalf("mkdir hydra dir: %v", err)
	}
	path := ManifestPath(root)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// yaml.v3 puts the offending line in its message text. hydra had that number at every
// "invalid manifest" error and discarded it, so the parser's real output is the fixture
// here rather than a hand-written string that could drift from what yaml actually emits.
func TestMalformedManifestCarriesTheLine(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{
			name: "syntax error names its line",
			body: "version: \"3\"\nproject: p\ngroups\n  backend: {}\n",
			want: 3,
		},
		{
			name: "type error names the field's line",
			body: "version: \"3\"\nproject: p\ngroups:\n  backend: [not, a, mapping]\n",
			want: 4,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			parseErr := yaml.Unmarshal([]byte(tt.body), &cfg)
			if parseErr == nil {
				t.Fatalf("body was expected to fail parsing:\n%s", tt.body)
			}
			e := &ErrMalformed{Path: "/ws/.hydra/config.yaml", Err: parseErr}
			if got := e.Line(); got != tt.want {
				t.Errorf("Line() = %d, want %d (yaml said %q)", got, tt.want, parseErr)
			}
			if !strings.Contains(e.Error(), "/ws/.hydra/config.yaml") {
				t.Errorf("Error() omits the path: %s", e.Error())
			}
		})
	}
}

// A manifest that parses must not be reported as malformed.
func TestMalformedLineIsZeroWhenTheParserNamedNone(t *testing.T) {
	e := &ErrMalformed{Path: "p", Err: errors.New("something with no line in it")}
	if got := e.Line(); got != 0 {
		t.Errorf("Line() = %d, want 0", got)
	}
}

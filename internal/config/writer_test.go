package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Save merges nodes from the file on disk onto a freshly encoded document. These pin the
// boundary of that merge: what it must keep, what it must never resurrect, and the invariant
// that outranks both — the bytes it writes always parse.

func saveInto(t *testing.T, prior string, mutate func(*Config)) (string, *Config) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if prior != "" {
		if err := os.WriteFile(path, []byte(prior), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	var c Config
	if prior != "" {
		if err := yaml.Unmarshal([]byte(prior), &c); err != nil {
			t.Fatalf("seed does not parse: %v", err)
		}
	}
	if mutate != nil {
		mutate(&c)
	}
	if err := c.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var reloaded Config
	if err := yaml.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("Save wrote a manifest that does not parse: %v\n---\n%s", err, data)
	}
	return string(data), &reloaded
}

// An anchor on a modelled key with the alias on an unmodelled one is the case that produced an
// unloadable workspace: the modelled key is re-encoded from the struct, so `&name` vanished while
// the carried `*name` remained.
func TestSaveKeepsAnchorsAliasesStillResolve(t *testing.T) {
	prior := `version: "3"
project: p
paths: &shared
    bare_dir: .bare
groups:
    backend:
        repos:
            api:
                remote: r1
            worker:
                remote: r2
ci:
    inherit: *shared
`
	got, reloaded := saveInto(t, prior, func(c *Config) {
		delete(c.Groups["backend"].Repos, "worker")
	})
	if !strings.Contains(got, "&shared") {
		t.Errorf("anchor was dropped while its alias survived:\n%s", got)
	}
	if _, ok := reloaded.Groups["backend"].Repos["worker"]; ok {
		t.Error("the removed repo came back")
	}
	if !strings.Contains(got, "ci:") {
		t.Errorf("the unmodelled key was dropped:\n%s", got)
	}
}

// When the anchor sits on a node the mutation DELETES, no amount of carrying can keep the alias
// valid. The writer must still never leave an unparseable file, so it falls back to a plain
// marshal and loses the annotation instead of the workspace.
func TestSaveFallsBackRatherThanWriteAnUnparseableManifest(t *testing.T) {
	prior := `version: "3"
project: p
paths:
    bare_dir: .bare
groups:
    backend:
        repos:
            api:
                remote: r1
            worker: &w
                remote: r2
ci:
    like: *w
`
	// saveInto fails the test if the result does not parse, which is the assertion here.
	got, reloaded := saveInto(t, prior, func(c *Config) {
		delete(c.Groups["backend"].Repos, "worker")
	})
	if strings.Contains(got, "*w") {
		t.Errorf("a dangling alias survived into the written file:\n%s", got)
	}
	if _, ok := reloaded.Groups["backend"].Repos["api"]; !ok {
		t.Error("the surviving repo was lost in the fallback")
	}
}

// A header block separated from the first key by a blank line attaches to the document node, not
// to `version`, so a merge that only walks the mapping never sees it.
func TestSaveKeepsTheDocumentHeader(t *testing.T) {
	prior := `# Team manifest
# reviewed in PR #412

version: "3"
project: p
paths:
    bare_dir: .bare
`
	got, _ := saveInto(t, prior, func(c *Config) { c.Project = "renamed" })
	for _, want := range []string{"# Team manifest", "# reviewed in PR #412"} {
		if !strings.Contains(got, want) {
			t.Errorf("document header line %q was dropped:\n%s", want, got)
		}
	}
}

// `omitempty` drops a modelled key whose value is empty, taking its annotation with it. Carrying
// the empty value back keeps the comment and resurrects nothing, because empty and absent decode
// identically.
func TestSaveKeepsCommentsOnEmptyModelledKeys(t *testing.T) {
	prior := `version: "3"
project: p
paths:
    bare_dir: .bare
defaults:
    # Base branch for new worktrees when --from is not passed.
    base_branch: ""
`
	got, reloaded := saveInto(t, prior, func(c *Config) { c.Project = "renamed" })
	if !strings.Contains(got, "# Base branch for new worktrees") {
		t.Errorf("comment on an empty modelled key was dropped:\n%s", got)
	}
	if reloaded.Defaults.BaseBranch != "" {
		t.Errorf("base_branch gained a value: %q", reloaded.Defaults.BaseBranch)
	}
}

// The empty-value carry must not reach map levels. A group is data, so an annotated empty group
// that a removal deleted must stay deleted — childType answers Elem() for any map key, which
// would otherwise make it look modelled and bring it back.
func TestSaveDoesNotResurrectAnAnnotatedEmptyGroup(t *testing.T) {
	prior := `version: "3"
project: p
paths:
    bare_dir: .bare
groups:
    backend:
        repos:
            api:
                remote: r1
    # legacy, keep an eye on this
    infra: {}
`
	got, reloaded := saveInto(t, prior, func(c *Config) { delete(c.Groups, "infra") })
	if _, ok := reloaded.Groups["infra"]; ok {
		t.Errorf("the deleted group came back:\n%s", got)
	}
}

// The shipped example is the worked demonstration of "hydra preserves your comments", so it has
// to survive a write by the binary that makes the claim. Any comment style hydra cannot keep must
// not appear in it.
func TestShippedExampleManifestRoundTripsWithoutLosingComments(t *testing.T) {

	raw, err := os.ReadFile(filepath.Join("..", "..", "hydra.config.yaml.example"))
	if err != nil {
		t.Fatalf("read example: %v", err)
	}
	before := strings.Count(string(raw), "#")
	got, _ := saveInto(t, string(raw), func(c *Config) {
		c.Groups["backend"].Repos["added"] = Repo{Remote: "r"}
	})
	if after := strings.Count(got, "#"); after < before {
		t.Errorf("one write lost %d comment markers from the shipped example.\n"+
			"Move any comment that sits INSIDE a list above its key: list entries have no "+
			"stable anchor, so the merge cannot keep them.\n---\n%s", before-after, got)
	}
}

// An untouched list keeps its annotation: position is an exact match when nothing moved.
func TestSaveKeepsCommentsInAnUnchangedSequence(t *testing.T) {
	prior := `version: "3"
project: p
paths:
    bare_dir: .bare
hooks:
    post_clone:
        - run: npm install # true for every repo
`
	got, _ := saveInto(t, prior, func(c *Config) { c.Project = "renamed" })
	if !strings.Contains(got, "# true for every repo") {
		t.Errorf("comment on an unchanged list entry was dropped:\n%s", got)
	}
}

// A list the mutation CHANGED must lose them instead. Position no longer identifies the entry a
// comment described, and moving it onto a neighbour would make the file assert something false.
func TestSaveDropsCommentsWhenTheSequenceChanges(t *testing.T) {
	prior := `version: "3"
project: p
paths:
    bare_dir: .bare
groups:
    backend:
        repos:
            api:
                remote: r1
                branches: [main] # the only long-lived branch
`
	got, reloaded := saveInto(t, prior, func(c *Config) {
		r := c.Groups["backend"].Repos["api"]
		r.Branches = []string{"main", "stage"}
		c.Groups["backend"].Repos["api"] = r
	})
	if strings.Contains(got, "the only long-lived branch") {
		t.Errorf("a comment survived onto a list it no longer describes:\n%s", got)
	}
	if len(reloaded.Groups["backend"].Repos["api"].Branches) != 2 {
		t.Error("the new branch list did not land")
	}
}

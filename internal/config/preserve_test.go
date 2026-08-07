package config

import (
	"os"
	"strings"
	"testing"
)

// The manifest is documented as the shareable, committable half of .hydra/, so a user is
// expected to annotate it and to put keys in it that a given hydra build may not model.
// Save used to marshal a closed struct over the whole file, which deleted both silently
// at exit 0. These tests pin the three rules that replaced that: comments survive, unknown
// keys survive WITHIN a schema version, and a map deletion is never undone.

func TestSavePreservesComments(t *testing.T) {
	path := writeManifest(t, t.TempDir(), `# Team manifest — reviewed in PR #412
version: "2"
project: shop
paths:
    bare_dir: .bare # do not move
groups:
    svc:
        api:
            # the only repo anyone should add to
            remote: git@example.com:org/api.git
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{
		"# Team manifest — reviewed in PR #412",
		"# do not move",
		"# the only repo anyone should add to",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("comment %q did not survive Save\n--- file ---\n%s", want, got)
		}
	}
}

func TestSavePreservesUnknownKeysWithinSchemaVersion(t *testing.T) {
	path := writeManifest(t, t.TempDir(), `version: "2"
project: shop
paths:
    bare_dir: .bare
groups: {}
owners:
    - platform-team
ci:
    provider: azure
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, want := range []string{"owners:", "platform-team", "ci:", "provider: azure"} {
		if !strings.Contains(string(got), want) {
			t.Errorf("unknown key %q did not survive Save\n--- file ---\n%s", want, got)
		}
	}
}

// A migration that means to DROP a field must not have it written back on the next save,
// so unknown keys are carried only when the file on disk declares the version being
// written. This is the one case where preserving them is wrong.
func TestSaveDropsUnknownKeysAcrossSchemaVersions(t *testing.T) {
	path := writeManifest(t, t.TempDir(), `version: "1"
project: shop
paths:
    bare_dir: .bare
groups: {}
retired_field: gone-in-v2
`)

	// Load refuses an unsupported version, which is the point of the gate — so build the
	// in-memory config directly, as a migration would, and write the current version over
	// the older file.
	cfg := DefaultConfig("shop")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(got), "retired_field") {
		t.Errorf("retired_field was resurrected across a schema version change\n--- file ---\n%s", got)
	}
}

// groups and each group's repo map are exactly what `repo remove` deletes from. Carrying
// "keys the struct does not have" at a MAP level would silently undo the removal, so
// unknown-key preservation is confined to fixed-field structs.
func TestSaveDoesNotResurrectRemovedRepos(t *testing.T) {
	path := writeManifest(t, t.TempDir(), `# keep this comment
version: "2"
project: shop
paths:
    bare_dir: .bare
groups:
    svc:
        api:
            remote: git@example.com:org/api.git
        worker:
            remote: git@example.com:org/worker.git
    infra:
        tf:
            remote: git@example.com:org/tf.git
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	delete(cfg.Groups["svc"], "worker")
	delete(cfg.Groups, "infra")
	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if strings.Contains(string(got), "worker") {
		t.Errorf("removed repo was written back\n--- file ---\n%s", got)
	}
	if strings.Contains(string(got), "infra") {
		t.Errorf("removed group was written back\n--- file ---\n%s", got)
	}
	if !strings.Contains(string(got), "# keep this comment") {
		t.Errorf("comment lost while deleting\n--- file ---\n%s", got)
	}

	// And the survivor is still loadable and intact.
	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if _, ok := reloaded.Groups["svc"]["api"]; !ok {
		t.Error("api did not survive the removal of its sibling")
	}
	if len(reloaded.Groups) != 1 {
		t.Errorf("groups = %d, want 1", len(reloaded.Groups))
	}
}

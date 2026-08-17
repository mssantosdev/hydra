package trust

import (
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
)

// An outside carry source is machine authority like a hook, so it must cost trust exactly the
// way a hook does: gaining one re-blocks, editing one re-blocks, and an INSIDE entry does
// neither — a gate that re-blocks on an edit that cannot reach beyond the workspace trains
// people to approve reflexively.
func TestFingerprintCoversOutsideCarrySources(t *testing.T) {
	base := Fingerprint(load(t, withHook))

	changed := []struct{ name, body string }{
		{"gaining a workspace-level outside source", strings.Replace(withHook,
			"hooks:", "carry:\n  - from: ~/.arvia/mcp.json\n    to: .mcp.json\nhooks:", 1)},
		{"gaining an absolute outside source", strings.Replace(withHook,
			"hooks:", "carry:\n  - from: /srv/store/ca.pem\n    to: certs/ca.pem\nhooks:", 1)},
		{"gaining a repo-level outside source", strings.Replace(withHook,
			"        remote: git@h:o/api.git",
			"        remote: git@h:o/api.git\n        carry:\n          - from: ~/.arvia/api.env\n            to: .env", 1)},
	}
	for _, tc := range changed {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fingerprint(load(t, tc.body)); got == base {
				t.Errorf("%s did not change the fingerprint, so the gate would not re-block", tc.name)
			}
		})
	}

	// Editing an APPROVED outside source re-blocks too: the approval covered a specific path.
	withOutside := strings.Replace(withHook,
		"hooks:", "carry:\n  - from: ~/.arvia/mcp.json\n    to: .mcp.json\nhooks:", 1)
	approved := Fingerprint(load(t, withOutside))
	retargeted := strings.Replace(withOutside, "~/.arvia/mcp.json", "~/.ssh/id_rsa", 1)
	if Fingerprint(load(t, retargeted)) == approved {
		t.Error("retargeting an approved outside source did not change the fingerprint")
	}

	// An INSIDE entry is containment-checked and must not cost trust.
	withInside := strings.Replace(withHook,
		"hooks:", "carry:\n  - from: .shared/ca.pem\n    to: certs/ca.pem\nhooks:", 1)
	if Fingerprint(load(t, withInside)) != base {
		t.Error("an inside carry entry cost trust; containment already covers it")
	}
}

// A manifest whose ONLY notable content is an outside carry source still activates the gate:
// HasExecutableSurface derives from the same list as the fingerprint, so the two cannot disagree.
func TestOutsideCarryAloneActivatesTheGate(t *testing.T) {
	const noHooks = `
version: "3"
project: p
groups:
  backend:
    repos:
      api:
        remote: git@h:o/api.git
carry:
  - from: ~/.arvia/mcp.json
    to: .mcp.json
`
	if !config.HasExecutableSurface(load(t, noHooks)) {
		t.Error("a manifest with an outside carry source must have a surface to approve")
	}
	inside := strings.Replace(noHooks, "~/.arvia/mcp.json", ".shared/mcp.json", 1)
	if config.HasExecutableSurface(load(t, inside)) {
		t.Error("a manifest with only inside entries has nothing to approve")
	}
}

// carryValue mirrors what the surface hashes for one outside entry: source, destination, mode.
func carryValue(from, dest, mode string) string { return from + "\x00" + dest + "\x00" + mode }

// The surface names each entry by its manifest path, index included, at every level resolution
// reads — the paths are what a trust refusal's details.changed shows a human.
func TestSurfaceNamesOutsideCarryPaths(t *testing.T) {
	const allLevels = `
version: "3"
project: p
carry:
  - from: .shared/inside.pem
    to: a.pem
  - from: ~/.arvia/ws.pem
    to: b.pem
groups:
  backend:
    carry:
      - from: /srv/group.pem
        to: c.pem
    repos:
      api:
        remote: git@h:o/api.git
        carry:
          - from: ~/.arvia/repo.pem
            to: d.pem
`
	surface := config.ExecutableSurface(load(t, allLevels))
	got := make(map[string]string, len(surface))
	for _, e := range surface {
		got[e.Path] = e.Value
	}
	want := map[string]string{
		"carry[1]":                          carryValue("~/.arvia/ws.pem", "b.pem", "copy"),
		"groups.backend.carry[0]":           carryValue("/srv/group.pem", "c.pem", "copy"),
		"groups.backend.repos.api.carry[0]": carryValue("~/.arvia/repo.pem", "d.pem", "copy"),
	}
	for path, value := range want {
		if got[path] != value {
			t.Errorf("surface[%q] = %q, want %q (have %v)", path, got[path], value, got)
		}
	}
	if _, present := got["carry[0]"]; present {
		t.Error("the inside entry joined the surface; containment already covers it")
	}
}

// An approved outside entry is approved to do ONE thing: read that source, to that destination, in
// that mode. Retargeting `to:` at a tracked path would publish the secret on the next push, and
// flipping link→copy turns a pointer into committable bytes — destination containment stops
// traversal, not publication, so both must cost trust.
func TestFingerprintCoversOutsideCarryDestinationAndMode(t *testing.T) {
	const withOutside = `
version: "3"
project: p
groups:
  backend:
    repos:
      api:
        remote: git@h:o/api.git
carry:
  - from: ~/.arvia/mcp.json
    to: .mcp.json
    mode: link
`
	approved := Fingerprint(load(t, withOutside))

	for _, tc := range []struct{ name, body string }{
		{"retargeting to: at a tracked path", strings.Replace(withOutside, "to: .mcp.json", "to: config/published.json", 1)},
		{"flipping link to copy", strings.Replace(withOutside, "mode: link", "mode: copy", 1)},
		{"dropping mode, which defaults to copy", strings.Replace(withOutside, "    mode: link\n", "", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if Fingerprint(load(t, tc.body)) == approved {
				t.Errorf("%s did not change the fingerprint; an approved read stayed approved for a different outcome", tc.name)
			}
		})
	}

	// Writing a default that already applied costs nothing: effective values are hashed.
	const noMode = `
version: "3"
project: p
groups:
  backend:
    repos:
      api:
        remote: git@h:o/api.git
carry:
  - from: ~/.arvia/mcp.json
    to: .mcp.json
`
	explicit := strings.Replace(noMode, "    to: .mcp.json", "    to: .mcp.json\n    mode: copy", 1)
	if Fingerprint(load(t, noMode)) != Fingerprint(load(t, explicit)) {
		t.Error("spelling out the default mode cost trust; the surface must hash effective values")
	}
}

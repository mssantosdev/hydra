package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The bare form is the common case, so it must read and write as a filename rather than as a
// single-key object. Round-tripping matters because hydra rewrites this file: a manifest a
// human wrote as `- .env` coming back as `- path: .env` on the next repo add is the same class
// of unasked-for edit as losing a comment.
func TestCarryEntry_BareFormRoundTrips(t *testing.T) {
	var got struct {
		Carry []CarryEntry `yaml:"carry"`
	}
	if err := yaml.Unmarshal([]byte("carry:\n  - .env\n  - .secrets/\n"), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got.Carry) != 2 || got.Carry[0].Path != ".env" {
		t.Fatalf("parsed = %+v", got.Carry)
	}

	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), "- .env") {
		t.Errorf("bare form did not round-trip:\n%s", out)
	}
	if strings.Contains(string(out), "path: .env") {
		t.Errorf("bare form expanded into a mapping:\n%s", out)
	}
}

func TestCarryEntry_MappingForm(t *testing.T) {
	var got struct {
		Carry []CarryEntry `yaml:"carry"`
	}
	body := "carry:\n  - from: .shared/ca.pem\n    to: certs/ca.pem\n    mode: link\n"
	if err := yaml.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	e := got.Carry[0]
	if e.From != ".shared/ca.pem" || e.Dest() != "certs/ca.pem" || e.Mode != CarryLink {
		t.Errorf("parsed = %+v", e)
	}
	if !e.FromWorkspace() {
		t.Error("a from: entry is workspace-sourced and must say so")
	}
}

// Rejected at PARSE time, so a manifest that could write outside a worktree is refused when
// it is read rather than warned about when it is acted on. A manifest is designed to be handed
// between people; this is the boundary that makes that safe.
func TestCarryEntry_RejectsUnsafeAndAmbiguousEntries(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"escaping destination", "carry:\n  - from: a\n    to: ../../etc/passwd\n", "inside the worktree"},
		{"absolute destination", "carry:\n  - from: a\n    to: /etc/cron.d/pwn\n", "inside the worktree"},
		{"escaping source", "carry:\n  - from: ../../../.ssh/id_rsa\n    to: k\n", "inside the workspace"},
		{"absolute source", "carry:\n  - from: /etc/shadow\n    to: k\n", "inside the workspace"},
		{"no source at all", "carry:\n  - mode: link\n", "needs either a path"},
		{"two sources", "carry:\n  - path: .env\n    from: other\n", "cannot have both"},
		{"bare path with to", "carry:\n  - path: .env\n    to: elsewhere\n", "use from/to"},
		{"unknown mode", "carry:\n  - path: .env\n    mode: hardlink\n", "want \"copy\""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got struct {
				Carry []CarryEntry `yaml:"carry"`
			}
			err := yaml.Unmarshal([]byte(tc.body), &got)
			if err == nil {
				t.Fatalf("%s was accepted; parsed %+v", tc.name, got.Carry)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not explain the problem (want %q)", err, tc.want)
			}
		})
	}
}

// Resolution walks levels in order and APPENDS: a workspace carrying a shared certificate and
// a repo carrying its own .env both apply. Replacing would force every repo to restate what it
// inherited, and someone will forget one.
//
// The levels are an ordered slice rather than hardcoded because the middle one does not exist
// yet — a group has nowhere to hold anything until it becomes an object — so inserting it later
// changes neither the append semantics nor this test.
func TestResolveCarry_AppendsWorkspaceThenRepo(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Carry:   []CarryEntry{{From: ".shared/ca.pem", To: "certs/ca.pem"}},
		Groups: map[string]Group{"svc": {Repos: map[string]Repo{
			"api": {Remote: "git@example.com:o/api.git", Carry: []CarryEntry{{Path: ".env"}}},
		}}},
	}

	got := ResolveCarry(c, "api")
	if len(got) != 2 {
		t.Fatalf("resolved = %+v, want workspace then repo", got)
	}
	if got[0].Dest() != "certs/ca.pem" || got[1].Dest() != ".env" {
		t.Errorf("order = %q, %q; nearest level must come last", got[0].Dest(), got[1].Dest())
	}

	// A repo naming the same DESTINATION overrides how the inherited file arrives, without
	// having to suppress it first.
	c.SetRepo("svc", "api", Repo{
		Remote: "git@example.com:o/api.git",
		Carry:  []CarryEntry{{From: ".shared/ca.pem", To: "certs/ca.pem", Mode: CarryLink}},
	})
	got = ResolveCarry(c, "api")
	if len(got) != 1 {
		t.Fatalf("resolved = %+v, want one entry after the override", got)
	}
	if got[0].Mode != CarryLink {
		t.Errorf("the nearer level must win, got mode %q", got[0].Mode)
	}
}

func TestResolveCarry_EmptyWhenNothingDeclared(t *testing.T) {
	c := &Config{Version: SchemaVersion, Groups: map[string]Group{"svc": {Repos: map[string]Repo{"api": {}}}}}
	if got := ResolveCarry(c, "api"); got != nil {
		t.Errorf("resolved = %+v, want nil", got)
	}
	if got := ResolveCarry(nil, "api"); got != nil {
		t.Errorf("a nil config resolved to %+v", got)
	}
}

// RegisterRepo merges remote and default_branch into the existing entry; SetRepo with a fresh
// struct would discard branch_pattern, branch_provider, branches, and carry on re-registration.
func TestRegisterRepo_PreservesEverythingElse(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Groups: map[string]Group{"svc": {Repos: map[string]Repo{
			"api": {
				Remote:         "git@example.com:o/api.git",
				DefaultBranch:  "main",
				BranchPattern:  "{kind}/{slug}",
				BranchProvider: BranchNaming{runnable: &BranchRunnable{Run: "./scripts/branch-name"}},
				Branches:       []string{"main", "stage", "prod"},
				Carry:          []CarryEntry{{Path: ".env"}},
			},
		}}},
	}

	c.RegisterRepo("svc", "api", "git@example.com:o/api.git", "master")

	got := c.Groups["svc"].Repos["api"]
	if got.DefaultBranch != "master" {
		t.Errorf("default branch = %q, want the new value", got.DefaultBranch)
	}
	if got.BranchPattern == "" || got.BranchProvider.IsZero() {
		t.Error("branch_pattern/branch_provider were dropped — the pre-existing bug")
	}
	if len(got.Branches) != 3 {
		t.Errorf("branches = %v, want the declaration intact", got.Branches)
	}
	if len(got.Carry) != 1 {
		t.Errorf("carry = %v, want the declaration intact", got.Carry)
	}

	// An empty value must not blank what a previous fetch resolved.
	c.RegisterRepo("svc", "api", "", "")
	if got := c.Groups["svc"].Repos["api"]; got.DefaultBranch != "master" || got.Remote == "" {
		t.Errorf("an empty re-registration blanked a field: %+v", got)
	}
}

// A version-2 manifest maps a group straight to its repositories. It must keep loading — refusing
// would make every command fail until the user ran a migration for a purely structural change — and
// it must be written back as version 3, so the upgrade lands as a one-line diff in a file that was
// already committed.
func TestLoadUpgradesLegacyGroupShape(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `# Team manifest — reviewed in PR #412
version: "2"
project: shop
paths:
    bare_dir: .bare
groups:
    backend:
        api:
            # the only repo anyone should add to
            remote: git@example.com:org/api.git
            default_branch: main
owners:
    - platform-team
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("a version-2 manifest must still load: %v", err)
	}
	if cfg.Version != SchemaVersion {
		t.Errorf("version = %q, want it normalised to %q", cfg.Version, SchemaVersion)
	}
	ref, ok := cfg.FindRepo("api")
	if !ok {
		t.Fatal("the legacy shape did not renest into Group.Repos")
	}
	if ref.Repo.Remote == "" || ref.Group != "backend" {
		t.Errorf("renested wrongly: %+v", ref)
	}

	if err := cfg.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.Contains(string(got), `version: "3"`) {
		t.Errorf("the upgrade was not written:\n%s", got)
	}
	if !strings.Contains(string(got), "repos:") {
		t.Errorf("the v3 shape was not written:\n%s", got)
	}
	// The upgrade relocates every repo entry, which is exactly when a manifest is most likely to
	// be annotated — so the merge aligns the shapes before comparing rather than losing them.
	for _, want := range []string{
		"# Team manifest — reviewed in PR #412",
		"# the only repo anyone should add to",
		"platform-team",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("%q did not survive the upgrade:\n%s", want, got)
		}
	}

	// And it is idempotent: loading the upgraded file gives the same repo.
	again, err := Load(path)
	if err != nil {
		t.Fatalf("reload after upgrade: %v", err)
	}
	if _, ok := again.FindRepo("api"); !ok {
		t.Error("api did not survive a round trip through version 3")
	}
}

// A group's `path:` places its worktrees. Group names stay one segment; a slash would make the
// selector, completion and rename semantics ambiguous.
func TestGroupDir(t *testing.T) {
	if got := (Group{}).Dir("backend"); got != "backend" {
		t.Errorf("an empty path means the group name, got %q", got)
	}
	if got := (Group{Path: "platform/infra"}).Dir("infra"); got != "platform/infra" {
		t.Errorf("Dir = %q, want the declared path", got)
	}
}

// The chain the model was missing: a group's value beats the workspace's, and a repo's beats both.
func TestResolveDefaults_NearestLevelWins(t *testing.T) {
	c := &Config{
		Version:  SchemaVersion,
		Defaults: Defaults{BaseBranch: "main", BranchPattern: "{slug}"},
		Groups: map[string]Group{
			"backend": {
				Defaults: Defaults{BaseBranch: "develop"},
				Repos: map[string]Repo{
					"api":      {Remote: "r"},
					"monorepo": {Remote: "r", BranchPattern: "{kind}/{slug}"},
				},
			},
		},
	}

	// The group overrides the workspace; the workspace still supplies what the group omits.
	if got := ResolveDefaults(c, "api"); got.BaseBranch != "develop" || got.BranchPattern != "{slug}" {
		t.Errorf("api resolved to %+v", got)
	}
	// The repo overrides the group.
	if got := ResolveDefaults(c, "monorepo"); got.BranchPattern != "{kind}/{slug}" || got.BaseBranch != "develop" {
		t.Errorf("monorepo resolved to %+v", got)
	}
	// An unknown alias falls back to the workspace rather than inventing anything.
	if got := ResolveDefaults(c, "nope"); got.BaseBranch != "main" {
		t.Errorf("unknown alias resolved to %+v", got)
	}
}

package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The level model: workspace, group and repo carry the same keys. `defaults` resolves
// nearest-wins, `hooks` append outward-in, and a repo can spell its policy flat or in a
// `defaults:` block.
//
// These assert the RESOLVERS, not the reporter. `hydra config show` derives its provenance from
// the same chain, so a test that only checks the reporter passes whether or not any command
// obeys it.

func levelFixture() *Config {
	return &Config{
		Version: SchemaVersion,
		Hooks:   Hooks{PostAdd: []Hook{{Run: "ws"}}},
		Groups: map[string]Group{
			"backend": {
				Defaults: Defaults{BaseBranch: "develop"},
				Hooks:    Hooks{PostAdd: []Hook{{Run: "grp"}}},
				Repos: map[string]Repo{
					"api":  {Remote: "r", Hooks: Hooks{PostAdd: []Hook{{Run: "repo"}}}},
					"bare": {Remote: "r"},
				},
			},
		},
	}
}

func TestResolveHooksAppendsWorkspaceThenGroupThenRepo(t *testing.T) {
	got, known := ResolveHooks(levelFixture(), "backend", "api", "post_add")
	if !known {
		t.Fatal("post_add reported unknown")
	}
	want := []string{"ws", "grp", "repo"}
	if len(got) != len(want) {
		t.Fatalf("chain length: got %d %+v, want %d", len(got), got, len(want))
	}
	for i, w := range want {
		if got[i].Run != w {
			t.Errorf("position %d: got %q, want %q", i, got[i].Run, w)
		}
	}
}

// Appending, not overriding, is the documented rule: a workspace-wide `direnv allow` must still
// run for a repo that adds its own toolchain step.
func TestResolveHooksKeepsWorkspaceChainForRepoWithNoneOfItsOwn(t *testing.T) {
	got, _ := ResolveHooks(levelFixture(), "backend", "bare", "post_add")
	if len(got) != 2 || got[0].Run != "ws" || got[1].Run != "grp" {
		t.Fatalf("got %+v, want [ws grp]", got)
	}
}

// Topic events name no repository, so the workspace chain is the whole answer rather than an error.
func TestResolveHooksWithNoRepoInScopeReturnsWorkspaceChain(t *testing.T) {
	got, known := ResolveHooks(levelFixture(), "", "", "post_add")
	if !known || len(got) != 1 || got[0].Run != "ws" {
		t.Fatalf("got %+v known=%v, want [ws] true", got, known)
	}
}

func TestResolveHooksRejectsUnknownEvent(t *testing.T) {
	if _, known := ResolveHooks(levelFixture(), "backend", "api", "post_lunch"); known {
		t.Fatal("an invented event was reported known")
	}
}

// HooksFor is the workspace-only form kept for event validation. A caller that wants to RUN hooks
// must not reach for it, so this pins the distinction rather than leaving both looking equivalent.
func TestHooksForStaysWorkspaceOnly(t *testing.T) {
	got, _ := levelFixture().HooksFor("post_add")
	if len(got) != 1 || got[0].Run != "ws" {
		t.Fatalf("got %+v, want just the workspace chain", got)
	}
}

func TestResolveDefaultsGroupBaseBranchBeatsWorkspace(t *testing.T) {
	c := levelFixture()
	c.Defaults.BaseBranch = "master"
	if got := ResolveDefaults(c, "api").BaseBranch; got != "develop" {
		t.Fatalf("base_branch: got %q, want the group's %q", got, "develop")
	}
}

// A repo can spell its policy flat (`branch_pattern:`, which predates the group level and is in
// manifests already in use) or in the uniform `defaults:` block. Both must work, and the block
// must win where they disagree — otherwise a manifest using both is ambiguous.
func TestRepoDefaultsMergesBothSpellingsBlockWinning(t *testing.T) {
	for _, tc := range []struct {
		name string
		repo Repo
		want Defaults
	}{
		{"flat pattern only", Repo{BranchPattern: "flat"}, Defaults{BranchPattern: "flat"}},
		{"flat provider only", Repo{BranchProvider: BranchNaming{pattern: "fp"}}, Defaults{BranchProvider: BranchNaming{pattern: "fp"}}},
		{"block only", Repo{Defaults: Defaults{BranchPattern: "blk"}}, Defaults{BranchPattern: "blk"}},
		{"block wins", Repo{BranchPattern: "flat", Defaults: Defaults{BranchPattern: "blk"}}, Defaults{BranchPattern: "blk"}},
		{"block fills only what it sets", Repo{BranchProvider: BranchNaming{pattern: "fp"}, Defaults: Defaults{BranchPattern: "blk"}}, Defaults{BranchPattern: "blk", BranchProvider: BranchNaming{pattern: "fp"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := repoDefaults(tc.repo)
			if got.BranchPattern != tc.want.BranchPattern || got.BranchProvider != tc.want.BranchProvider {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The repo level must survive a round trip through YAML. A key absent from the struct is dropped
// by the parser without error, so a manifest can carry it, parse clean, and lose it in silence.
func TestRepoLevelKeysSurviveUnmarshal(t *testing.T) {
	var c Config
	raw := []byte(`version: "3"
project: p
groups:
  backend:
    repos:
      api:
        remote: r
        defaults:
          base_branch: develop
        hooks:
          post_add:
            - run: go mod download
`)
	if err := yaml.Unmarshal(raw, &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r := c.Groups["backend"].Repos["api"]
	if r.Defaults.BaseBranch != "develop" {
		t.Errorf("repo defaults.base_branch was dropped: %+v", r.Defaults)
	}
	if len(r.Hooks.PostAdd) != 1 || r.Hooks.PostAdd[0].Run != "go mod download" {
		t.Errorf("repo hooks.post_add was dropped: %+v", r.Hooks)
	}
}

// A row earns its place by having an origin below the project, not by holding a different VALUE.
// A group that pins the workspace's own value — so the family stops following it — is a deliberate
// act, and a value comparison makes it invisible while crediting the project for it.
func TestExplainDefaultsReportsAGroupThatPinsTheProjectValue(t *testing.T) {
	c := &Config{
		Version:  SchemaVersion,
		Defaults: Defaults{BaseBranch: "master"},
		Groups: map[string]Group{
			"backend": {
				Defaults: Defaults{BaseBranch: "master"},
				Repos:    map[string]Repo{"api": {Remote: "r"}},
			},
		},
	}
	rows := ExplainDefaults(c)
	var found *ResolvedSetting
	for i := range rows {
		if rows[i].Key == "api.base_branch" {
			found = &rows[i]
		}
	}
	if found == nil {
		t.Fatalf("the group's ownership of base_branch is not reported: %+v", rows)
	}
	if found.From != "group backend" {
		t.Errorf("from = %q, want %q", found.From, "group backend")
	}
}

// An alias may appear in two groups: a manifest is hand-editable and the uniqueness check lives in
// clone. Each pair must get its own row, qualified so two rows cannot share one key.
func TestExplainDefaultsQualifiesAnAliasUsedInTwoGroups(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Groups: map[string]Group{
			"backend": {Defaults: Defaults{BaseBranch: "develop"}, Repos: map[string]Repo{"api": {Remote: "r1"}}},
			"web":     {Defaults: Defaults{BaseBranch: "staging"}, Repos: map[string]Repo{"api": {Remote: "r2"}}},
		},
	}
	got := map[string]string{}
	for _, r := range ExplainDefaults(c) {
		if _, clash := got[r.Key]; clash {
			t.Fatalf("two rows share the key %q", r.Key)
		}
		got[r.Key] = r.Value
	}
	for key, want := range map[string]string{
		"backend/api.base_branch": "develop",
		"web/api.base_branch":     "staging",
	} {
		if got[key] != want {
			t.Errorf("%s = %q, want %q (rows: %+v)", key, got[key], want, got)
		}
	}
}

// The origin is recorded by the resolution, so the reporter cannot disagree with the resolver.
func TestExplainDefaultsAgreesWithResolveDefaults(t *testing.T) {
	c := &Config{
		Version:  SchemaVersion,
		Defaults: Defaults{BaseBranch: "master", BranchPattern: "p/{slug}"},
		Groups: map[string]Group{
			"backend": {
				Defaults: Defaults{BaseBranch: "develop"},
				Repos:    map[string]Repo{"api": {Remote: "r", BranchProvider: BranchNaming{pattern: "cmd"}}},
			},
		},
	}
	want := ResolveDefaults(c, "api")
	got := map[string]string{}
	for _, r := range ExplainDefaults(c) {
		if k, ok := strings.CutPrefix(r.Key, "api."); ok {
			got[k] = r.Value
		}
	}
	for key, value := range map[string]string{
		"base_branch":     want.BaseBranch,
		"branch_provider": want.effectiveNaming().Pattern(),
	} {
		if value != "" && got[key] != value {
			t.Errorf("%s: reporter says %q, resolver says %q", key, got[key], value)
		}
	}
}

// An alias may appear in two groups, and FindRepo collapses those onto the first. Resolving a hook
// chain by alias alone therefore ran one group's hooks for a worktree in the other — the wrong
// chain, silently. The group is named, so it cannot be guessed.
func TestResolveHooksUsesTheNamedGroupNotTheFirstMatch(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Hooks:   Hooks{PostAdd: []Hook{{Run: "ws"}}},
		Groups: map[string]Group{
			"backend": {
				Hooks: Hooks{PostAdd: []Hook{{Run: "grp-backend"}}},
				Repos: map[string]Repo{"api": {Remote: "r1"}},
			},
			"web": {
				Hooks: Hooks{PostAdd: []Hook{{Run: "grp-web"}}},
				Repos: map[string]Repo{"api": {Remote: "r2"}},
			},
		},
	}
	for group, want := range map[string]string{"backend": "grp-backend", "web": "grp-web"} {
		got, _ := ResolveHooks(c, group, "api", "post_add")
		if len(got) != 2 || got[1].Run != want {
			t.Errorf("group %q resolved to %+v, want the workspace chain then %q", group, got, want)
		}
	}
}

func TestResolveHooksTagsWorkspacePath(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Hooks:   Hooks{PostAdd: []Hook{{Run: "ws"}}},
		Groups: map[string]Group{
			"backend": {Repos: map[string]Repo{"api": {Remote: "r"}}},
		},
	}
	got, known := ResolveHooks(c, "backend", "api", "post_add")
	if !known || len(got) != 1 || got[0].Path != "hooks.post_add[0]" {
		t.Fatalf("got %+v known=%v, want hooks.post_add[0]", got, known)
	}
}

func TestResolveHooksTagsGroupPath(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Groups: map[string]Group{
			"backend": {
				Hooks: Hooks{PostAdd: []Hook{{Run: "grp"}}},
				Repos: map[string]Repo{"api": {Remote: "r"}},
			},
		},
	}
	got, _ := ResolveHooks(c, "backend", "api", "post_add")
	if len(got) != 1 || got[0].Path != "groups.backend.hooks.post_add[0]" {
		t.Fatalf("got %+v, want groups.backend.hooks.post_add[0]", got)
	}
}

func TestResolveHooksTagsRepoPath(t *testing.T) {
	c := &Config{
		Version: SchemaVersion,
		Groups: map[string]Group{
			"backend": {
				Repos: map[string]Repo{
					"api": {Remote: "r", Hooks: Hooks{PostAdd: []Hook{{Run: "repo"}}}},
				},
			},
		},
	}
	got, _ := ResolveHooks(c, "backend", "api", "post_add")
	if len(got) != 1 || got[0].Path != "groups.backend.repos.api.hooks.post_add[0]" {
		t.Fatalf("got %+v, want groups.backend.repos.api.hooks.post_add[0]", got)
	}
}

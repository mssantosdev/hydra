package config

import "testing"

// A three-level chain cannot be read off the manifest: workspace, group and repo can all set
// base_branch, so "why is my base develop" needs the resolution run and the winner named.
// ExplainDefaults is what `hydra config show` reports, and these pin the naming.

func explainSetup() *Config {
	return &Config{
		Version: SchemaVersion,
		Project: "shop",
		Defaults: Defaults{
			BaseBranch:    "master",
			BranchPattern: "{kind}/{slug}",
		},
		Groups: map[string]Group{
			"backend": {
				Defaults: Defaults{BaseBranch: "develop"},
				Repos: map[string]Repo{
					"api":     {Remote: "git@example.com:org/api.git", BranchPattern: "feat/{slug}"},
					"billing": {Remote: "git@example.com:org/billing.git"},
				},
			},
			"web": {
				Repos: map[string]Repo{
					"storefront": {Remote: "git@example.com:org/storefront.git"},
				},
			},
		},
	}
}

func findSetting(t *testing.T, rows []ResolvedSetting, key string) ResolvedSetting {
	t.Helper()
	for _, r := range rows {
		if r.Key == key {
			return r
		}
	}
	t.Fatalf("no row for %q in %+v", key, rows)
	return ResolvedSetting{}
}

func TestExplainDefaultsNamesTheWinningLevel(t *testing.T) {
	rows := ExplainDefaults(explainSetup())

	if got := findSetting(t, rows, "base_branch"); got.Value != "master" || got.From != "project" {
		t.Errorf("project base_branch = %q from %q, want master from project", got.Value, got.From)
	}
	// The group overrides the workspace.
	if got := findSetting(t, rows, "api.base_branch"); got.Value != "develop" || got.From != "group backend" {
		t.Errorf("api.base_branch = %q from %q, want develop from group backend", got.Value, got.From)
	}
	// The repo overrides both.
	if got := findSetting(t, rows, "api.branch_pattern"); got.Value != "feat/{slug}" || got.From != "repo api" {
		t.Errorf("api.branch_pattern = %q from %q, want feat/{slug} from repo api", got.Value, got.From)
	}
	// A repo that only inherits the GROUP's override still reports the group, not itself.
	if got := findSetting(t, rows, "billing.base_branch"); got.From != "group backend" {
		t.Errorf("billing.base_branch from %q, want group backend", got.From)
	}
}

// A repo whose resolved values all match the workspace contributes no rows. Otherwise the
// output is repo count times key count, and the overrides — the only interesting part — are
// buried in restatements of the default.
func TestExplainDefaultsOmitsPureInheritance(t *testing.T) {
	rows := ExplainDefaults(explainSetup())
	for _, r := range rows {
		if r.Key == "storefront.base_branch" || r.Key == "storefront.branch_pattern" {
			t.Errorf("storefront inherits everything and should contribute no rows, got %+v", r)
		}
	}
}

func TestExplainDefaultsHandlesNoManifest(t *testing.T) {
	if rows := ExplainDefaults(nil); rows != nil {
		t.Errorf("nil config should explain nothing, got %+v", rows)
	}
	empty := &Config{Version: SchemaVersion, Groups: map[string]Group{}}
	if rows := ExplainDefaults(empty); len(rows) != 0 {
		t.Errorf("a manifest with no defaults should explain nothing, got %+v", rows)
	}
}

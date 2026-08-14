package config

import "sort"

// ExecutableEntry is one manifest value whose presence causes hydra to execute something.
//
// Path is the YAML path it was read from, which is what an error reports and what a human
// opens the file to. Value is the literal string hydra would run.
type ExecutableEntry struct {
	Path  string
	Value string
}

// ExecutableSurface returns EVERY manifest value that causes execution, in a deterministic
// order.
//
// This is one named list on purpose. The trust fingerprint is derived from it rather than
// from a second enumeration, so a future executable field that is not added here fails to
// be covered by the gate in a way that shows up as a missing test rather than a silent
// hole — and adding a field means changing one function, not two.
//
// What is deliberately NOT here:
//
//   - branch_provider in its SCALAR form, and the deprecated branch_pattern. Those are
//     closed placeholder substitution over a literal string. Nothing is executed, so
//     nothing needs approving, and including them would cost trust on an edit that cannot
//     run code.
//   - carry entries. They copy files under manifest direction, which is a real capability,
//     but it is containment-checked rather than executed. Whether it belongs in the
//     fingerprint is an open question recorded in the design, not settled here.
//   - anything git-derived. The surface is a property of the manifest alone, so the same
//     manifest fingerprints identically on every machine.
func ExecutableSurface(c *Config) []ExecutableEntry {
	if c == nil {
		return nil
	}
	var out []ExecutableEntry

	// Hooks, at all three levels. AllConfiguredHooks already tags each entry with its
	// manifest path and orders them workspace → groups → repos, sorted within each.
	for _, event := range HookEvents() {
		for _, hook := range AllConfiguredHooks(c, event) {
			if hook.Run == "" {
				continue
			}
			// ResolvedHook.Path is already the full manifest path including the event and
			// index, and it is the same string a hook FAILURE reports — so a trust refusal
			// and a hook failure name the same entry the same way, by construction rather
			// than by two functions agreeing.
			out = append(out, ExecutableEntry{Path: hook.Path, Value: hook.Run})
		}
	}

	// branch_provider in its RUNNABLE form, at all three levels. The scalar form is a
	// pattern and executes nothing.
	appendRunnable(&out, "defaults.branch_provider", c.Defaults.BranchProvider)
	for _, group := range c.SortedGroups() {
		g := c.Groups[group]
		appendRunnable(&out, "groups."+group+".defaults.branch_provider", g.Defaults.BranchProvider)
		aliases := make([]string, 0, len(g.Repos))
		for alias := range g.Repos {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			repo := g.Repos[alias]
			base := "groups." + group + ".repos." + alias
			appendRunnable(&out, base+".branch_provider", repo.BranchProvider)
			appendRunnable(&out, base+".defaults.branch_provider", repo.Defaults.BranchProvider)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Value < out[j].Value
	})
	return out
}

func appendRunnable(out *[]ExecutableEntry, path string, naming BranchNaming) {
	runnable, ok := naming.Runnable()
	if !ok || runnable.Run == "" {
		return
	}
	*out = append(*out, ExecutableEntry{Path: path + ".run", Value: runnable.Run})
}

// HasExecutableSurface reports whether this manifest can cause execution at all.
//
// The trust gate is skipped entirely when it cannot: a workspace created by `hydra init`
// and never given a hook has nothing to approve, so it never sees the feature. That is most
// of the blast radius removed, and it is derived from the same list rather than being a
// special case that could disagree with it.
func HasExecutableSurface(c *Config) bool {
	return len(ExecutableSurface(c)) > 0
}

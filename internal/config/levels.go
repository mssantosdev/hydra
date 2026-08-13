package config

import (
	"fmt"
	"sort"
)

// ResolvedHook is a hook tagged with the manifest path it was loaded from.
type ResolvedHook struct {
	Hook
	Path string
}

// ResolveHooks returns the chain to run for one event and one repository, appended
// workspace → group → repo.
//
// Lists append rather than override, so a workspace-wide `direnv allow`, a group's shared
// bring-up and a repo's `go mod download` all run. Overriding would force every child to
// restate what it inherits.
func ResolveHooks(c *Config, group, alias, event string) ([]ResolvedHook, bool) {
	if c == nil {
		return nil, false
	}
	chain, known := hooksOf(c.Hooks, event)
	if !known {
		return nil, false
	}
	out := taggedHooks("", chain, event)

	// The GROUP is named rather than looked up from the alias. An alias may appear in two groups —
	// nothing rejects it, because a manifest is hand-editable — and FindRepo collapses those onto
	// the first, so resolving by alias alone runs one group's chain for a worktree in the other.
	g, ok := c.Groups[group]
	if !ok || group == "" {
		// No group in scope: a topic event, or a caller that named none. The workspace chain is
		// the whole answer.
		return out, true
	}
	if hs, _ := hooksOf(g.Hooks, event); len(hs) > 0 {
		out = append(out, taggedHooks("groups."+group+".", hs, event)...)
	}
	if hs, _ := hooksOf(g.Repos[alias].Hooks, event); len(hs) > 0 {
		out = append(out, taggedHooks("groups."+group+".repos."+alias+".", hs, event)...)
	}
	return out, true
}

// AllConfiguredHooks returns every hook entry for an event anywhere in the manifest,
// each tagged with its manifest path. The order is workspace, then groups in sorted
// name order, then repos in sorted alias order within each group.
func AllConfiguredHooks(c *Config, event string) []ResolvedHook {
	if c == nil {
		return nil
	}
	chain, known := hooksOf(c.Hooks, event)
	if !known {
		return nil
	}
	out := taggedHooks("", chain, event)
	for _, group := range c.SortedGroups() {
		g := c.Groups[group]
		if hs, _ := hooksOf(g.Hooks, event); len(hs) > 0 {
			out = append(out, taggedHooks("groups."+group+".", hs, event)...)
		}
		aliases := make([]string, 0, len(g.Repos))
		for alias := range g.Repos {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			if hs, _ := hooksOf(g.Repos[alias].Hooks, event); len(hs) > 0 {
				out = append(out, taggedHooks("groups."+group+".repos."+alias+".", hs, event)...)
			}
		}
	}
	return out
}

func taggedHooks(prefix string, hs []Hook, event string) []ResolvedHook {
	if len(hs) == 0 {
		return nil
	}
	out := make([]ResolvedHook, 0, len(hs))
	for i, h := range hs {
		out = append(out, ResolvedHook{
			Hook: h,
			Path: fmt.Sprintf("%shooks.%s[%d]", prefix, event, i),
		})
	}
	return out
}

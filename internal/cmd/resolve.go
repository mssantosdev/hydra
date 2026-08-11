package cmd

import (
	"fmt"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/topic"
)

// Session is the resolved project a command operates within.
//
// It exists so the resolver takes its inputs as arguments instead of reading the
// package globals. Those globals are set once by loadProject and are fine for a
// read-only leaf command, but resolution must be callable against an explicit
// project: `--all` spans several, and mutating globals mid-invocation to walk them
// is how a command ends up operating on the wrong workspace.
type Session struct {
	Cfg        *config.Config
	Root       string
	ConfigPath string
	Topics     *topic.Store
}

// currentSession snapshots what loadProject resolved.
func currentSession() Session {
	return Session{
		Cfg:        cfg,
		Root:       projectRoot,
		ConfigPath: projectConfigPath,
		Topics:     topic.Open(projectRoot),
	}
}

// sessionFor builds a session for one entry of a multi-project walk.
func sessionFor(target projectTarget) Session {
	return Session{
		Cfg:    target.Cfg,
		Root:   target.Root,
		Topics: topic.Open(target.Root),
	}
}

// Selector describes one query over a project's worktrees. The zero value selects
// every worktree.
//
// Topic, Repos, Group and Filter NARROW the set. Against does not — it annotates each
// surviving worktree with a comparison. It lives here because it is part of the same
// query and must reach the tracking phase, and empty() deliberately ignores it: a
// bare --against still describes every worktree.
type Selector struct {
	Topic   string
	Repos   []string
	Group   string
	Filter  []string
	Against string
}

// empty reports whether the selector narrows anything at all.
func (s Selector) empty() bool {
	return s.Topic == "" && len(s.Repos) == 0 && s.Group == "" && len(s.Filter) == 0
}

// filters is the parsed form of --filter.
//
// The value set is closed so it can be completed and validated. An open-ended
// expression language was refused: it cannot be completed, and every value here is
// answerable from data hydra already computes.
type filters struct {
	dirty    bool
	behind   bool
	branches []string
}

// derived reports whether any filter needs per-worktree git data. It decides
// whether the expensive tracking phase must run at all.
func (f filters) derived() bool { return f.dirty || f.behind }

const filterValues = "dirty, behind, or branch:<glob>"

func parseFilters(raw []string) (filters, error) {
	var out filters
	for _, entry := range raw {
		value := strings.TrimSpace(entry)
		switch {
		case value == "dirty":
			out.dirty = true
		case value == "behind":
			out.behind = true
		case strings.HasPrefix(value, "branch:"):
			glob := strings.TrimPrefix(value, "branch:")
			if glob == "" {
				return filters{}, output.Errorf(output.CodeInternal,
					"--filter branch: needs a pattern, for example branch:feat/*")
			}
			// Reject a malformed glob up front; path.Match would return false for every branch.
			if _, err := path.Match(glob, ""); err != nil {
				return filters{}, output.Errorf(output.CodeInternal,
					"invalid --filter branch pattern %q: %v", glob, err)
			}
			out.branches = append(out.branches, glob)
		default:
			return filters{}, output.Errorf(output.CodeInternal,
				"invalid --filter value %q (want %s)", value, filterValues).
				WithDetail("filter", value).
				WithDetail("valid", []string{"dirty", "behind", "branch:<glob>"})
		}
	}
	return out, nil
}

// matchesBranch reports whether a branch satisfies the branch globs. Several globs
// are a union: --filter branch:feat/* --filter branch:fix/* means either.
func (f filters) matchesBranch(branch string) bool {
	if len(f.branches) == 0 {
		return true
	}
	for _, glob := range f.branches {
		if ok, _ := path.Match(glob, branch); ok {
			return true
		}
	}
	return false
}

// resolvedWorktree pairs a worktree with the envelope item derived from it. Item is
// only fully populated when the tracking phase ran.
type resolvedWorktree struct {
	Context worktreeContext
	Item    worktreeJSON
}

// resolveTargets applies a selector to a session's worktrees.
//
// Filtering is deliberately two-phase. Topic, repo, group and branch are answerable
// from data already in hand, so they run FIRST and shrink the set before any
// per-worktree git call. Dirty and behind are *derived from* those calls, so they
// can only run after. Collapsing the phases either makes the cheap filters pay for
// git on worktrees about to be discarded, or makes the derived filters impossible.
//
// tracking forces the expensive phase for callers that need ahead/behind/dirty
// regardless of filtering. A derived filter turns it on by itself.
// resolveTargets returns the matching worktrees, any warnings, and how many
// REPOSITORIES failed outright.
//
// Repository failures are counted explicitly so advisory warnings (empty selector,
// unresolvable --against ref, and similar) do not inflate the failure count.
func resolveTargets(s Session, sel Selector, tracking bool) ([]resolvedWorktree, []string, int, error) {
	parsed, err := parseFilters(sel.Filter)
	if err != nil {
		return nil, nil, 0, err
	}

	// Validate narrowing values before doing any work, so a typo is reported as a
	// typo rather than as an empty result.
	if err := validateRepos(s, sel.Repos); err != nil {
		return nil, nil, 0, err
	}
	if err := validateGroup(s, sel.Group); err != nil {
		return nil, nil, 0, err
	}

	// Topic EXISTENCE is deliberately not checked here.
	//
	// The resolver runs once per project, so failing on "this project has no topic X"
	// would abort a --all walk the moment it reached a project that legitimately has
	// no such topic — even though X lives in a sibling project. Existence is answered
	// once across the whole walk by requireTopicInTargets in the caller; here an
	// absent topic simply means "nothing in this project matches", which the
	// membership comparison below already produces.
	//
	// Unreadable state is a different thing from an absent topic, and is handled
	// immediately below: corruption may fail, absence may not.
	index, indexErr := newTopicIndex(s.Root)

	contexts, warnings := collectWorktrees(s.Cfg, s.Root)
	// Everything collectWorktrees reported is a repository that could not be read. Count
	// it here, before any advisory warning is appended below.
	repoFailures := len(warnings)
	if indexErr != nil {
		// Membership is unreadable. That is fatal only when it was asked about;
		// otherwise a listing with git data intact is still worth returning.
		if sel.Topic != "" {
			return nil, warnings, repoFailures, indexErr
		}
		warnings = append(warnings, fmt.Sprintf("topic state unreadable: %v", indexErr))
	}

	// Phase one: cheap.
	kept := make([]resolvedWorktree, 0, len(contexts))
	repos := lowerSet(sel.Repos)
	for _, ctx := range contexts {
		if len(repos) > 0 {
			if _, ok := repos[strings.ToLower(ctx.RepoContext.Alias)]; !ok {
				continue
			}
		}
		if sel.Group != "" && !strings.EqualFold(ctx.RepoContext.Group, sel.Group) {
			continue
		}
		if !parsed.matchesBranch(ctx.Branch) {
			continue
		}
		item := ctx.json()
		index.decorate(&item)
		if sel.Topic != "" && (item.Topic == nil || *item.Topic != sel.Topic) {
			continue
		}
		kept = append(kept, resolvedWorktree{Context: ctx, Item: item})
	}

	// --against forces the expensive phase even with no derived filter: the comparison
	// is per worktree and needs git.
	if !tracking && !parsed.derived() && sel.Against == "" {
		return kept, emptySelectionWarning(warnings, sel, len(contexts), len(kept)), repoFailures, nil
	}

	// Phase two: expensive, and only over what survived phase one.
	out := make([]resolvedWorktree, 0, len(kept))
	for _, target := range kept {
		item, err := target.Context.withTracking()
		if err != nil {
			// Coded, so a consumer can branch on it. The git text is kept because it
			// names the cause, but it arrived verbatim and in the system locale, which
			// nothing downstream could match.
			code := output.CodeGitFailed
			if _, statErr := os.Stat(target.Context.Path); statErr != nil {
				code = output.CodeWorktreeUnknown
			}
			warnings = append(warnings, fmt.Sprintf("%s: %s: %v",
				code, target.Context.Qualified(), err))
			// Keep the un-tracked item so the worktree is still reported, but never
			// let it satisfy a derived filter: its dirty/behind fields are unknown,
			// not false.
			if parsed.derived() {
				continue
			}
			out = append(out, target)
			continue
		}
		index.decorate(&item)
		if parsed.dirty && !item.Dirty {
			continue
		}
		if parsed.behind && item.Behind == 0 {
			continue
		}
		decorateAgainst(&item, target.Context, sel.Against, &warnings)
		out = append(out, resolvedWorktree{Context: target.Context, Item: item})
	}
	return out, emptySelectionWarning(warnings, sel, len(contexts), len(out)), repoFailures, nil
}

// emptySelectionWarning says so when a selector reduced a non-empty workspace to nothing.
//
// Zero matches is a legitimate answer — "nothing is dirty" is true and must stay exit 0 —
// but it is indistinguishable from a typo'd glob, and a caller reading `success` with an
// empty list concludes the workspace has no such work rather than that its selector was
// wrong. Naming how many candidates were considered makes the two cases tellable apart
// without turning a valid answer into an error.
func emptySelectionWarning(warnings []string, sel Selector, candidates, kept int) []string {
	if kept > 0 || candidates == 0 {
		return warnings
	}
	var used []string
	if sel.Topic != "" {
		used = append(used, "--topic "+sel.Topic)
	}
	if len(sel.Repos) > 0 {
		used = append(used, "--repos "+strings.Join(sel.Repos, ","))
	}
	if sel.Group != "" {
		used = append(used, "--group "+sel.Group)
	}
	for _, f := range sel.Filter {
		used = append(used, "--filter "+f)
	}
	if len(used) == 0 {
		return warnings
	}
	return append(warnings, fmt.Sprintf(
		"%s matched none of the %d worktree(s) in this project",
		strings.Join(used, " "), candidates))
}

// decorateAgainst annotates one worktree with its position relative to REF.
//
// A failure is a per-worktree warning rather than a fatal error: an unresolvable ref
// in one repository must not make the other repositories unlistable, and that is the
// normal case — a release branch often exists in some repos and not others.
//
// A detached worktree is skipped: there is no branch to compare.
func decorateAgainst(item *worktreeJSON, ctx worktreeContext, ref string, warnings *[]string) {
	if ref == "" || item.Detached {
		return
	}

	ahead, behind, err := git.CountAgainst(ctx.Path, ref)
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s: %v", ctx.Qualified(), err))
		return
	}
	item.Against = &againstJSON{
		Ref: ref,
		// Merged is Ahead == 0: nothing on this branch is missing from REF. That is the
		// question people actually ask, so it is answered rather than left to be
		// derived from a count.
		Merged: ahead == 0,
		Ahead:  ahead,
		Behind: behind,
	}
}

func lowerSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	return out
}

// validateRepos rejects an unknown alias instead of returning nothing. "That repo
// is not registered" and "that repo has no worktrees" are different answers.
func validateRepos(s Session, aliases []string) error {
	if len(aliases) == 0 {
		return nil
	}
	known := make(map[string]struct{})
	var names []string
	for _, ref := range s.Cfg.Repos() {
		known[strings.ToLower(ref.Alias)] = struct{}{}
		names = append(names, ref.Alias)
	}
	sort.Strings(names)
	for _, alias := range aliases {
		if _, ok := known[strings.ToLower(strings.TrimSpace(alias))]; !ok {
			return output.Errorf(output.CodeRepoUnknown,
				"repository %q is not registered; run \"hydra repo list\" to see registered repositories", alias).
				WithDetail("repo", alias).
				WithDetail("known", names)
		}
	}
	return nil
}

// validateGroup rejects an unknown group for the same reason.
func validateGroup(s Session, group string) error {
	if group == "" {
		return nil
	}
	var names []string
	for _, name := range s.Cfg.SortedGroups() {
		if strings.EqualFold(name, group) {
			return nil
		}
		names = append(names, name)
	}
	return output.Errorf(output.CodeRepoUnknown,
		"group %q does not exist; run \"hydra list\" to see groups", group).
		WithDetail("group", group).
		WithDetail("known", names)
}

// matchWorktrees returns EVERY worktree a handle matches.
//
// Directory and group-qualified names are tried before branch names, because a
// qualified name is unique by construction while a branch name is not: every repo
// has a main, so "main" legitimately names several worktrees.
func matchWorktrees(items []worktreeContext, query string) []worktreeContext {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}

	var byName []worktreeContext
	for _, item := range items {
		if strings.EqualFold(item.DirName, query) || strings.EqualFold(item.Qualified(), query) {
			byName = append(byName, item)
		}
	}
	if len(byName) > 0 {
		return byName
	}

	var byBranch []worktreeContext
	for _, item := range items {
		if item.Branch != "" && strings.EqualFold(item.Branch, query) {
			byBranch = append(byBranch, item)
		}
	}
	return byBranch
}

// resolveOneWorktree requires a handle to name exactly one worktree.
//
// Returning the first of several matches was a real bug: every repo has a main
// branch, so "hydra path main" picked whichever repo happened to sort first and
// reported no problem. Silently acting on the wrong worktree is worse than
// refusing, so ambiguity is an error listing the candidates.
func resolveOneWorktree(items []worktreeContext, query string) (worktreeContext, error) {
	matches := matchWorktrees(items, query)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return worktreeContext{}, output.Errorf(output.CodeWorktreeUnknown,
			"no worktree named %q; run \"hydra list\" to see worktrees", query).
			WithDetail("worktree", query)
	default:
		candidates := make([]string, 0, len(matches))
		for _, match := range matches {
			candidates = append(candidates, match.Qualified())
		}
		sort.Strings(candidates)
		return worktreeContext{}, output.Errorf(output.CodeWorktreeNameConflict,
			"%q matches %d worktrees; name one of %s",
			query, len(matches), strings.Join(candidates, ", ")).
			WithDetail("worktree", query).
			WithDetail("candidates", candidates)
	}
}

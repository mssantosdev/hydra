package config

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// CarryMode is how a carried file gets into a new worktree.
const (
	// CarryCopy duplicates the file. Independent afterwards, and it survives the source
	// worktree being removed.
	CarryCopy = "copy"
	// CarryLink symlinks it, so one file is edited in one place. Removing the source
	// worktree breaks every link into it, which doctor reports.
	CarryLink = "link"
)

// CarryEntry is one file a new worktree needs that git will not bring: a `.env`, a dev
// certificate, a `docker-compose.override.yml`. A fresh worktree has every tracked file and
// none of these, so it cannot run until someone copies them by hand, per worktree, per repo,
// forever — a tax nobody reports as a bug because everyone learns the workaround in week one.
//
// This is deliberately NOT a hook. Placing files is materialisation, and hydra already owns
// layout: it knows the source worktree because it just resolved one. A hook would have to
// rebuild `<root>/<group>/<repo>`, which `--as` can override, and a missing file would come
// back as `hook_failed` rather than the warning it actually is.
//
// Two forms, because the sources are genuinely different:
//
//	carry:
//	  - .env                    # from the SOURCE WORKTREE of this repo
//	  - from: .shared/dev.pem   # from a WORKSPACE path — no repo, no source worktree
//	    to: certs/dev.pem
//	    mode: link
//
// The bare form only means something where a source worktree exists, so it warns on a fresh
// clone — `apply` and `repo restore` replay structure, not secrets. The `from:` form has a
// fixed workspace-relative source and survives a fresh machine, which is why shared context
// belongs in it.
type CarryEntry struct {
	// Path is the bare form: one path, relative to the worktree, taken from the source
	// worktree at the same relative location.
	Path string `yaml:"path,omitempty"`

	// From is a workspace-relative source. To is where it lands inside the worktree, and
	// defaults to From when omitted.
	From string `yaml:"from,omitempty"`
	To   string `yaml:"to,omitempty"`

	// Mode is copy (default) or link.
	Mode string `yaml:"mode,omitempty"`
}

// Dest is where this entry lands inside a worktree, relative to its root.
func (e CarryEntry) Dest() string {
	switch {
	case e.To != "":
		return e.To
	case e.Path != "":
		return e.Path
	default:
		return e.From
	}
}

// FromWorkspace reports whether the source is a fixed path rather than the source
// worktree. These are the only entries that can be satisfied on a fresh clone.
func (e CarryEntry) FromWorkspace() bool { return e.From != "" }

// OutsideWorkspace reports whether this entry's source reaches outside the workspace.
//
// Only the EXPLICIT spellings count: an absolute path or `~/`. A relative `from:` is resolved
// against the workspace root and stays containment-checked at carry time — the obfuscated
// spellings of "outside" (`..`, a symlink that leaves) are refused, so a manifest that reads
// beyond its workspace SAYS so in the diff a trust approval reviews.
func (e CarryEntry) OutsideWorkspace() bool {
	return e.From != "" && (path.IsAbs(e.From) || strings.HasPrefix(e.From, "~/"))
}

// UnmarshalYAML accepts either a bare string or a mapping, so the common case reads as a
// list of filenames instead of a list of single-key objects.
func (e *CarryEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		var s string
		if err := value.Decode(&s); err != nil {
			return err
		}
		e.Path = strings.TrimSpace(s)
		return e.validate()
	}
	// A named type avoids recursing into this method.
	type raw CarryEntry
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	*e = CarryEntry(r)
	e.Path = strings.TrimSpace(e.Path)
	e.From = strings.TrimSpace(e.From)
	e.To = strings.TrimSpace(e.To)
	e.Mode = strings.TrimSpace(e.Mode)
	return e.validate()
}

// MarshalYAML writes the bare form back as a bare string, so a manifest a human wrote as
// `- .env` does not come back as `- path: .env` the first time hydra saves it.
func (e CarryEntry) MarshalYAML() (any, error) {
	if e.Path != "" && e.From == "" && e.To == "" && e.Mode == "" {
		return e.Path, nil
	}
	type raw CarryEntry
	return raw(e), nil
}

// validate rejects entries that cannot be acted on, at parse time rather than at the moment
// a worktree is being created — a manifest that names nothing, or names two sources, is a
// mistake worth reporting before it is half-applied.
func (e CarryEntry) validate() error {
	switch {
	case e.Path == "" && e.From == "":
		return fmt.Errorf("carry entry needs either a path or a `from:`")
	case e.Path != "" && e.From != "":
		return fmt.Errorf("carry entry %q cannot have both a bare path and a `from:`", e.Path)
	case e.Path != "" && e.To != "":
		return fmt.Errorf("carry entry %q cannot have both a bare path and a `to:`; use from/to", e.Path)
	}
	if e.Mode != "" && e.Mode != CarryCopy && e.Mode != CarryLink {
		return fmt.Errorf("carry entry %q has mode %q, want %q or %q", e.Dest(), e.Mode, CarryCopy, CarryLink)
	}
	// An absolute or escaping destination would write outside the worktree, which is not
	// something a manifest should be able to ask for.
	dest := e.Dest()
	if path.IsAbs(dest) || strings.HasPrefix(path.Clean(dest), "..") {
		return fmt.Errorf("carry destination %q must stay inside the worktree", dest)
	}
	if e.From != "" {
		switch {
		case strings.HasPrefix(path.Clean(e.From), ".."):
			// `..` stays refused even though absolute paths are now legal: an outside source
			// must be SPELLED as outside (absolute or ~/), so the manifest diff a trust
			// approval reviews says what it reaches. Dot-dot walking is the obfuscated spelling.
			return fmt.Errorf("carry source %q must not walk out of the workspace with ..; name the target explicitly (an absolute or ~/ path)", e.From)
		case e.From == "~" || (strings.HasPrefix(e.From, "~") && !strings.HasPrefix(e.From, "~/")):
			return fmt.Errorf("carry source %q: only ~/ expands (to your home directory); ~user is not supported", e.From)
		}
	}
	return nil
}

// ResolveCarry returns the carry entries that apply to one repository, nearest level last.
//
// Levels are walked as an ORDERED LIST rather than being hardcoded, because the middle one
// does not exist yet: `Groups` is map[string]map[string]Repo, so a group has nowhere to hold
// anything. When it becomes an object, its entries slot into this slice and neither the
// append semantics nor the tests around them change.
//
// Lists APPEND down the chain — a workspace carrying a shared certificate and a repo carrying
// its own `.env` both apply. Replacing would force every repo to restate what it inherited,
// and someone will forget one. A later level naming the same DESTINATION wins, so a repo can
// override how an inherited file arrives without having to suppress it first.
func ResolveCarry(c *Config, alias string) []CarryEntry {
	if c == nil {
		return nil
	}
	levels := [][]CarryEntry{c.Carry}
	if ref, ok := c.FindRepo(alias); ok {
		levels = append(levels, c.Groups[ref.Group].Carry, ref.Repo.Carry)
	}

	out := make([]CarryEntry, 0, 4)
	at := map[string]int{}
	for _, level := range levels {
		for _, entry := range level {
			dest := path.Clean(entry.Dest())
			if i, seen := at[dest]; seen {
				out[i] = entry
				continue
			}
			at[dest] = len(out)
			out = append(out, entry)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ResolveDefaults collapses the workspace → group → repo chain for one repository.
//
// SCALARS resolve nearest-wins: a repo's own value beats its group's, which beats the workspace's.
// That is the level the model was missing — the chain ran project → repo and skipped the one that
// means "these repositories belong together", so a family convention had to be repeated on every
// repo, and `base_branch` could not vary below the project at all.
//
// An empty value is not a value: it means "inherit", not "clear". A group cannot blank a workspace
// default by declaring an empty string, because a YAML author writing `base_branch: ""` almost
// always means they left it unset rather than that they want the fallback suppressed.
func ResolveDefaults(c *Config, alias string) Defaults {
	if c == nil {
		return Defaults{}
	}
	out := c.Defaults
	ref, ok := c.FindRepo(alias)
	if !ok {
		return out
	}
	for _, level := range []Defaults{c.Groups[ref.Group].Defaults, repoDefaults(ref.Repo)} {
		if level.BaseBranch != "" {
			out.BaseBranch = level.BaseBranch
		}
		if !level.BranchProvider.IsZero() {
			out.BranchProvider = level.BranchProvider
			out.BranchPattern = ""
		} else if level.BranchPattern != "" {
			out.BranchPattern = level.BranchPattern
			out.BranchProvider = BranchNaming{}
		}
		// A bool has no "unset", so strictness is turned ON by any level and never off by a
		// silent zero value. Escaping an inherited strict pattern is a deliberate edit at the
		// level that set it, not an accident of a child having no opinion.
		if level.BranchPatternStrict {
			out.BranchPatternStrict = true
		}
	}
	return out
}

// repoDefaults lifts a repo's overrides into a Defaults so the chain is one loop over levels
// rather than three special cases.
//
// A repo can spell them two ways: the flat `branch_pattern`/`branch_provider` fields, referenced
// by name in manifests already in use, and the uniform `defaults:` block every level carries.
// The block wins where it is set, so a manifest using both is not ambiguous.
func repoDefaults(r Repo) Defaults {
	out := r.Defaults
	if out.BranchProvider.IsZero() && !r.BranchProvider.IsZero() {
		out.BranchProvider = r.BranchProvider
	}
	if out.BranchPattern == "" && r.BranchPattern != "" && out.BranchProvider.IsZero() {
		out.BranchPattern = r.BranchPattern
	}
	return out
}

// ResolvedSetting is one effective manifest value and the level that supplied it.
type ResolvedSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	From  string `json:"from"`
}

// ExplainDefaults reports the effective manifest defaults and where each value came from.
//
// It exists because a three-level chain cannot be read off the file. With workspace, group and
// repo all able to set base_branch, "why is my base develop" has no answer you can get by
// looking — you have to run the resolution in your head across three places. This runs it and
// names the winner.
//
// The origin is recorded BY the resolution, not re-derived after it: the level that writes a value
// last is the level it came from, so nearest-wins and provenance are one fact. A second walk with
// its own copy of the precedence rule could disagree with the first, and a new rule added to the
// resolver would silently not reach it.
//
// Rows are returned for the project level and for every repo whose value comes from somewhere
// else, so the output is as long as the overrides actually are rather than repo count times key
// count.
func ExplainDefaults(c *Config) []ResolvedSetting {
	if c == nil {
		return nil
	}
	var out []ResolvedSetting
	for _, s := range settingsOf(c.Defaults) {
		s.From = "project"
		out = append(out, s)
	}

	// An alias may appear in two groups: nothing rejects it, because a manifest is hand-editable
	// and the uniqueness check lives in `clone`. Qualify only the ambiguous ones, so the common
	// output stays `api.base_branch` and the ambiguous one cannot print two rows under one key.
	seen := map[string]int{}
	for _, ref := range c.Repos() {
		seen[ref.Alias]++
	}
	for _, ref := range c.Repos() {
		name := ref.Alias
		if seen[ref.Alias] > 1 {
			name = ref.Group + "/" + ref.Alias
		}
		for _, s := range resolveWithOrigin(c, ref) {
			// A row earns its place by having an origin below the project, not by holding a
			// different VALUE. A group that deliberately pins the workspace's value — so the
			// family stops following it — is invisible under a value comparison, and the row that
			// does show then credits the project for a value the group owns.
			if s.From == "project" {
				continue
			}
			s.Key = name + "." + s.Key
			out = append(out, s)
		}
	}
	return out
}

// resolveWithOrigin runs the level chain for one repository and returns each effective setting
// tagged with the level that supplied it.
func resolveWithOrigin(c *Config, ref RepoRef) []ResolvedSetting {
	winner := map[string]string{}
	value := map[string]string{}
	order := []string{}

	for _, level := range []struct {
		from string
		d    Defaults
	}{
		{"project", c.Defaults},
		{"group " + ref.Group, c.Groups[ref.Group].Defaults},
		{"repo " + ref.Alias, repoDefaults(ref.Repo)},
	} {
		for _, s := range settingsOf(level.d) {
			if _, seen := value[s.Key]; !seen {
				order = append(order, s.Key)
			}
			value[s.Key], winner[s.Key] = s.Value, level.from
		}
	}

	out := make([]ResolvedSetting, 0, len(order))
	for _, key := range order {
		out = append(out, ResolvedSetting{Key: key, Value: value[key], From: winner[key]})
	}
	return out
}

// settingsOf lists one level's non-empty settings. It is the single place a key name is written,
// so adding a default is one line here and nothing can read a key the constructor does not name.
func settingsOf(d Defaults) []ResolvedSetting {
	var out []ResolvedSetting
	add := func(key, value string) {
		if value != "" {
			out = append(out, ResolvedSetting{Key: key, Value: value})
		}
	}
	add("base_branch", d.BaseBranch)
	naming := d.effectiveNaming()
	if run, ok := naming.Runnable(); ok {
		add("branch_provider", run.Run)
	} else if pattern := naming.Pattern(); pattern != "" {
		add("branch_provider", pattern)
	} else if d.BranchPattern != "" {
		add("branch_pattern", d.BranchPattern)
	}
	// A bool is not a string with an empty case, so it does not pretend to be one.
	if d.BranchPatternStrict {
		add("branch_pattern_strict", "true")
	}
	return out
}

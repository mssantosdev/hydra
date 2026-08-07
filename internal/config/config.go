package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only .hydra/config.yaml schema version this binary understands.
// It is the CONFIG schema version and is unrelated to hydra's release version.
// There is no compatibility layer: older workspaces are re-created by hand.
const SchemaVersion = "2"

// Config represents a hydra project (workspace) configuration.
type Config struct {
	Version  string                     `yaml:"version"`
	Project  string                     `yaml:"project"`
	Paths    Paths                      `yaml:"paths"`
	Groups   map[string]map[string]Repo `yaml:"groups"`
	Defaults Defaults                   `yaml:"defaults,omitempty"`
	Hooks    Hooks                      `yaml:"hooks,omitempty"`

	// Carry names files every worktree in this workspace needs and git ignores. See
	// CarryEntry; resolution appends workspace then repo, and a group level slots in
	// between once a group can hold anything.
	Carry []CarryEntry `yaml:"carry,omitempty"`
}

// Paths holds the project-relative layout knobs.
type Paths struct {
	BareDir string `yaml:"bare_dir"`
}

// Repo describes a repository registered under a group. The map key that points
// at a Repo is the alias, and the alias is the single source of both the bare
// path (<bare_dir>/<alias>.git) and the worktree directory base name.
type Repo struct {
	Remote        string `yaml:"remote"`
	DefaultBranch string `yaml:"default_branch,omitempty"`

	// BranchPattern and BranchProvider override the project defaults for this repo.
	// A repo-level entry wins because a monorepo and a service in the same workspace
	// legitimately name branches differently.
	BranchPattern  string `yaml:"branch_pattern,omitempty"`
	BranchProvider string `yaml:"branch_provider,omitempty"`

	// Branches is the DECLARED shape of this repository: the branches a workspace built
	// from this manifest should have worktrees for. It is what makes the manifest enough
	// to reproduce a setup — `repo restore` creates these, where before it could only
	// create the default branch and had to tell the caller to go find a captured
	// `hydra list --output json` for the rest.
	//
	// It is DECLARED state, so only commands where the user names the set write it:
	// `repo add --branches` and `repo set --branches`. `hydra add` and `hydra start` never
	// touch it, which is what keeps work-in-progress out of a committed file. A branch here
	// that no longer exists on the remote is a warning, never a failure — one stale entry
	// must not stop a workspace from being restored.
	Branches []string `yaml:"branches,omitempty"`

	// Carry is this repository's own ignored-but-required files — the `.env` a Go service
	// needs, the `.env.local` a Bun app needs. Appended after the workspace's.
	Carry []CarryEntry `yaml:"carry,omitempty"`
}

// Defaults holds project-wide defaults.
type Defaults struct {
	BaseBranch string `yaml:"base_branch,omitempty"`

	// BranchPattern is ONE literal string with closed placeholder substitution:
	// {topic} {kind} {slug} {user} {repo} {group}. It is deliberately not a template
	// language — no conditionals, per-kind maps, counters, date arithmetic, nested
	// defaults, alternation or embedded shell. Anything beyond substitution belongs
	// in BranchProvider, so the pattern cannot grow into the workflow DSL that three
	// reviewers rejected outright.
	BranchPattern string `yaml:"branch_pattern,omitempty"`

	// BranchProvider is an executable that receives JSON on stdin and prints one
	// branch name on stdout. It is NOT a lifecycle hook: hooks run after the branch
	// exists, route stdout to stderr, and have no timeout, so they cannot return a
	// value.
	BranchProvider string `yaml:"branch_provider,omitempty"`

	// BranchPatternStrict turns an off-pattern explicit branch into an error instead
	// of a warning. Off by default: branch shape belongs to the team, not to hydra.
	BranchPatternStrict bool `yaml:"branch_pattern_strict,omitempty"`
}

// Hook is a single declarative shell command bound to a lifecycle event.
type Hook struct {
	Run      string `yaml:"run"`
	Optional bool   `yaml:"optional,omitempty"`

	// Timeout bounds this hook, as a Go duration ("30s", "5m"). Empty uses the package
	// default. "0" disables the bound for a hook that genuinely has no upper limit — an
	// explicit choice, where an unbounded hook used to be the only behaviour.
	Timeout string `yaml:"timeout,omitempty"`
}

// Hooks holds the per-event hook chains.
type Hooks struct {
	PostClone  []Hook `yaml:"post_clone,omitempty"`
	PostAdd    []Hook `yaml:"post_add,omitempty"`
	PreRemove  []Hook `yaml:"pre_remove,omitempty"`
	PostRemove []Hook `yaml:"post_remove,omitempty"`
	PostSync   []Hook `yaml:"post_sync,omitempty"`
}

// RepoRef is a flattened (group, alias, repo) tuple.
type RepoRef struct {
	Group string
	Alias string
	Repo  Repo
}

// DefaultConfig returns a new empty project config.
// BaseBranch is intentionally empty so resolution falls through to the repo's
// default_branch and then origin/HEAD.
func DefaultConfig(project string) *Config {
	return &Config{
		Version: SchemaVersion,
		Project: project,
		Paths:   Paths{BareDir: ".bare"},
		Groups:  make(map[string]map[string]Repo),
	}
}

// Save writes the manifest to path, creating its directory when needed.
//
// The manifest lives inside <root>/.hydra/, so the parent may not exist yet on a
// fresh workspace; every caller would otherwise have to MkdirAll first, and one
// forgetting is a confusing "no such file or directory".
//
// It PRESERVES the comments and unrecognised keys of the file it replaces. A plain
// yaml.Marshal of this struct silently deleted both: a manifest carrying
// "# reviewed in PR #412", a ci: block and an owners: list lost all three to a
// single `hydra repo remove`, producing a deletion nobody asked for inside a
// reviewed diff. .hydra/config.yaml is documented as the shareable, committable
// half of the directory, so it has to survive being written by the tool that owns
// it. See mergePreserving for the rules.
func (c *Config) Save(path string) error {
	// Node.Encode yields the MAPPING for c, not a document node — unlike
	// yaml.Unmarshal, which wraps its result in one. Reaching for out.Content[0] here
	// lands on the "version" KEY scalar, and mergePreserving's Kind check then bails
	// silently, so every comment is dropped with no error. Keep the two levels
	// straight: `out` is the mapping, `old.Content[0]` is the mapping inside a
	// document.
	var out yaml.Node
	if err := out.Encode(c); err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// A missing or unreadable prior file is not an error: Save is also how a manifest
	// is created. Only a file we can parse can donate comments.
	if prior, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: path comes from the workspace root, not caller input
		var old yaml.Node
		if yaml.Unmarshal(prior, &old) == nil && len(old.Content) == 1 {
			doc := old.Content[0]
			mergePreserving(doc, &out, reflect.TypeOf(c), sameSchema(doc, c.Version))
		}
	}

	data, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	// The manifest is the shareable half of .hydra/ and may be committed, so it
	// stays world-readable.
	//nolint:gosec // G306: 0644 is deliberate for a repo-tracked config file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// sameSchema reports whether the file on disk declares the version we are writing.
// Unknown keys are carried over only within one schema version: a migration that
// means to DROP a field would otherwise resurrect it on the next write, which is
// the one case where preserving unknowns is wrong.
func sameSchema(doc *yaml.Node, writing string) bool {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value == "version" {
			return doc.Content[i+1].Value == writing
		}
	}
	return false
}

// mergePreserving copies comments — and, inside fixed-field structs, keys this binary
// does not model — from the file on disk onto the document about to be written.
//
// It walks the Go type alongside the nodes because struct and map mean opposite things
// here. A struct has a closed set of fields, so a key on disk that the struct lacks is
// a key we do not understand and must not destroy. A MAP's keys are data: `groups` and
// each group's repo map are exactly the things `repo remove` deletes from, so a key
// missing from the new document means DELETED, and carrying it back would silently undo
// the removal. Comments are carried at every level; unknown keys only where the type
// says the field set is closed.
//
// Sequences are replaced wholesale: a comment inside a list has no stable key to
// reattach to, and guessing by index would move a comment onto a different entry, which
// is worse than dropping it.
func mergePreserving(old, new *yaml.Node, t reflect.Type, carryUnknown bool) {
	if old.Kind != yaml.MappingNode || new.Kind != yaml.MappingNode {
		return
	}
	t = deref(t)
	closedFields := t != nil && t.Kind() == reflect.Struct
	seen := map[string]bool{}
	for i := 0; i+1 < len(new.Content); i += 2 {
		key, val := new.Content[i], new.Content[i+1]
		seen[key.Value] = true
		oldKey, oldVal := lookup(old, key.Value)
		if oldKey == nil {
			continue
		}
		// Comments live on the key node for "# above the field" and on either node for
		// trailing ones, so all three are carried, and only when the new document has
		// none of its own to lose.
		if key.HeadComment == "" {
			key.HeadComment = oldKey.HeadComment
		}
		if key.LineComment == "" {
			key.LineComment = oldKey.LineComment
		}
		if key.FootComment == "" {
			key.FootComment = oldKey.FootComment
		}
		if val.LineComment == "" {
			val.LineComment = oldVal.LineComment
		}
		mergePreserving(oldVal, val, childType(t, key.Value), carryUnknown)
	}
	if !carryUnknown || !closedFields {
		return
	}
	for i := 0; i+1 < len(old.Content); i += 2 {
		key := old.Content[i].Value
		if seen[key] {
			continue
		}
		// UNKNOWN means the struct has no field for it. A key the struct DOES model but left
		// out of the new document was cleared on purpose — `omitempty` makes an empty field
		// and an absent one look identical here — and carrying it back would undo the clearing
		// silently. That is not hypothetical: it briefly hid three call sites replacing a repo
		// entry with a fresh struct, so `branches` and `carry` appeared to survive a
		// re-registration that had in fact dropped them.
		if childType(t, key) != nil {
			continue
		}
		new.Content = append(new.Content, old.Content[i], old.Content[i+1])
	}
}

// childType resolves the Go type behind one key of a mapping: a struct field looked up
// by its yaml tag, or a map's element type. Returning nil means "unknown", which makes
// the level behave as a map — the conservative choice, since it only ever suppresses
// unknown-key carrying and never resurrects a deletion.
func childType(t reflect.Type, key string) reflect.Type {
	t = deref(t)
	if t == nil {
		return nil
	}
	switch t.Kind() {
	case reflect.Map:
		return t.Elem()
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			name, _, _ := strings.Cut(f.Tag.Get("yaml"), ",")
			if name == key {
				return f.Type
			}
		}
	}
	return nil
}

func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// lookup returns the key and value nodes for name in a mapping, or nil.
func lookup(mapping *yaml.Node, name string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

// ErrUnsupportedVersion reports a .hydra/config.yaml whose schema version this binary
// cannot read. It is mapped to the config_version_unsupported error code.
type ErrUnsupportedVersion struct {
	Path    string
	Version string
}

func (e *ErrUnsupportedVersion) Error() string {
	found := e.Version
	if found == "" {
		found = "(none)"
	}
	return fmt.Sprintf(
		"%s declares version %s, but this hydra requires version %s; re-create the workspace with \"hydra init\" (there is no migration path)",
		e.Path, found, SchemaVersion,
	)
}

// Load reads and validates a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path) //nolint:gosec // reading the config path the caller asked for is the whole function
	if err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config %s: %w", path, err)
	}

	if cfg.Version != SchemaVersion {
		return nil, &ErrUnsupportedVersion{Path: path, Version: cfg.Version}
	}

	if cfg.Paths.BareDir == "" {
		cfg.Paths.BareDir = ".bare"
	}
	if cfg.Groups == nil {
		cfg.Groups = make(map[string]map[string]Repo)
	}
	if cfg.Project == "" {
		cfg.Project = filepath.Base(ProjectRoot(path))
	}

	return &cfg, nil
}

// Layout of hydra's per-project files. These are the single source of truth for
// the on-disk names; nothing else may hard-code them. Before this existed the
// manifest filename was duplicated in 22 places across 13 files, which is how it
// became unmovable.
const (
	// StateDir holds every hydra-owned file for a project.
	StateDir = ".hydra"
	// ManifestName is the shared, committable manifest inside StateDir.
	ManifestName = "config.yaml"
)

// ManifestDir returns the hydra directory for a workspace root.
func ManifestDir(root string) string { return filepath.Join(root, StateDir) }

// ManifestPath returns the manifest location for a workspace root.
func ManifestPath(root string) string { return filepath.Join(root, StateDir, ManifestName) }

// ProjectRoot resolves the workspace root that owns a manifest.
//
// The manifest lives at <root>/.hydra/config.yaml, so the root is the parent of
// the .hydra directory — NOT the manifest's own parent. Getting this wrong sends
// every derived path inside .hydra/ (e.g. .hydra/.bare/api.git), which is exactly
// what happened when the manifest moved. A path passed explicitly via --config
// need not sit inside .hydra/, so that case falls back to the parent.
func ProjectRoot(manifestPath string) string {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		abs = manifestPath
	}
	dir := filepath.Dir(abs)
	if filepath.Base(dir) == StateDir {
		return filepath.Dir(dir)
	}
	return dir
}

// FindConfig searches for the manifest walking up from startDir.
func FindConfig(startDir string) (string, *Config, error) {
	dir := startDir
	for {
		configPath := ManifestPath(dir)
		if _, err := os.Stat(configPath); err == nil {
			cfg, err := Load(configPath)
			if err != nil {
				return "", nil, err
			}
			return configPath, cfg, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", nil, fmt.Errorf("no %s found in %s or any parent directory",
		filepath.Join(StateDir, ManifestName), startDir)
}

// FindRepo locates a repo by alias across all groups.
func (c *Config) FindRepo(alias string) (RepoRef, bool) {
	for _, group := range c.SortedGroups() {
		if repo, ok := c.Groups[group][alias]; ok {
			return RepoRef{Group: group, Alias: alias, Repo: repo}, true
		}
	}
	return RepoRef{}, false
}

// Repos returns every registered repo, ordered by group then alias.
func (c *Config) Repos() []RepoRef {
	var refs []RepoRef
	for _, group := range c.SortedGroups() {
		aliases := make([]string, 0, len(c.Groups[group]))
		for alias := range c.Groups[group] {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			refs = append(refs, RepoRef{Group: group, Alias: alias, Repo: c.Groups[group][alias]})
		}
	}
	return refs
}

// SortedGroups returns group names in stable order.
func (c *Config) SortedGroups() []string {
	groups := make([]string, 0, len(c.Groups))
	for name := range c.Groups {
		groups = append(groups, name)
	}
	sort.Strings(groups)
	return groups
}

// SetRepo registers or replaces a repo under a group.
func (c *Config) SetRepo(group, alias string, repo Repo) {
	if c.Groups == nil {
		c.Groups = make(map[string]map[string]Repo)
	}
	if c.Groups[group] == nil {
		c.Groups[group] = make(map[string]Repo)
	}
	c.Groups[group][alias] = repo
}

// RemoveRepo drops a repo, and the group when it becomes empty.
func (c *Config) RemoveRepo(group, alias string) {
	if c.Groups[group] == nil {
		return
	}
	delete(c.Groups[group], alias)
	if len(c.Groups[group]) == 0 {
		delete(c.Groups, group)
	}
}

// BarePath returns the absolute bare repository path for an alias.
func (c *Config) BarePath(projectRoot, alias string) string {
	return filepath.Join(projectRoot, c.Paths.BareDir, alias+".git")
}

// HooksFor returns the hook chain bound to an event name, and whether the event
// name is known.
func (c *Config) HooksFor(event string) ([]Hook, bool) {
	switch event {
	case "post_clone":
		return c.Hooks.PostClone, true
	case "post_add":
		return c.Hooks.PostAdd, true
	case "pre_remove":
		return c.Hooks.PreRemove, true
	case "post_remove":
		return c.Hooks.PostRemove, true
	case "post_sync":
		return c.Hooks.PostSync, true
	}
	return nil, false
}

// HookEvents lists every supported hook event name, in lifecycle order.
func HookEvents() []string {
	return []string{"post_clone", "post_add", "pre_remove", "post_remove", "post_sync"}
}

// RegisterRepo records a repository's remote and default branch WITHOUT discarding anything
// else already recorded for it.
//
// Three call sites used to do `SetRepo(group, alias, Repo{Remote: …, DefaultBranch: …})`,
// which replaces the whole entry. On a first registration that is harmless — there is nothing
// to lose. On a re-registration it silently dropped `branch_pattern` and `branch_provider`,
// and once declarations existed it would have dropped `branches` and `carry` too: `repo
// restore` would strip the very declaration it had just consumed, and a convergent
// `repo add` re-run would strip it on the second call.
//
// A field is only overwritten when the new value is non-empty, so re-registering with an
// unchanged remote cannot blank a default branch that was resolved by an earlier fetch.
func (c *Config) RegisterRepo(group, alias, remote, defaultBranch string) {
	entry := Repo{}
	if ref, ok := c.FindRepo(alias); ok && ref.Group == group {
		entry = ref.Repo
	}
	if remote != "" {
		entry.Remote = remote
	}
	if defaultBranch != "" {
		entry.DefaultBranch = defaultBranch
	}
	c.SetRepo(group, alias, entry)
}

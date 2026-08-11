package config

import (
	"bytes"
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
const SchemaVersion = "3"

// LegacySchemaVersion is the shape hydra wrote before a group could hold anything: `groups`
// mapped a group name straight to its repositories, so there was nowhere to put a group's own
// layout, defaults, hooks or carried files. A version-2 manifest still LOADS — it is renested in
// memory — and is written back as version 3 on the next mutation, so no workspace needs a
// migration step and the upgrade is visible in the diff of a file that was already committed.
const LegacySchemaVersion = "2"

// Config represents a hydra project (workspace) configuration.
type Config struct {
	Version  string           `yaml:"version"`
	Project  string           `yaml:"project"`
	Paths    Paths            `yaml:"paths"`
	Groups   map[string]Group `yaml:"groups"`
	Defaults Defaults         `yaml:"defaults,omitempty"`
	Hooks    Hooks            `yaml:"hooks,omitempty"`

	// Carry names files every worktree in this workspace needs and git ignores. See
	// CarryEntry; resolution appends workspace then repo, and a group level slots in
	// between once a group can hold anything.
	Carry []CarryEntry `yaml:"carry,omitempty"`
}

// Paths holds the project-relative layout knobs.
type Paths struct {
	BareDir string `yaml:"bare_dir"`
}

// Group is a partition of the workspace and everything that applies to the repositories in it.
//
// It became an object because it was the only noun in the model with nowhere to put anything: a
// bare map key, no properties, no command. Yet the override chain ran project → repo, skipping the
// level that means "these repositories belong together" — so `branch_pattern` had to be repeated
// on every repo in a family, and `base_branch` could not vary below the project at all.
//
// hydra does NOT decide what a group means. `go-projects`/`java-projects` and `backend`/`web` are
// equally valid partitions, so every level carries the same keys and the resolution chain is the
// only rule; what belongs where is the user's modelling choice.
type Group struct {
	// Path places this group's worktrees, relative to the workspace root. Empty defaults to the
	// group name.
	//
	// This is why group names stay one path segment and `/` is still rejected in them: a slash in
	// the KEY made the selector, completion and rename semantics ambiguous, where a path field is
	// unambiguous and does the same job. `platform/infra` belongs here, not in the name.
	Path string `yaml:"path,omitempty"`

	Defaults Defaults     `yaml:"defaults,omitempty"`
	Hooks    Hooks        `yaml:"hooks,omitempty"`
	Carry    []CarryEntry `yaml:"carry,omitempty"`

	// Repos maps alias → repository. The alias is the key because it is the single source of the
	// bare path and the worktree directory base name.
	Repos map[string]Repo `yaml:"repos"`
}

// Dir returns the directory this group's worktrees live under, relative to the workspace root.
func (g Group) Dir(name string) string {
	if g.Path != "" {
		return g.Path
	}
	return name
}

// UnmarshalYAML accepts both shapes so a version-2 manifest keeps working.
//
// The two are told apart structurally rather than by consulting the version: a v3 group is a
// mapping with a `repos` key, a v2 group is a mapping of aliases straight to repositories. Reading
// the version would mean decoding the document twice, and a manifest whose `version` disagrees with
// its own shape is exactly the half-migrated state doctor has to be able to report.
func (g *Group) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("group must be a mapping, got %v", value.Kind)
	}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if value.Content[i].Value == "repos" {
			type raw Group
			var r raw
			if err := value.Decode(&r); err != nil {
				return err
			}
			*g = Group(r)
			return nil
		}
	}
	// Legacy: the whole mapping is the repo set.
	var repos map[string]Repo
	if err := value.Decode(&repos); err != nil {
		return err
	}
	*g = Group{Repos: repos}
	return nil
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
	// from this manifest should have worktrees for. `repo restore` creates these from the
	// manifest alone — without them only the default branch can be reproduced.
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

	// Defaults and Hooks are the repo's slots in the level model — the same two keys a group
	// and the workspace carry.
	//
	// BranchPattern and BranchProvider above are the flat spelling of the same policy, kept
	// because manifests in use reference them by name. repoDefaults merges the two, this block
	// winning.
	Defaults Defaults `yaml:"defaults,omitempty"`
	Hooks    Hooks    `yaml:"hooks,omitempty"`
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
	// default. "0" explicitly disables the bound for a hook with no upper limit.
	Timeout string `yaml:"timeout,omitempty"`
}

// Hooks holds the per-event hook chains.
type Hooks struct {
	PostClone  []Hook `yaml:"post_clone,omitempty"`
	PostAdd    []Hook `yaml:"post_add,omitempty"`
	PreRemove  []Hook `yaml:"pre_remove,omitempty"`
	PostRemove []Hook `yaml:"post_remove,omitempty"`
	PostSync   []Hook `yaml:"post_sync,omitempty"`

	// Topic events fire ONCE PER OPERATION, not once per worktree. Wiring a notification to
	// post_add posts N times for one piece of work; a run_once flag would paper over assigning
	// an operation-scoped hook to a per-worktree event. The level says whose config a hook is;
	// the event shape says how often it fires.
	//
	// PreTopicClose is the quality-gate hook: it runs while work exists and can veto finishing
	// a unit of work. post_add fires before any work exists; pre_remove fires during teardown.
	PostTopicStart []Hook `yaml:"post_topic_start,omitempty"`
	PreTopicClose  []Hook `yaml:"pre_topic_close,omitempty"`
	PostTopicClose []Hook `yaml:"post_topic_close,omitempty"`
	PreTopicRemove []Hook `yaml:"pre_topic_remove,omitempty"`
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
		Groups:  make(map[string]Group),
	}
}

// Save writes the manifest to path, creating its directory when needed.
//
// The manifest lives inside <root>/.hydra/, so the parent may not exist yet on a
// fresh workspace; every caller would otherwise have to MkdirAll first, and one
// forgetting is a confusing "no such file or directory".
//
// It PRESERVES the comments and unrecognised keys of the file it replaces. Plain
// yaml.Marshal drops both without error — annotations and extension keys would vanish on
// every write. .hydra/config.yaml is documented as the shareable, committable half of
// the directory, so it has to survive being written by the tool that owns it. See
// mergePreserving for the rules.
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
	merged := false
	if prior, err := os.ReadFile(path); err == nil { //nolint:gosec // G304: path comes from the workspace root, not caller input
		var old yaml.Node
		if yaml.Unmarshal(prior, &old) == nil && len(old.Content) == 1 {
			doc := old.Content[0]
			carry := sameSchema(doc, c.Version)
			normalizeLegacyGroups(doc)
			mergePreserving(doc, &out, reflect.TypeOf(c), carry)
			out.HeadComment = joinComments(old.HeadComment, out.HeadComment)
			out.FootComment = joinComments(out.FootComment, old.FootComment)
			merged = true
		}
	}

	data, err := yaml.Marshal(&out)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// The merge grafts nodes from the old file onto the new document, so a pathological input
	// could in principle produce bytes that no longer parse or no longer describe this Config.
	// Preserving annotations is worth a lot; it is not worth any chance of leaving a workspace
	// that hydra itself cannot load, where every later command — `doctor` included — can only
	// report not_in_project. A plain marshal always round-trips, so fall back to it and lose
	// the comments instead.
	if merged {
		if plain, ok := verifyRoundTrip(data, c); !ok {
			data = plain
		}
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

// verifyRoundTrip checks that the merged bytes still parse and still describe the same manifest.
// It returns a plain marshal of c and false when they do not, so the caller can write something
// loadable rather than something annotated.
//
// Equality is by re-marshalling both through the struct, which compares MEANING rather than
// layout: the merged file legitimately differs in comments, key order and anchors.
func verifyRoundTrip(data []byte, c *Config) ([]byte, bool) {
	plain, err := yaml.Marshal(c)
	if err != nil {
		// c itself cannot be marshalled, so there is no safe alternative to offer.
		return nil, true
	}
	var probe Config
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return plain, false
	}
	again, err := yaml.Marshal(&probe)
	if err != nil {
		return plain, false
	}
	return plain, bytes.Equal(again, plain)
}

// joinComments concatenates two comment blocks, dropping empties, so a document-level block and
// a generated one can coexist without a blank comment line between them.
func joinComments(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + "\n" + second
	}
}

// sameSchema reports whether the file on disk declares the version we are writing.
// Unknown keys are carried over only within one schema version: a migration that
// means to DROP a field would otherwise resurrect it on the next write, which is
// the one case where preserving unknowns is wrong.
func sameSchema(doc *yaml.Node, writing string) bool {
	for i := 0; i+1 < len(doc.Content); i += 2 {
		if doc.Content[i].Value != "version" {
			continue
		}
		found := doc.Content[i+1].Value
		if found == writing {
			return true
		}
		// The 2 → 3 change RENESTS groups and removes no field, so a key this binary does not
		// model is just as unknown afterwards and dropping it on the upgrade would be the same
		// silent data loss this merge exists to prevent. A future migration that genuinely means
		// to drop a field must exclude itself here, deliberately.
		return found == LegacySchemaVersion && writing == SchemaVersion
	}
	return false
}

// normalizeLegacyGroups rewrites a version-2 group mapping into the version-3 shape IN THE OLD
// NODE, before the merge runs.
//
// Without it the upgrade loses every comment attached to a repository entry: `groups.svc.api` moves
// to `groups.svc.repos.api`, so the merge looks for `api` beside `path`/`defaults`/`repos` and finds
// nothing. Aligning the shapes first keeps the merge generic — it never learns about versions — and
// means the one commit where a manifest is most likely to be annotated is not the one that eats the
// annotations.
func normalizeLegacyGroups(doc *yaml.Node) {
	_, groups := lookup(doc, "groups")
	if groups == nil || groups.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(groups.Content); i += 2 {
		group := groups.Content[i+1]
		if group.Kind != yaml.MappingNode {
			continue
		}
		if key, _ := lookup(group, "repos"); key != nil {
			continue // already v3
		}
		inner := &yaml.Node{Kind: yaml.MappingNode, Tag: group.Tag, Content: group.Content}
		group.Content = []*yaml.Node{
			{Kind: yaml.ScalarNode, Tag: "!!str", Value: "repos"},
			inner,
		}
	}
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
	closed := t != nil && t.Kind() == reflect.Struct
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
		// The anchor has to survive with the value. Re-encoding a modelled key from the struct
		// drops any `&name` it carried, and an unmodelled key carried verbatim below may hold
		// the matching `*name` — which would then reference an anchor no longer in the file,
		// making the manifest unparseable rather than merely lossy.
		if val.Anchor == "" {
			val.Anchor = oldVal.Anchor
		}
		mergePreserving(oldVal, val, childType(t, key.Value), carryUnknown)
		copySequenceComments(oldVal, val)
	}
	if !carryUnknown && !closed {
		return
	}
	for i := 0; i+1 < len(old.Content); i += 2 {
		oldKey, oldVal := old.Content[i], old.Content[i+1]
		if seen[oldKey.Value] {
			continue
		}
		modelled := childType(t, oldKey.Value) != nil
		switch {
		case !modelled:
			// UNKNOWN means the struct has no field for it, so nothing in this binary asked for
			// it to go away. Only inside a closed field set: a MAP's keys are data, and `groups`
			// and each repo map are exactly what `repo remove` deletes from, so a key missing
			// there means DELETED.
			if !carryUnknown || !closed {
				continue
			}
		case closed && isEmptyNode(oldVal) && hasComment(oldKey, oldVal):
			// A STRUCT field the new document omitted, whose old value was already empty and
			// carried annotation. `omitempty` makes empty and absent identical on the way out,
			// so re-emitting the empty value resurrects no data, and it is the only way an
			// annotated `base_branch: ""` keeps the comment that explains it.
			//
			// `closed` is what keeps this off map levels: childType answers Elem() for ANY key
			// of a map, so without it a commented empty group would come back after `repo
			// remove` deleted it.
		default:
			// Modelled, and either non-empty or unannotated: absent means cleared on purpose.
			continue
		}
		new.Content = append(new.Content, oldKey, oldVal)
	}
}

// copySequenceComments carries comments into a list, but ONLY when the list is byte-for-byte the
// same data it was.
//
// A list entry has no key to match on, so the merge cannot reattach a comment the way it does for
// a mapping. Matching by position is exact when nothing about the list changed, and wrong the
// moment an entry is added, removed or reordered — a comment would migrate to a neighbour and
// state something false about it. Requiring identical shape first buys the common case (a
// hand-annotated `hooks:` or `carry:` block that no command touched) without ever guessing:
// `repo add --branches` rewrites `branches`, and there the comments are dropped as before.
func copySequenceComments(old, new *yaml.Node) {
	if old == nil || new == nil || old.Kind != yaml.SequenceNode || new.Kind != yaml.SequenceNode {
		return
	}
	if !sameShape(old, new) {
		return
	}
	copyCommentsInPlace(old, new)
}

// sameShape reports whether two nodes hold identical data, ignoring comments, anchors and style.
func sameShape(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || len(a.Content) != len(b.Content) {
		return false
	}
	for i := range a.Content {
		if !sameShape(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// copyCommentsInPlace walks two structurally identical trees and fills empty comments on the
// second from the first.
func copyCommentsInPlace(old, new *yaml.Node) {
	if old == nil || new == nil {
		return
	}
	if new.HeadComment == "" {
		new.HeadComment = old.HeadComment
	}
	if new.LineComment == "" {
		new.LineComment = old.LineComment
	}
	if new.FootComment == "" {
		new.FootComment = old.FootComment
	}
	for i := range new.Content {
		if i < len(old.Content) {
			copyCommentsInPlace(old.Content[i], new.Content[i])
		}
	}
}

// isEmptyNode reports whether a node carries no data: an empty scalar, or a collection whose
// every value is itself empty. Such a node is indistinguishable from an absent key once the
// manifest is decoded, which is what makes re-emitting it safe.
func isEmptyNode(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value == "" || n.Tag == "!!null"
	case yaml.MappingNode:
		for i := 1; i < len(n.Content); i += 2 {
			if !isEmptyNode(n.Content[i]) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		return len(n.Content) == 0
	default:
		// An alias resolves to data this function cannot see, so it is never empty.
		return false
	}
}

// hasComment reports whether a key/value pair carries annotation worth keeping, anywhere in the
// pair's subtree.
//
// It has to look inside: the annotation on an empty block usually sits on the inner keys, as in
//
//	defaults:
//	  # Base branch for new worktrees when --from is not passed.
//	  base_branch: ""
//
// where `defaults` itself is bare. Checking only the outer pair would drop exactly the comments
// that explain the empty value.
func hasComment(nodes ...*yaml.Node) bool {
	for _, n := range nodes {
		if n == nil {
			continue
		}
		if n.HeadComment != "" || n.LineComment != "" || n.FootComment != "" {
			return true
		}
		if hasComment(n.Content...) {
			return true
		}
	}
	return false
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

	// A version-2 manifest LOADS. Its only difference is that a group maps straight to its
	// repositories, which Group.UnmarshalYAML renests, so nothing else in the document changes
	// meaning. Refusing it would have made every command fail until the user ran a migration for
	// a purely structural change — and the next mutation writes version 3 anyway, so the upgrade
	// lands as a one-line diff in a file that was already committed.
	//
	// Anything OLDER or NEWER is still refused: the gate exists so a manifest written by a hydra
	// that knows more than this one is never silently half-read.
	if cfg.Version != SchemaVersion && cfg.Version != LegacySchemaVersion {
		return nil, &ErrUnsupportedVersion{Path: path, Version: cfg.Version}
	}
	cfg.Version = SchemaVersion

	if cfg.Paths.BareDir == "" {
		cfg.Paths.BareDir = ".bare"
	}
	if cfg.Groups == nil {
		cfg.Groups = make(map[string]Group)
	}
	if cfg.Project == "" {
		cfg.Project = filepath.Base(ProjectRoot(path))
	}

	return &cfg, nil
}

// Layout of hydra's per-project files. These are the single source of truth for the
// on-disk names; nothing else may hard-code them.
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
// the .hydra directory — NOT the manifest's own parent. Treating the manifest's parent
// as the root would nest every derived path inside .hydra/ (e.g. .hydra/.bare/api.git).
// A path passed explicitly via --config need not sit inside .hydra/, so that case falls
// back to the parent.
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
		if repo, ok := c.Groups[group].Repos[alias]; ok {
			return RepoRef{Group: group, Alias: alias, Repo: repo}, true
		}
	}
	return RepoRef{}, false
}

// Repos returns every registered repo, ordered by group then alias.
func (c *Config) Repos() []RepoRef {
	var refs []RepoRef
	for _, group := range c.SortedGroups() {
		aliases := make([]string, 0, len(c.Groups[group].Repos))
		for alias := range c.Groups[group].Repos {
			aliases = append(aliases, alias)
		}
		sort.Strings(aliases)
		for _, alias := range aliases {
			refs = append(refs, RepoRef{Group: group, Alias: alias, Repo: c.Groups[group].Repos[alias]})
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
		c.Groups = make(map[string]Group)
	}
	entry := c.Groups[group]
	if entry.Repos == nil {
		entry.Repos = make(map[string]Repo)
	}
	entry.Repos[alias] = repo
	c.Groups[group] = entry
}

// RemoveRepo drops a repo, and the group when it becomes empty.
func (c *Config) RemoveRepo(group, alias string) {
	if c.Groups[group].Repos == nil {
		return
	}
	delete(c.Groups[group].Repos, alias)
	// A group with no repositories left is removed entirely, including whatever it declared. Its
	// defaults and carried files describe repositories that are no longer there.
	if len(c.Groups[group].Repos) == 0 {
		delete(c.Groups, group)
	}
}

// BarePath returns the absolute bare repository path for an alias.
func (c *Config) BarePath(projectRoot, alias string) string {
	return filepath.Join(projectRoot, c.Paths.BareDir, alias+".git")
}

// HooksFor returns the WORKSPACE chain bound to an event name, and whether the event name is
// known. Callers that RUN hooks for a repository want ResolveHooks; this form is for validating
// an event name.
func (c *Config) HooksFor(event string) ([]Hook, bool) {
	return hooksOf(c.Hooks, event)
}

// ResolveHooks returns the chain to run for one event and one repository, appended
// workspace → group → repo.
//
// Lists append rather than override, so a workspace-wide `direnv allow`, a group's shared
// bring-up and a repo's `go mod download` all run. Overriding would force every child to
// restate what it inherits.
func ResolveHooks(c *Config, alias, event string) ([]Hook, bool) {
	if c == nil {
		return nil, false
	}
	chain, known := hooksOf(c.Hooks, event)
	if !known {
		return nil, false
	}
	out := make([]Hook, 0, len(chain))
	out = append(out, chain...)

	ref, ok := c.FindRepo(alias)
	if !ok {
		// No repository in scope — a topic event, or a caller that named none. The workspace
		// chain is the whole answer.
		return out, true
	}
	for _, level := range []Hooks{c.Groups[ref.Group].Hooks, ref.Repo.Hooks} {
		if hs, _ := hooksOf(level, event); len(hs) > 0 {
			out = append(out, hs...)
		}
	}
	return out, true
}

// hooksOf binds an event name to one level's chain — the single place names map to fields, so a
// new event is added in one switch.
func hooksOf(h Hooks, event string) ([]Hook, bool) {
	switch event {
	case "post_clone":
		return h.PostClone, true
	case "post_add":
		return h.PostAdd, true
	case "pre_remove":
		return h.PreRemove, true
	case "post_remove":
		return h.PostRemove, true
	case "post_sync":
		return h.PostSync, true
	case "post_topic_start":
		return h.PostTopicStart, true
	case "pre_topic_close":
		return h.PreTopicClose, true
	case "post_topic_close":
		return h.PostTopicClose, true
	case "pre_topic_remove":
		return h.PreTopicRemove, true
	}
	return nil, false
}

// HookEvents lists every supported hook event name, in lifecycle order.
func HookEvents() []string {
	return []string{
		"post_clone", "post_add", "pre_remove", "post_remove", "post_sync",
		"post_topic_start", "pre_topic_close", "post_topic_close", "pre_topic_remove",
	}
}

// RegisterRepo records a repository's remote and default branch WITHOUT discarding anything
// else already recorded for it.
//
// SetRepo replaces the whole entry. A fresh Repo{Remote, DefaultBranch} discards anything
// else already recorded — branch_pattern, branch_provider, branches, and carry on
// re-registration. RegisterRepo merges into the existing entry instead.
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

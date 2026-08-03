package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// SchemaVersion is the only .hydra.yaml schema version this binary understands.
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
}

// Defaults holds project-wide defaults.
type Defaults struct {
	BaseBranch string `yaml:"base_branch,omitempty"`
}

// Hook is a single declarative shell command bound to a lifecycle event.
type Hook struct {
	Run      string `yaml:"run"`
	Optional bool   `yaml:"optional,omitempty"`
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

// Save writes the config to path.
func (c *Config) Save(path string) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	// .hydra.yaml is committed to the repo, so it must stay world-readable.
	//nolint:gosec // G306: 0644 is deliberate for a repo-tracked config file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// ErrUnsupportedVersion reports a .hydra.yaml whose schema version this binary
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
		cfg.Project = filepath.Base(filepath.Dir(path))
	}

	return &cfg, nil
}

// FindConfig searches for .hydra.yaml walking up from startDir.
func FindConfig(startDir string) (string, *Config, error) {
	dir := startDir
	for {
		configPath := filepath.Join(dir, ".hydra.yaml")
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
	return "", nil, fmt.Errorf("no .hydra.yaml found in %s or any parent directory", startDir)
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

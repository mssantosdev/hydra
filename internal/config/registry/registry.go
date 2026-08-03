// Package registry stores the global map of hydra project names to workspace roots.
package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/mssantosdev/hydra/internal/config/global"
)

// SchemaVersion is the registry file schema version.
const SchemaVersion = "1"

// Registry maps project names to absolute workspace roots.
type Registry struct {
	Version  string            `yaml:"version"`
	Projects map[string]string `yaml:"projects"`
}

// Path returns the registry file location.
func Path() string {
	return filepath.Join(global.GetConfigDir(), "projects.yaml")
}

// Load reads the registry, returning an empty one when the file is absent.
func Load() (*Registry, error) {
	path := Path()
	//nolint:gosec // G304: path comes from GetConfigDir, not caller input
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Registry{Version: SchemaVersion, Projects: map[string]string{}}, nil
		}
		return nil, fmt.Errorf("failed to read project registry: %w", err)
	}

	var r Registry
	if err := yaml.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("failed to parse project registry %s: %w", path, err)
	}
	if r.Projects == nil {
		r.Projects = map[string]string{}
	}
	r.Version = SchemaVersion
	return &r, nil
}

// Save writes the registry to disk.
func (r *Registry) Save() error {
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	r.Version = SchemaVersion
	data, err := yaml.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to marshal project registry: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write project registry: %w", err)
	}
	return nil
}

// Add registers a project root. Re-registering the same root is a no-op;
// registering a different root under an existing name is an error.
func (r *Registry) Add(name, root string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve project root: %w", err)
	}
	if r.Projects == nil {
		r.Projects = map[string]string{}
	}
	if existing, ok := r.Projects[name]; ok && existing != abs {
		return fmt.Errorf("project %q is already registered at %s", name, existing)
	}
	r.Projects[name] = abs
	return nil
}

// Remove drops a project entry.
func (r *Registry) Remove(name string) error {
	if _, ok := r.Projects[name]; !ok {
		return fmt.Errorf("project %q is not registered", name)
	}
	delete(r.Projects, name)
	return nil
}

// Resolve returns the workspace root registered under name.
func (r *Registry) Resolve(name string) (string, bool) {
	root, ok := r.Projects[name]
	return root, ok
}

// Names returns registered project names in stable order.
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.Projects))
	for name := range r.Projects {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Prune drops entries whose root no longer holds a .hydra.yaml and returns the
// removed project names.
func (r *Registry) Prune() []string {
	var removed []string
	for _, name := range r.Names() {
		if _, err := os.Stat(filepath.Join(r.Projects[name], ".hydra.yaml")); err != nil {
			removed = append(removed, name)
			delete(r.Projects, name)
		}
	}
	return removed
}

// Register is the convenience path used by init/new/clone/adopt: load, add, save.
func Register(name, root string) error {
	r, err := Load()
	if err != nil {
		return err
	}
	if err := r.Add(name, root); err != nil {
		return err
	}
	return r.Save()
}

package config

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// BranchRunnable is the executable form of branch_provider: an argv with an optional
// timeout, spelled like a hook.
type BranchRunnable struct {
	Run     string `yaml:"run"`
	Timeout string `yaml:"timeout,omitempty"`
}

// BranchNaming is how a branch name is chosen: a pattern string or a runnable provider.
//
// A scalar is placeholder substitution; a mapping with run: executes a workspace-relative
// binary. The shape announces which one applies.
type BranchNaming struct {
	pattern  string
	runnable *BranchRunnable
}

// IsZero reports whether no branch naming policy is configured.
func (b BranchNaming) IsZero() bool {
	return b.pattern == "" && b.runnable == nil
}

// Pattern returns the pattern form, if any.
func (b BranchNaming) Pattern() string { return b.pattern }

// Runnable returns the executable form, if any.
func (b BranchNaming) Runnable() (BranchRunnable, bool) {
	if b.runnable == nil {
		return BranchRunnable{}, false
	}
	return *b.runnable, true
}

// UnmarshalYAML accepts a scalar pattern or a mapping with run:.
func (b *BranchNaming) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		var pattern string
		if err := value.Decode(&pattern); err != nil {
			return err
		}
		b.pattern = strings.TrimSpace(pattern)
		b.runnable = nil
		return nil
	case yaml.MappingNode:
		// A named type avoids recursing into this method. yaml ignores unknown sub-keys on
		// decode, so a mapping with no run: must be refused here — a successful decode is
		// not proof the shape was right.
		type raw BranchRunnable
		var run raw
		if err := value.Decode(&run); err != nil {
			return fmt.Errorf("line %d: branch_provider: %w", value.Line, err)
		}
		run.Run = strings.TrimSpace(run.Run)
		if run.Run == "" {
			return fmt.Errorf("line %d: branch_provider mapping requires run", value.Line)
		}
		b.pattern = ""
		b.runnable = (*BranchRunnable)(&run)
		return nil
	default:
		return fmt.Errorf("line %d: branch_provider must be a string or mapping", value.Line)
	}
}

// MarshalYAML writes the scalar form back as a scalar and the runnable form as a mapping.
func (b BranchNaming) MarshalYAML() (any, error) {
	if b.runnable != nil {
		type raw BranchRunnable
		return raw(*b.runnable), nil
	}
	if b.pattern != "" {
		return b.pattern, nil
	}
	return nil, nil
}

// effectiveNaming returns the branch naming policy for one Defaults block, treating the
// deprecated branch_pattern alias as the scalar form of branch_provider.
func (d Defaults) effectiveNaming() BranchNaming {
	if !d.BranchProvider.IsZero() {
		return d.BranchProvider
	}
	if d.BranchPattern != "" {
		return BranchNaming{pattern: d.BranchPattern}
	}
	return BranchNaming{}
}

// BranchNamingPolicy returns the pattern and runnable provider strings resolved from this
// Defaults block. The runnable path is workspace-relative; callers run it with the project
// root as the working directory.
func (d Defaults) BranchNamingPolicy() (pattern, provider string, timeout time.Duration) {
	naming := d.effectiveNaming()
	if run, ok := naming.Runnable(); ok {
		provider = run.Run
		if run.Timeout != "" {
			parsed, err := time.ParseDuration(run.Timeout)
			if err == nil {
				timeout = parsed
			}
		}
		return "", provider, timeout
	}
	return naming.Pattern(), "", 0
}

// ErrConfigInvalid is a manifest value hydra refuses to act on.
type ErrConfigInvalid struct {
	Field string
	Msg   string
}

func (e *ErrConfigInvalid) Error() string {
	if e.Msg != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Msg)
	}
	return e.Field + " is invalid"
}

func validateBranchNaming(field string, d Defaults) error {
	if d.BranchPattern != "" && !d.BranchProvider.IsZero() {
		return &ErrConfigInvalid{
			Field: field,
			Msg:   "branch_pattern and branch_provider cannot both be set; use branch_provider only",
		}
	}
	naming := d.effectiveNaming()
	if d.BranchPatternStrict {
		if _, ok := naming.Runnable(); ok {
			return &ErrConfigInvalid{
				Field: field + ".branch_pattern_strict",
				Msg:   "applies only to the pattern form of branch_provider",
			}
		}
	}
	if run, ok := naming.Runnable(); ok {
		if err := validateProviderRun(field+".branch_provider.run", run.Run); err != nil {
			return err
		}
		if run.Timeout != "" {
			if _, err := time.ParseDuration(run.Timeout); err != nil {
				return &ErrConfigInvalid{
					Field: field + ".branch_provider.timeout",
					Msg:   fmt.Sprintf("invalid duration %q", run.Timeout),
				}
			}
		}
	}
	return nil
}

// validateProviderRun requires a workspace-relative path, not a bare command name that
// would resolve through the caller's PATH.
func validateProviderRun(field, run string) error {
	run = strings.TrimSpace(run)
	if run == "" {
		return nil
	}
	if run[0] != '.' && !strings.ContainsAny(run, `/\`) {
		return &ErrConfigInvalid{
			Field: field,
			Msg:   "must be a workspace-relative path, not a bare command name",
		}
	}
	path := strings.TrimPrefix(run, "./")
	return checkContainedPath(field, path)
}

func (c *Config) validateBranchNaming() error {
	if err := validateBranchNaming("defaults", c.Defaults); err != nil {
		return err
	}
	for _, groupName := range sortedGroupNames(c) {
		group := c.Groups[groupName]
		field := fmt.Sprintf("groups.%s.defaults", groupName)
		if err := validateBranchNaming(field, group.Defaults); err != nil {
			return err
		}
		for alias, repo := range group.Repos {
			repoField := fmt.Sprintf("groups.%s.repos.%s", groupName, alias)
			if err := validateBranchNaming(repoField, repoDefaults(repo)); err != nil {
				return err
			}
			if repo.BranchPattern != "" && !repo.BranchProvider.IsZero() {
				return &ErrConfigInvalid{
					Field: repoField,
					Msg:   "branch_pattern and branch_provider cannot both be set; use branch_provider only",
				}
			}
		}
	}
	return nil
}

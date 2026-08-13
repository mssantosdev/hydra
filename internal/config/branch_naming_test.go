package config

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBranchNamingUnmarshalYAML(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		yaml    string
		wantPat string
		wantRun string
		wantErr string
	}{
		{
			name:    "scalar pattern",
			yaml:    `branch_provider: "feat/{topic}-{slug}"`,
			wantPat: "feat/{topic}-{slug}",
		},
		{
			name:    "runnable with timeout",
			yaml:    "branch_provider:\n  run: ./scripts/name-branch\n  timeout: 10s\n",
			wantRun: "./scripts/name-branch",
		},
		{
			name:    "mapping without run refused",
			yaml:    "branch_provider:\n  timeout: 10s\n",
			wantErr: "requires run",
		},
		{
			name:    "list refused",
			yaml:    "branch_provider: [oops]",
			wantErr: "must be a string or mapping",
		},
		{
			name:    "deprecated branch_pattern only",
			yaml:    `branch_pattern: "{kind}/{slug}"`,
			wantPat: "{kind}/{slug}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var d Defaults
			err := yaml.Unmarshal([]byte(tc.yaml), &d)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			naming := d.effectiveNaming()
			if tc.wantRun != "" {
				run, ok := naming.Runnable()
				if !ok || run.Run != tc.wantRun {
					t.Fatalf("runnable = %+v, ok=%v, want run %q", run, ok, tc.wantRun)
				}
				return
			}
			if got := naming.Pattern(); got != tc.wantPat {
				t.Fatalf("pattern = %q, want %q", got, tc.wantPat)
			}
		})
	}
}

func TestLoadBranchNamingValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		body    string
		field   string
		wantErr string
	}{
		{
			name: "both branch fields set",
			body: `version: "3"
project: p
paths:
  bare_dir: .bare
defaults:
  branch_pattern: "{kind}/{slug}"
  branch_provider: "feat/{slug}"
`,
			field:   "defaults",
			wantErr: "branch_pattern and branch_provider cannot both be set",
		},
		{
			name: "strict with runnable refused",
			body: `version: "3"
project: p
paths:
  bare_dir: .bare
defaults:
  branch_pattern_strict: true
  branch_provider:
    run: ./scripts/name-branch
`,
			field:   "defaults.branch_pattern_strict",
			wantErr: "applies only to the pattern form",
		},
		{
			name: "bare run name refused",
			body: `version: "3"
project: p
paths:
  bare_dir: .bare
defaults:
  branch_provider:
    run: name-branch
`,
			field:   "defaults.branch_provider.run",
			wantErr: "workspace-relative path",
		},
		{
			name: "workspace-relative run accepted",
			body: `version: "3"
project: p
paths:
  bare_dir: .bare
defaults:
  branch_provider:
    run: ./scripts/name-branch
`,
		},
		{
			name: "deprecated branch_pattern only still loads",
			body: `version: "3"
project: p
paths:
  bare_dir: .bare
defaults:
  branch_pattern: "{kind}/{slug}"
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := manifestAt(t, tc.body)
			cfg, err := Load(path)
			if tc.wantErr != "" {
				var invalid *ErrConfigInvalid
				if !errors.As(err, &invalid) {
					t.Fatalf("Load() err = %v, want ErrConfigInvalid", err)
				}
				if tc.field != "" && invalid.Field != tc.field {
					t.Fatalf("field = %q, want %q", invalid.Field, tc.field)
				}
				if !strings.Contains(invalid.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want substring %q", invalid, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if tc.name == "deprecated branch_pattern only still loads" {
				if got, _, _ := cfg.Defaults.BranchNamingPolicy(); got != "{kind}/{slug}" {
					t.Fatalf("policy pattern = %q", got)
				}
			}
		})
	}
}

func TestBranchNamingMarshalYAMLRoundTrip(t *testing.T) {
	for _, prior := range []string{
		`version: "3"
project: p
paths:
    bare_dir: .bare
defaults:
    # team naming convention
    branch_provider: "feat/{slug}"
`,
		`version: "3"
project: p
paths:
    bare_dir: .bare
defaults:
    branch_provider:
        run: ./scripts/name-branch
        timeout: 10s
`,
	} {
		t.Run(strings.TrimSpace(strings.Split(prior, "\n")[0]), func(t *testing.T) {
			got, reloaded := saveInto(t, prior, func(c *Config) { c.Project = "renamed" })
			if !strings.Contains(got, "branch_provider") {
				t.Fatalf("branch_provider key missing:\n%s", got)
			}
			if reloaded.Defaults.BranchProvider.IsZero() && reloaded.Defaults.BranchPattern == "" {
				t.Fatal("branch naming policy was dropped on round trip")
			}
		})
	}
}

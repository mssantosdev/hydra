package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestValidateRelativeProjectPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "nested relative path", input: "test/test-123", want: filepath.Clean("test/test-123")},
		{name: "absolute path rejected", input: "/tmp/test", wantErr: true},
		{name: "parent escape rejected", input: "../test", wantErr: true},
		{name: "empty rejected", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateRelativeProjectPath(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("validateRelativeProjectPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGenerateShellHelperDoesNotTruncateMktempFile(t *testing.T) {
	helper := generateBashZshHelper()
	if strings.Contains(helper, `: > "$output_file"`) {
		t.Fatal("bash helper should not truncate mktemp output file")
	}
	if strings.Contains(helper, `printf '' > "$output_file"`) {
		t.Fatal("shell helper should not clobber output file before switch")
	}
}

func TestBootstrapLocalProject(t *testing.T) {
	env := testutil.NewTestEnv(t)
	projectRoot, configPath, cfg, err := createProjectRoot(env.RootDir, "test/test-123")
	if err != nil {
		t.Fatalf("createProjectRoot failed: %v", err)
	}

	opts := &newProjectOptions{
		ProjectPath:   "test/test-123",
		Mode:          "local",
		Group:         "backend",
		Alias:         "api",
		LocalRepoName: "api-repo",
		InitialBranch: "main",
	}

	if err := bootstrapLocalProject(projectRoot, configPath, cfg, opts); err != nil {
		t.Fatalf("bootstrapLocalProject failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".hydra.yaml")); err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, ".bare", "api.git")); err != nil {
		t.Fatalf("expected bare repo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(projectRoot, "api-repo", ".git")); err != nil {
		t.Fatalf("expected local repo: %v", err)
	}
	if info, err := os.Lstat(filepath.Join(projectRoot, "backend", "api")); err != nil {
		t.Fatalf("expected symlink for main branch: %v", err)
	} else if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected backend/api to be a symlink, got %v", info.Mode())
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed reading config: %v", err)
	}
	configContent := string(content)
	if !strings.Contains(configContent, "backend") || !strings.Contains(configContent, "api-repo") {
		t.Fatalf("config missing registered repo: %s", configContent)
	}
}

func TestCreateProjectRootWithoutExistingConfig(t *testing.T) {
	env := testutil.NewTestEnv(t)

	projectRoot, configPath, cfg, err := createProjectRoot(env.RootDir, "client-a/platform")
	if err != nil {
		t.Fatalf("createProjectRoot failed: %v", err)
	}

	if projectRoot == "" || configPath == "" || cfg == nil {
		t.Fatal("expected project root, config path, and config")
	}

	if _, err := os.Stat(filepath.Join(projectRoot, ".hydra.yaml")); err != nil {
		t.Fatalf("expected config file: %v", err)
	}
	if _, err := os.Stat(projectRoot); err != nil {
		t.Fatalf("expected project root to exist: %v", err)
	}
}

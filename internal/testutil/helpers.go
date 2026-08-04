package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
)

// TestEnv is a scratch hydra workspace on disk.
type TestEnv struct {
	RootDir     string
	OriginalDir string
	T           *testing.T
}

// NewTestEnv creates an isolated workspace, an isolated hydra config dir, and
// restores the working directory afterwards.
func NewTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}

	rootDir, err := os.MkdirTemp("", "hydra-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	// Resolve /tmp symlinks so paths reported by git match the ones tests build.
	if resolved, err := filepath.EvalSymlinks(rootDir); err == nil {
		rootDir = resolved
	}

	t.Cleanup(func() {
		_ = os.Chdir(originalDir)
		_ = os.RemoveAll(rootDir)
	})

	t.Setenv("GO_TEST", "1")
	t.Setenv("HYDRA_CONFIG_DIR", filepath.Join(rootDir, ".hydra-config"))
	// Keep test output free of ANSI and of auto-JSON surprises.
	t.Setenv("NO_COLOR", "1")

	return &TestEnv{RootDir: rootDir, OriginalDir: originalDir, T: t}
}

// InitConfig writes a schema v2 .hydra/config.yaml at the workspace root.
func (e *TestEnv) InitConfig() string {
	e.T.Helper()

	cfg := config.DefaultConfig(filepath.Base(e.RootDir))
	configPath := config.ManifestPath(e.RootDir)
	if err := cfg.Save(configPath); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
	return configPath
}

// LoadConfig reads the workspace config.
func (e *TestEnv) LoadConfig() *config.Config {
	e.T.Helper()

	cfg, err := config.Load(config.ManifestPath(e.RootDir))
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}
	return cfg
}

// SaveConfig writes the workspace config back.
func (e *TestEnv) SaveConfig(cfg *config.Config) {
	e.T.Helper()

	if err := cfg.Save(config.ManifestPath(e.RootDir)); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// CreateRemoteRepo builds a real upstream repository outside the workspace, with
// an initial commit on defaultBranch plus any extra branches. Its path is a valid
// git remote URL, so tests can exercise real fetch and upstream tracking.
func (e *TestEnv) CreateRemoteRepo(name, defaultBranch string, extraBranches ...string) string {
	e.T.Helper()

	remotePath := filepath.Join(e.RootDir, ".remotes", name)
	if err := os.MkdirAll(remotePath, 0755); err != nil {
		e.T.Fatalf("Failed to create remote dir: %v", err)
	}

	e.git(remotePath, "init", "-b", defaultBranch)
	e.git(remotePath, "config", "user.email", "test@test.com")
	e.git(remotePath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(remotePath, "README.md"), []byte("# "+name+"\n"), 0644); err != nil {
		e.T.Fatalf("Failed to write remote README: %v", err)
	}
	e.git(remotePath, "add", ".")
	e.git(remotePath, "commit", "-m", "Initial commit")

	for _, branch := range extraBranches {
		e.git(remotePath, "branch", branch, defaultBranch)
	}
	// A non-bare remote refuses pushes to the checked-out branch; keep it detached.
	e.git(remotePath, "config", "receive.denyCurrentBranch", "ignore")

	return remotePath
}

// CommitToRemote adds an empty commit to a branch of a remote created by
// CreateRemoteRepo, so tests can make a worktree genuinely behind.
func (e *TestEnv) CommitToRemote(remotePath, branch, message string) {
	e.T.Helper()

	current := strings.TrimSpace(e.gitOutput(remotePath, "rev-parse", "--abbrev-ref", "HEAD"))
	if current != branch {
		e.git(remotePath, "checkout", branch)
		defer e.git(remotePath, "checkout", current)
	}
	e.git(remotePath, "commit", "--allow-empty", "-m", message)
}

// CreateBareRepo creates a bare repository backed by a real remote and registers
// nothing; use AddToConfig to register it.
func (e *TestEnv) CreateBareRepo(alias, defaultBranch string, extraBranches ...string) (barePath, remotePath string) {
	e.T.Helper()

	remotePath = e.CreateRemoteRepo(alias+"-origin", defaultBranch, extraBranches...)
	barePath = e.GetBarePath(alias)
	if err := os.MkdirAll(filepath.Dir(barePath), 0755); err != nil {
		e.T.Fatalf("Failed to create bare dir: %v", err)
	}
	if err := git.InitBareWithRemote(barePath, remotePath); err != nil {
		e.T.Fatalf("Failed to init bare repo: %v", err)
	}
	return barePath, remotePath
}

// CreateWorktree creates a worktree as a real sibling directory under group,
// tracking origin when the branch exists there.
func (e *TestEnv) CreateWorktree(group, alias, branch, dirName string) string {
	e.T.Helper()

	barePath := e.GetBarePath(alias)
	worktreePath := filepath.Join(e.RootDir, group, dirName)
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0755); err != nil {
		e.T.Fatalf("Failed to create group dir: %v", err)
	}

	kind, err := git.ClassifyBranch(barePath, branch)
	if err != nil {
		e.T.Fatalf("Failed to classify branch %s: %v", branch, err)
	}

	switch kind {
	case git.BranchRemote, git.BranchBoth:
		err = git.AddWorktreeTracking(barePath, worktreePath, branch)
	case git.BranchLocal:
		err = git.AddWorktreeExistingLocal(barePath, worktreePath, branch)
	default:
		baseRef, baseErr := git.GetRemoteDefaultBranch(barePath)
		if baseErr != nil {
			e.T.Fatalf("Failed to resolve base branch: %v", baseErr)
		}
		err = git.AddWorktreeNewBranch(barePath, worktreePath, branch, "origin/"+baseRef)
	}
	if err != nil {
		e.T.Fatalf("Failed to create worktree %s: %v", branch, err)
	}

	return worktreePath
}

// SetupRepo is the common fixture: a bare repo with a real remote, registered in
// the config under group/alias, plus a worktree for the default branch.
func (e *TestEnv) SetupRepo(group, alias, defaultBranch string, extraBranches ...string) (barePath, remotePath, worktreePath string) {
	e.T.Helper()

	barePath, remotePath = e.CreateBareRepo(alias, defaultBranch, extraBranches...)
	e.AddToConfig(group, alias, remotePath, defaultBranch)
	worktreePath = e.CreateWorktree(group, alias, defaultBranch, alias)
	return barePath, remotePath, worktreePath
}

// AddToConfig registers a repo under a group in schema v2.
func (e *TestEnv) AddToConfig(group, alias, remote, defaultBranch string) {
	e.T.Helper()

	cfg := e.LoadConfig()
	cfg.SetRepo(group, alias, config.Repo{Remote: remote, DefaultBranch: defaultBranch})
	e.SaveConfig(cfg)
}

// MakeWorktreeDirty creates uncommitted changes in a worktree.
func (e *TestEnv) MakeWorktreeDirty(worktreePath string) {
	e.T.Helper()

	file := filepath.Join(worktreePath, "dirty-file.txt")
	if err := os.WriteFile(file, []byte("dirty content"), 0644); err != nil {
		e.T.Fatalf("Failed to create dirty file: %v", err)
	}
}

// Chdir changes to the workspace root.
func (e *TestEnv) Chdir() {
	e.T.Helper()
	if err := os.Chdir(e.RootDir); err != nil {
		e.T.Fatalf("Failed to chdir: %v", err)
	}
}

// ChdirTo changes into a path relative to the workspace root.
func (e *TestEnv) ChdirTo(relative string) {
	e.T.Helper()
	if err := os.Chdir(filepath.Join(e.RootDir, relative)); err != nil {
		e.T.Fatalf("Failed to chdir to %s: %v", relative, err)
	}
}

// GetBarePath returns the bare repository path for an alias.
func (e *TestEnv) GetBarePath(alias string) string {
	return filepath.Join(e.RootDir, ".bare", alias+".git")
}

// GetWorktreePath returns the sibling worktree path for a group and directory name.
func (e *TestEnv) GetWorktreePath(group, dirName string) string {
	return filepath.Join(e.RootDir, group, dirName)
}

// Upstream returns the configured upstream of a worktree, or "" when it has none.
func (e *TestEnv) Upstream(worktreePath string) string {
	e.T.Helper()

	//nolint:gosec // G204: test fixture shelling out to git; constant binary, no shell
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// FileExists checks if a path exists.
func (e *TestEnv) FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists checks if a directory exists.
func (e *TestEnv) DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsSymlink reports whether a path is a symlink.
func (e *TestEnv) IsSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

// CreateCommit creates a commit in a worktree.
func (e *TestEnv) CreateCommit(worktreePath, message string) {
	e.T.Helper()

	e.git(worktreePath, "config", "user.email", "test@test.com")
	e.git(worktreePath, "config", "user.name", "Test")

	file := filepath.Join(worktreePath, fmt.Sprintf("file-%s.txt", strings.ReplaceAll(message, "/", "-")))
	if err := os.WriteFile(file, []byte(message), 0644); err != nil {
		e.T.Fatalf("Failed to write file: %v", err)
	}
	e.git(worktreePath, "add", ".")
	e.git(worktreePath, "commit", "-m", message)
}

func (e *TestEnv) git(dir string, args ...string) {
	e.T.Helper()

	//nolint:gosec // G204: test fixture shelling out to git; constant binary, no shell
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		e.T.Fatalf("git %s failed in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
}

func (e *TestEnv) gitOutput(dir string, args ...string) string {
	e.T.Helper()

	//nolint:gosec // G204: test fixture shelling out to git; constant binary, no shell
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		e.T.Fatalf("git %s failed in %s: %v", strings.Join(args, " "), dir, err)
	}
	return string(out)
}

// Contains asserts a substring is present.
func Contains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("Expected string to contain %q, but it didn't.\nString: %s", substr, s)
	}
}

// NotContains asserts a substring is absent.
func NotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("Expected string to NOT contain %q, but it did.\nString: %s", substr, s)
	}
}

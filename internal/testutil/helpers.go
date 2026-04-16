package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
)

// TestEnv represents a test environment
type TestEnv struct {
	RootDir     string
	OriginalDir string
	T           *testing.T
}

// NewTestEnv creates a new test environment
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

	t.Cleanup(func() {
		os.Chdir(originalDir)
		os.RemoveAll(rootDir)
	})

	os.Setenv("GO_TEST", "1")
	t.Cleanup(func() {
		os.Unsetenv("GO_TEST")
	})

	return &TestEnv{
		RootDir:     rootDir,
		OriginalDir: originalDir,
		T:           t,
	}
}

// InitConfig creates a .hydra.yaml config file
func (e *TestEnv) InitConfig() string {
	e.T.Helper()

	cfg := config.DefaultConfig()
	configPath := filepath.Join(e.RootDir, ".hydra.yaml")

	if err := cfg.Save(configPath); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}

	return configPath
}

// CreateBareRepo creates a bare git repository
func (e *TestEnv) CreateBareRepo(name string) string {
	e.T.Helper()

	bareDir := filepath.Join(e.RootDir, ".bare", name+".git")
	if err := os.MkdirAll(bareDir, 0755); err != nil {
		e.T.Fatalf("Failed to create bare dir: %v", err)
	}

	cmd := exec.Command("git", "init", "--bare", bareDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		e.T.Fatalf("Failed to init bare repo: %v\nOutput: %s", err, output)
	}

	return bareDir
}

// CreateWorktree creates a worktree for a bare repo
func (e *TestEnv) CreateWorktree(bareRepo, branch string) string {
	e.T.Helper()

	worktreePath := filepath.Join(bareRepo, branch)

	// Create initial commit in bare repo if needed
	cmd := exec.Command("git", "--git-dir="+bareRepo, "show-ref", "--verify", "--quiet", "refs/heads/main")
	if err := cmd.Run(); err != nil {
		// Need to create initial commit
		tempDir, err2 := os.MkdirTemp("", "hydra-init-*")
		if err2 != nil {
			e.T.Fatalf("Failed to create temp dir: %v", err2)
		}
		defer os.RemoveAll(tempDir)

		cmd = exec.Command("git", "clone", "--bare", bareRepo, tempDir)
		cmd.Run() // Ignore error

		cmd = exec.Command("git", "-C", tempDir, "init")
		if err := cmd.Run(); err != nil {
			e.T.Fatalf("Failed to init temp repo: %v", err)
		}

		cmd = exec.Command("git", "-C", tempDir, "config", "user.email", "test@test.com")
		cmd.Run()
		cmd = exec.Command("git", "-C", tempDir, "config", "user.name", "Test")
		cmd.Run()

		readme := filepath.Join(tempDir, "README.md")
		os.WriteFile(readme, []byte("# Test"), 0644)

		cmd = exec.Command("git", "-C", tempDir, "add", ".")
		cmd.Run()
		cmd = exec.Command("git", "-C", tempDir, "commit", "-m", "Initial commit")
		cmd.Run()
		cmd = exec.Command("git", "-C", tempDir, "push", bareRepo, "main")
		cmd.Run()
	}

	// Create worktree
	cmd = exec.Command("git", "--git-dir="+bareRepo, "worktree", "add", "-b", branch, worktreePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Branch might already exist
		cmd = exec.Command("git", "--git-dir="+bareRepo, "worktree", "add", worktreePath, branch)
		if output2, err2 := cmd.CombinedOutput(); err2 != nil {
			e.T.Fatalf("Failed to create worktree: %v\nOutput1: %s\nOutput2: %s", err, output, output2)
		}
	}

	return worktreePath
}

// AddToConfig adds a repository to the config
func (e *TestEnv) AddToConfig(group, alias, repoName string) {
	e.T.Helper()

	configPath := filepath.Join(e.RootDir, ".hydra.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		e.T.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Ecosystems == nil {
		cfg.Ecosystems = make(map[string]config.Ecosystem)
	}
	if cfg.Ecosystems[group] == nil {
		cfg.Ecosystems[group] = make(config.Ecosystem)
	}
	cfg.Ecosystems[group][alias] = repoName

	if err := cfg.Save(configPath); err != nil {
		e.T.Fatalf("Failed to save config: %v", err)
	}
}

// MakeWorktreeDirty creates uncommitted changes in a worktree
func (e *TestEnv) MakeWorktreeDirty(worktreePath string) {
	e.T.Helper()

	file := filepath.Join(worktreePath, "dirty-file.txt")
	if err := os.WriteFile(file, []byte("dirty content"), 0644); err != nil {
		e.T.Fatalf("Failed to create dirty file: %v", err)
	}
}

// Chdir changes to the test directory
func (e *TestEnv) Chdir() {
	e.T.Helper()
	os.Chdir(e.RootDir)
}

// ChdirToGroup changes to a group directory
func (e *TestEnv) ChdirToGroup(group string) {
	e.T.Helper()
	os.Chdir(filepath.Join(e.RootDir, group))
}

// GetBarePath returns the path to a bare repo
func (e *TestEnv) GetBarePath(name string) string {
	return filepath.Join(e.RootDir, ".bare", name+".git")
}

// GetWorktreePath returns the path to a worktree
func (e *TestEnv) GetWorktreePath(bareName, branch string) string {
	return filepath.Join(e.RootDir, ".bare", bareName+".git", branch)
}

// FileExists checks if a file exists
func (e *TestEnv) FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// DirExists checks if a directory exists
func (e *TestEnv) DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// Contains checks if a string contains a substring (helper for assertions)
func Contains(t *testing.T, s, substr string) {
	t.Helper()
	if !contains(s, substr) {
		t.Errorf("Expected string to contain %q, but it didn't.\nString: %s", substr, s)
	}
}

// NotContains checks if a string does not contain a substring
func NotContains(t *testing.T, s, substr string) {
	t.Helper()
	if contains(s, substr) {
		t.Errorf("Expected string to NOT contain %q, but it did.\nString: %s", substr, s)
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && stringContains(s, substr)
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// CaptureOutput captures stdout during a function execution
func CaptureOutput(f func()) string {
	// Save original stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run function
	f()

	// Restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read output
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	return string(buf[:n])
}

// CreateCommit creates a commit in a worktree
func (e *TestEnv) CreateCommit(worktreePath, message string) {
	e.T.Helper()

	// Configure git
	exec.Command("git", "-C", worktreePath, "config", "user.email", "test@test.com").Run()
	exec.Command("git", "-C", worktreePath, "config", "user.name", "Test").Run()

	// Create a file
	file := filepath.Join(worktreePath, fmt.Sprintf("file-%s.txt", message))
	os.WriteFile(file, []byte(message), 0644)

	// Commit
	cmd := exec.Command("git", "-C", worktreePath, "add", ".")
	if err := cmd.Run(); err != nil {
		e.T.Fatalf("Failed to add: %v", err)
	}

	cmd = exec.Command("git", "-C", worktreePath, "commit", "-m", message)
	if err := cmd.Run(); err != nil {
		e.T.Fatalf("Failed to commit: %v", err)
	}
}

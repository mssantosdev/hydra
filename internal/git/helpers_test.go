package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitTestEnv returns env vars that make commits independent of the developer's
// global git config.
func gitTestEnv() []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@t",
	)
}

func resolveTempDir(t *testing.T, dir string) string {
	t.Helper()
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = gitTestEnv()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// newUpstream builds a real repository to act as origin, with an initial commit
// on `main` plus a `stage` branch.
func newUpstream(t *testing.T) string {
	t.Helper()

	dir := resolveTempDir(t, t.TempDir())
	upstream := filepath.Join(dir, "upstream")
	if err := os.MkdirAll(upstream, 0755); err != nil {
		t.Fatalf("mkdir upstream: %v", err)
	}

	runGitTest(t, upstream, "init", "-b", "main")
	runGitTest(t, upstream, "commit", "--allow-empty", "-m", "init")
	runGitTest(t, upstream, "branch", "stage", "main")
	return upstream
}

// newBareWithRemote creates a bare repo tracking upstream via InitBareWithRemote.
func newBareWithRemote(t *testing.T, upstream string) (root, bare string) {
	t.Helper()
	root = resolveTempDir(t, t.TempDir())
	bare = filepath.Join(root, "api.git")
	if err := InitBareWithRemote(bare, upstream); err != nil {
		t.Fatalf("InitBareWithRemote: %v", err)
	}
	return root, bare
}

func fileURL(path string) string {
	return "file://" + filepath.ToSlash(path)
}

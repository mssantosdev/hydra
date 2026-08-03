package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// InitRepository creates a normal (non-bare) repository with an initial commit.
func InitRepository(repoPath, branch string) error {
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}

	if err := runGitIn(repoPath, "init"); err != nil {
		return err
	}
	if err := runGitIn(repoPath, "checkout", "-b", branch); err != nil {
		return err
	}
	if err := runGitIn(repoPath, "config", "user.email", "hydra@local"); err != nil {
		return err
	}
	if err := runGitIn(repoPath, "config", "user.name", "Hydra"); err != nil {
		return err
	}

	readmePath := filepath.Join(repoPath, "README.md")
	projectName := filepath.Base(repoPath)
	content := "# " + strings.ReplaceAll(projectName, "-", " ") + "\n"
	if err := os.WriteFile(readmePath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to create initial README: %w", err)
	}
	if err := runGitIn(repoPath, "add", "."); err != nil {
		return err
	}
	if err := runGitIn(repoPath, "commit", "-m", "Initial commit"); err != nil {
		return err
	}

	return nil
}

// InitBareLocal creates a bare repository holding one initial commit on branch and
// NO configured remote: a project that has no upstream yet.
//
// The seed checkout exists only to produce that first commit (a bare repo cannot
// commit) and is removed afterwards. Crucially, `origin` is never configured, so
// nothing is left pointing at a path that no longer exists.
func InitBareLocal(barePath, branch string) error {
	if err := runGit("init", "--bare", "--initial-branch="+branch, barePath); err != nil {
		return err
	}

	seed, err := os.MkdirTemp("", "hydra-seed-*")
	if err != nil {
		return fmt.Errorf("failed to create seed directory: %w", err)
	}
	defer os.RemoveAll(seed)

	if err := InitRepository(seed, branch); err != nil {
		return err
	}
	// Push into refs/heads/<branch> so the bare repo owns the history outright.
	if err := runGitIn(seed, "push", barePath, branch+":refs/heads/"+branch); err != nil {
		return err
	}
	return runGit("--git-dir="+barePath, "symbolic-ref", "HEAD", "refs/heads/"+branch)
}

// HasRemote reports whether a bare repository has an `origin` remote configured.
// A locally-bootstrapped project legitimately has none, and hydra must not report
// that as damage.
func HasRemote(bareRepo string) bool {
	return GetConfig(bareRepo, "remote.origin.url") != ""
}

// InProgressGitState reports rebase/merge/cherry-pick state present in a
// worktree's git dir. hydra reports this state; it never touches it.
func InProgressGitState(worktreePath string) ([]string, error) {
	out, err := runGitOutput("-C", worktreePath, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	gitDir := strings.TrimSpace(out)

	var found []string
	for _, name := range []string{"REBASE_HEAD", "MERGE_HEAD", "CHERRY_PICK_HEAD", "rebase-merge", "rebase-apply"} {
		if _, err := os.Stat(filepath.Join(gitDir, name)); err == nil {
			found = append(found, name)
		}
	}
	return found, nil
}

func runGitIn(repoPath string, args ...string) error {
	return runGit(append([]string{"-C", repoPath}, args...)...)
}

package git

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// WorktreeInfo holds information about a worktree
type WorktreeInfo struct {
	Path   string
	Branch string
	IsBare bool
	IsMain bool
}

// ListWorktrees returns all worktrees for a bare repo
func ListWorktrees(bareRepo string) ([]WorktreeInfo, error) {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "worktree", "list", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	var worktrees []WorktreeInfo
	var current WorktreeInfo

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if current.Path != "" {
				worktrees = append(worktrees, current)
				current = WorktreeInfo{}
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current.Path = strings.TrimPrefix(line, "worktree ")
		} else if strings.HasPrefix(line, "branch ") {
			branchRef := strings.TrimPrefix(line, "branch ")
			parts := strings.Split(branchRef, "/")
			if len(parts) > 0 {
				current.Branch = parts[len(parts)-1]
			}
		} else if line == "bare" {
			current.IsBare = true
		}
	}

	// Add last worktree if exists
	if current.Path != "" {
		worktrees = append(worktrees, current)
	}

	return worktrees, nil
}

// CreateWorktree creates a new worktree
func CreateWorktree(bareRepo, worktreePath, branch string) error {
	// Check if branch exists
	branchExists := false
	cmd := exec.Command("git", "--git-dir="+bareRepo, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	if err := cmd.Run(); err == nil {
		branchExists = true
	}

	var args []string
	args = append(args, "--git-dir="+bareRepo, "worktree", "add")

	if !branchExists {
		// Create new branch from HEAD
		args = append(args, "-b", branch)
	}

	args = append(args, worktreePath)

	if branchExists {
		args = append(args, branch)
	}

	cmd = exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}

// RemoveWorktree removes a worktree
func RemoveWorktree(bareRepo, worktreePath string, force bool) error {
	args := []string{"--git-dir=" + bareRepo, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)

	cmd := exec.Command("git", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}

// GetCurrentBranch returns the current branch in a worktree
func GetCurrentBranch(worktreePath string) (string, error) {
	cmd := exec.Command("git", "-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

// HasUncommittedChanges checks if worktree has uncommitted changes
func HasUncommittedChanges(worktreePath string) (bool, int, error) {
	// Check for staged or unstaged changes
	cmd := exec.Command("git", "-C", worktreePath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, 0, err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	return count > 0, count, nil
}

// IsBranchMerged checks if a branch is merged into another
func IsBranchMerged(bareRepo, branch, into string) bool {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "merge-base", "--is-ancestor", branch, into)
	return cmd.Run() == nil
}

// CloneBare creates a bare clone of a repository
func CloneBare(source, dest string) error {
	cmd := exec.Command("git", "clone", "--bare", source, dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// PushAll pushes all branches and tags to a remote
func PushAll(source, remote string) error {
	cmd := exec.Command("git", "-C", source, "push", remote, "--all")
	cmd.Run() // Ignore error

	cmd = exec.Command("git", "-C", source, "push", remote, "--tags")
	cmd.Run() // Ignore error

	return nil
}

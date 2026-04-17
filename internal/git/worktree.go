package git

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
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
			current.Branch = branchNameFromRef(branchRef)
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

// CreateWorktreeForBranch creates a worktree for an existing local or remote branch.
func CreateWorktreeForBranch(bareRepo, worktreePath, branch string) error {
	branchRef, err := ResolveBranchRef(bareRepo, branch)
	if err != nil {
		return err
	}

	args := []string{"--git-dir=" + bareRepo, "worktree", "add"}
	if strings.HasPrefix(branchRef, "origin/") {
		args = append(args, "-b", branch)
	}
	args = append(args, worktreePath, branchRef)

	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}

// CreateWorktreeFromBase creates a new branch from a specific base branch or ref.
func CreateWorktreeFromBase(bareRepo, worktreePath, branch, baseBranch string) error {
	baseRef, err := ResolveBranchRef(bareRepo, baseBranch)
	if err != nil {
		return err
	}

	cmd := exec.Command("git", "--git-dir="+bareRepo, "worktree", "add", "-b", branch, worktreePath, baseRef)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}

// CreateWorktreeNewBranch creates a new branch without forcing a base ref.
func CreateWorktreeNewBranch(bareRepo, worktreePath, branch string) error {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "worktree", "add", "-b", branch, worktreePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create worktree: %w", err)
	}

	return nil
}

// CreateWorktree keeps backward-compatible behavior for callers that only know the target branch.
func CreateWorktree(bareRepo, worktreePath, branch string) error {
	exists, err := BranchExists(bareRepo, branch)
	if err != nil {
		return err
	}
	if exists {
		return CreateWorktreeForBranch(bareRepo, worktreePath, branch)
	}
	return CreateWorktreeNewBranch(bareRepo, worktreePath, branch)
}

// BranchExists reports whether a branch exists locally or on origin.
func BranchExists(bareRepo, branch string) (bool, error) {
	if hasRef(bareRepo, "refs/heads/"+branch) || hasRef(bareRepo, "refs/remotes/origin/"+branch) {
		return true, nil
	}
	return false, nil
}

// RefExists reports whether an arbitrary ref exists.
func RefExists(bareRepo, ref string) bool {
	return hasRef(bareRepo, ref)
}

// ResolveBranchRef returns the best ref for a branch name.
func ResolveBranchRef(bareRepo, branch string) (string, error) {
	if hasRef(bareRepo, "refs/heads/"+branch) {
		return branch, nil
	}
	if hasRef(bareRepo, "refs/remotes/origin/"+branch) {
		return "origin/" + branch, nil
	}
	return "", fmt.Errorf("branch not found: %s", branch)
}

// ListLocalBranches returns local branches from the bare repository.
func ListLocalBranches(bareRepo string) ([]string, error) {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list local branches: %w", err)
	}

	return parseRefList(output), nil
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

func hasRef(bareRepo, ref string) bool {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "show-ref", "--verify", "--quiet", ref)
	return cmd.Run() == nil
}

func parseRefList(output []byte) []string {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	branches := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		branches = append(branches, name)
	}
	sort.Strings(branches)
	return branches
}

func branchNameFromRef(ref string) string {
	for _, prefix := range []string{"refs/heads/", "refs/remotes/origin/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

// PushAll pushes all branches and tags to a remote
func PushAll(source, remote string) error {
	cmd := exec.Command("git", "-C", source, "push", remote, "--all")
	cmd.Run() // Ignore error

	cmd = exec.Command("git", "-C", source, "push", remote, "--tags")
	cmd.Run() // Ignore error

	return nil
}

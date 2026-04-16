package git

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// RemoteBranch represents a branch from the remote
type RemoteBranch struct {
	Name      string
	IsDefault bool // main or master
	IsRemote  bool
}

// FetchRemoteBranches fetches branch information from a remote repository
// This is used before cloning to show available branches
func FetchRemoteBranches(repoURL string) ([]RemoteBranch, error) {
	// Use git ls-remote to list branches without cloning
	cmd := exec.Command("git", "ls-remote", "--heads", repoURL)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	var branches []RemoteBranch
	scanner := bufio.NewScanner(strings.NewReader(string(output)))

	for scanner.Scan() {
		line := scanner.Text()
		// Format: <sha>\trefs/heads/<branch-name>
		parts := strings.Split(line, "\t")
		if len(parts) != 2 {
			continue
		}

		ref := parts[1]
		if strings.HasPrefix(ref, "refs/heads/") {
			branchName := strings.TrimPrefix(ref, "refs/heads/")
			isDefault := branchName == "main" || branchName == "master"

			branches = append(branches, RemoteBranch{
				Name:      branchName,
				IsDefault: isDefault,
				IsRemote:  true,
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse remote branches: %w", err)
	}

	return branches, nil
}

// GetDefaultBranch returns the default branch (main or master) from the list
func GetDefaultBranch(branches []RemoteBranch) string {
	for _, b := range branches {
		if b.Name == "main" {
			return "main"
		}
	}
	for _, b := range branches {
		if b.Name == "master" {
			return "master"
		}
	}
	// Return first branch if no main/master found
	if len(branches) > 0 {
		return branches[0].Name
	}
	return "main"
}

// FilterBranches returns only the default branches (main, master)
func FilterBranches(branches []RemoteBranch, includeDefaults bool) []RemoteBranch {
	var result []RemoteBranch
	for _, b := range branches {
		if includeDefaults && b.IsDefault {
			result = append(result, b)
		}
	}
	return result
}

// FetchBareRepo fetches updates for a bare repository
func FetchBareRepo(bareRepo string) error {
	cmd := exec.Command("git", "--git-dir="+bareRepo, "fetch", "--all")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// GetRemoteBranchesFromBare gets branches from an existing bare repo
func GetRemoteBranchesFromBare(bareRepo string) ([]RemoteBranch, error) {
	// First fetch to ensure we have latest
	_ = FetchBareRepo(bareRepo)

	cmd := exec.Command("git", "--git-dir="+bareRepo, "branch", "-r")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	var branches []RemoteBranch
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "->") {
			continue
		}

		// Format: origin/branch-name
		if strings.HasPrefix(line, "origin/") {
			branchName := strings.TrimPrefix(line, "origin/")
			// Skip HEAD
			if branchName == "HEAD" {
				continue
			}

			isDefault := branchName == "main" || branchName == "master"
			branches = append(branches, RemoteBranch{
				Name:      branchName,
				IsDefault: isDefault,
				IsRemote:  true,
			})
		}
	}

	if len(branches) == 0 {
		locals, localErr := ListLocalBranches(bareRepo)
		if localErr == nil {
			for _, branchName := range locals {
				branches = append(branches, RemoteBranch{
					Name:      branchName,
					IsDefault: branchName == "main" || branchName == "master",
					IsRemote:  false,
				})
			}
		}
	}

	return branches, nil
}

// GetRemoteDefaultBranch resolves the default branch configured on origin/HEAD.
func GetRemoteDefaultBranch(bareRepo string) (string, error) {
	_ = FetchBareRepo(bareRepo)

	cmd := exec.Command("git", "--git-dir="+bareRepo, "symbolic-ref", "refs/remotes/origin/HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}

	ref := strings.TrimSpace(string(output))
	if ref == "" {
		return "", nil
	}

	return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
}

// WorktreeStatus represents the status of a worktree
type WorktreeStatus struct {
	Path          string
	Branch        string
	HasChanges    bool
	ChangeCount   int
	CommitsBehind int
	CommitsAhead  int
	IsClean       bool
}

// CheckWorktreeStatus checks the status of a worktree against remote
func CheckWorktreeStatus(bareRepo, worktreePath, branch string) (WorktreeStatus, error) {
	status := WorktreeStatus{
		Path:   worktreePath,
		Branch: branch,
	}

	// Check for uncommitted changes
	hasChanges, count, err := HasUncommittedChanges(worktreePath)
	if err != nil {
		return status, err
	}
	status.HasChanges = hasChanges
	status.ChangeCount = count

	// Check commits behind/ahead
	behind, ahead, err := getCommitDiff(bareRepo, branch)
	if err == nil {
		status.CommitsBehind = behind
		status.CommitsAhead = ahead
	}

	status.IsClean = !hasChanges && behind == 0 && ahead == 0
	return status, nil
}

// getCommitDiff returns commits behind and ahead of remote
func getCommitDiff(bareRepo, branch string) (behind, ahead int, err error) {
	// Get commits behind (remote has, local doesn't)
	cmd := exec.Command("git", "--git-dir="+bareRepo, "rev-list", "--count", "HEAD..origin/"+branch)
	output, err := cmd.Output()
	if err == nil {
		fmt.Sscanf(string(output), "%d", &behind)
	}

	// Get commits ahead (local has, remote doesn't)
	cmd = exec.Command("git", "--git-dir="+bareRepo, "rev-list", "--count", "origin/"+branch+"..HEAD")
	output, err = cmd.Output()
	if err == nil {
		fmt.Sscanf(string(output), "%d", &ahead)
	}

	return behind, ahead, nil
}

// PullWorktree pulls the latest changes for a worktree
func PullWorktree(worktreePath, branch string) error {
	cmd := exec.Command("git", "-C", worktreePath, "pull", "origin", branch)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StashChanges stashes changes in a worktree
func StashChanges(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "push", "-m", "hydra-auto-stash")
	return cmd.Run()
}

// PopStash pops the latest stash
func PopStash(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "stash", "pop")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ResetHard resets a worktree to HEAD
func ResetHard(worktreePath string) error {
	cmd := exec.Command("git", "-C", worktreePath, "reset", "--hard", "HEAD")
	return cmd.Run()
}

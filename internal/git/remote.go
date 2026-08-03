package git

import (
	"bufio"
	"fmt"
	"strings"
)

// RemoteBranch represents a branch from the remote
type RemoteBranch struct {
	Name      string
	IsDefault bool // main or master
	IsRemote  bool
}

// FetchRemoteBranches lists a remote's branches without cloning.
func FetchRemoteBranches(repoURL string) ([]RemoteBranch, error) {
	output, err := runGitOutput("ls-remote", "--heads", repoURL)
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	var branches []RemoteBranch
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		// Format: <sha>\trefs/heads/<branch-name>
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) != 2 {
			continue
		}
		ref := parts[1]
		if !strings.HasPrefix(ref, "refs/heads/") {
			continue
		}
		name := strings.TrimPrefix(ref, "refs/heads/")
		branches = append(branches, RemoteBranch{
			Name:      name,
			IsDefault: name == "main" || name == "master",
			IsRemote:  true,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to parse remote branches: %w", err)
	}

	return branches, nil
}

// GetDefaultBranch picks the conventional default branch out of a branch list.
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

// FetchBareRepo fetches origin into refs/remotes/origin/* for a bare repository.
func FetchBareRepo(bareRepo string) error {
	return runGitStreaming("--git-dir="+bareRepo, "fetch", "origin", "--prune", "--tags")
}

// GetRemoteBranchesFromBare lists origin's branches from an existing bare repo.
// refs/remotes/origin/* is authoritative; there is no local-branch fallback,
// because a bare repo built by InitBareWithRemote always has remote refs.
func GetRemoteBranchesFromBare(bareRepo string) ([]RemoteBranch, error) {
	if err := FetchBareRepo(bareRepo); err != nil {
		return nil, err
	}
	return listRemoteRefs(bareRepo)
}

// ListRemoteBranchesCached lists origin's branches without fetching first.
func ListRemoteBranchesCached(bareRepo string) ([]RemoteBranch, error) {
	return listRemoteRefs(bareRepo)
}

func listRemoteRefs(bareRepo string) ([]RemoteBranch, error) {
	output, err := runGitOutput("--git-dir="+bareRepo, "for-each-ref",
		"--format=%(refname:strip=3)", "refs/remotes/origin")
	if err != nil {
		return nil, fmt.Errorf("failed to list remote branches: %w", err)
	}

	var branches []RemoteBranch
	for _, name := range parseRefList(output) {
		if name == "HEAD" {
			continue
		}
		branches = append(branches, RemoteBranch{
			Name:      name,
			IsDefault: name == "main" || name == "master",
			IsRemote:  true,
		})
	}
	return branches, nil
}

// GetRemoteDefaultBranch resolves the default branch from refs/remotes/origin/HEAD.
// It returns a real error when origin/HEAD is unset, so callers must fall back
// explicitly instead of silently treating "" as success.
func GetRemoteDefaultBranch(bareRepo string) (string, error) {
	output, err := runGitOutput("--git-dir="+bareRepo, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", fmt.Errorf("origin/HEAD is not set for %s: %w", bareRepo, err)
	}
	ref := strings.TrimSpace(output)
	if ref == "" {
		return "", fmt.Errorf("origin/HEAD is empty for %s", bareRepo)
	}
	return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
}

// TrackingState describes a worktree branch relative to its upstream.
type TrackingState struct {
	Branch   string
	Upstream string // "" when the branch has no upstream (a valid local-only state)
	Ahead    int
	Behind   int
}

// WorktreeTracking reads the real tracking state of a worktree. A branch with no
// upstream is reported as Upstream == "" with a nil error; that is a valid state,
// not a failure.
func WorktreeTracking(worktreePath string) (TrackingState, error) {
	state := TrackingState{}

	branch, err := GetCurrentBranch(worktreePath)
	if err != nil {
		return state, fmt.Errorf("failed to read branch of %s: %w", worktreePath, err)
	}
	state.Branch = strings.TrimSpace(branch)
	if state.Branch == "HEAD" {
		// Detached: nothing to track.
		state.Branch = ""
		return state, nil
	}

	upstream, err := runGitOutput("-C", worktreePath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return state, nil
	}
	state.Upstream = strings.TrimSpace(upstream)
	if state.Upstream == "" {
		return state, nil
	}

	counts, err := runGitOutput("-C", worktreePath, "rev-list", "--left-right", "--count",
		state.Branch+"..."+state.Upstream)
	if err != nil {
		return state, fmt.Errorf("failed to count commits for %s: %w", worktreePath, err)
	}
	fields := strings.Fields(counts)
	if len(fields) != 2 {
		return state, fmt.Errorf("unexpected rev-list output for %s: %q", worktreePath, counts)
	}
	if _, err := fmt.Sscanf(fields[0], "%d", &state.Ahead); err != nil {
		return state, fmt.Errorf("failed to parse ahead count for %s: %w", worktreePath, err)
	}
	if _, err := fmt.Sscanf(fields[1], "%d", &state.Behind); err != nil {
		return state, fmt.Errorf("failed to parse behind count for %s: %w", worktreePath, err)
	}
	return state, nil
}

// WorktreeStatus represents the status of a worktree
type WorktreeStatus struct {
	Path          string
	Branch        string
	Upstream      string
	HasChanges    bool
	ChangeCount   int
	CommitsBehind int
	CommitsAhead  int
	IsClean       bool
	Detached      bool
}

// CheckWorktreeStatus reports uncommitted changes plus real ahead/behind counts.
func CheckWorktreeStatus(worktreePath string) (WorktreeStatus, error) {
	status := WorktreeStatus{Path: worktreePath}

	hasChanges, count, err := HasUncommittedChanges(worktreePath)
	if err != nil {
		return status, err
	}
	status.HasChanges = hasChanges
	status.ChangeCount = count

	tracking, err := WorktreeTracking(worktreePath)
	if err != nil {
		return status, err
	}
	status.Branch = tracking.Branch
	status.Upstream = tracking.Upstream
	status.CommitsAhead = tracking.Ahead
	status.CommitsBehind = tracking.Behind
	status.Detached = tracking.Branch == ""
	status.IsClean = !hasChanges && tracking.Ahead == 0 && tracking.Behind == 0

	return status, nil
}

// PullWorktree fast-forwards a worktree from its configured upstream.
func PullWorktree(worktreePath string) error {
	return runGitStreaming("-C", worktreePath, "pull", "--ff-only")
}

// StashChanges stashes changes in a worktree.
func StashChanges(worktreePath string) error {
	return runGit("-C", worktreePath, "stash", "push", "-m", "hydra-auto-stash")
}

// PopStash pops the latest stash.
func PopStash(worktreePath string) error {
	return runGitStreaming("-C", worktreePath, "stash", "pop")
}

// ResetHard resets a worktree to HEAD.
func ResetHard(worktreePath string) error {
	return runGit("-C", worktreePath, "reset", "--hard", "HEAD")
}

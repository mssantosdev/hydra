package git

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// WorktreeInfo holds information about a worktree as reported by
// `git worktree list --porcelain`. This is the only source of truth for the
// path<->branch mapping; never reconstruct either side from the other.
type WorktreeInfo struct {
	Path     string
	Branch   string // "" when detached
	Head     string // commit sha, always set for non-bare entries
	Detached bool
	Locked   bool
	Prunable bool
	IsBare   bool
}

// ListWorktrees returns all worktrees registered in a bare repo.
func ListWorktrees(bareRepo string) ([]WorktreeInfo, error) {
	output, err := runGitOutput("--git-dir="+bareRepo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	var worktrees []WorktreeInfo
	var current WorktreeInfo
	flush := func() {
		if current.Path != "" {
			worktrees = append(worktrees, current)
		}
		current = WorktreeInfo{}
	}

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			current.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			current.Head = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			current.Branch = branchNameFromRef(strings.TrimPrefix(line, "branch "))
		case line == "detached":
			current.Detached = true
		case line == "bare":
			current.IsBare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			current.Locked = true
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			current.Prunable = true
		}
	}
	flush()

	return worktrees, nil
}

// InitBareWithRemote creates a bare repository that behaves like a normal clone:
// refs/heads/* starts empty and refs/remotes/origin/* holds the remote view.
//
// `git clone --bare` is deliberately NOT used: it writes no remote.origin.fetch
// refspec and creates no remote-tracking refs, which makes upstream tracking
// impossible for every worktree created afterwards.
func InitBareWithRemote(barePath, remoteURL string) error {
	if err := runGit("init", "--bare", barePath); err != nil {
		return err
	}
	if err := runGit("--git-dir="+barePath, "remote", "add", "origin", remoteURL); err != nil {
		return err
	}
	if err := runGit("--git-dir="+barePath, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	if err := FetchBareRepo(barePath); err != nil {
		return err
	}
	// Best effort: a remote with no branches yet has no HEAD to resolve.
	_ = runGit("--git-dir="+barePath, "remote", "set-head", "origin", "-a")
	return nil
}

// RepairBareRemote makes an EXISTING bare repository conform to what
// InitBareWithRemote would have produced: correct origin URL, the standard fetch
// refspec, a completed fetch, and a resolved origin/HEAD.
//
// This is what makes an interrupted clone resumable. `InitBareWithRemote` performs
// a full network fetch, so a Ctrl-C or dropped connection can leave a bare repo
// that exists but has no remote-tracking refs and no origin/HEAD; re-running the
// clone must converge on a healthy repo instead of failing or starting over.
func RepairBareRemote(barePath, remoteURL string) error {
	if GetConfig(barePath, "remote.origin.url") == "" {
		if err := runGit("--git-dir="+barePath, "remote", "add", "origin", remoteURL); err != nil {
			return err
		}
	} else if remoteURL != "" {
		if err := runGit("--git-dir="+barePath, "remote", "set-url", "origin", remoteURL); err != nil {
			return err
		}
	}
	if err := SetFetchRefspec(barePath); err != nil {
		return err
	}
	if err := FetchBareRepo(barePath); err != nil {
		return err
	}
	_ = SetOriginHead(barePath)
	return nil
}

// SetOriginHead points refs/remotes/origin/HEAD at the remote's default branch.
func SetOriginHead(barePath string) error {
	return runGit("--git-dir="+barePath, "remote", "set-head", "origin", "-a")
}

// AddWorktreeTracking creates a worktree for a branch that exists on origin,
// creating the local branch WITH upstream tracking configured.
func AddWorktreeTracking(barePath, worktreePath, branch string) error {
	if hasRef(barePath, "refs/heads/"+branch) {
		// Local branch already exists; attach it and (re)point upstream.
		if err := runGit("--git-dir="+barePath, "worktree", "add", worktreePath, branch); err != nil {
			return err
		}
		return SetUpstream(worktreePath, branch)
	}
	return runGit("--git-dir="+barePath, "worktree", "add", "--track", "-b", branch, worktreePath, "origin/"+branch)
}

// AddWorktreeNewBranch creates a worktree on a brand-new branch cut from baseRef.
// --no-track is explicit: branching off origin/<base> would otherwise inherit the
// BASE branch as upstream, making ahead/behind counts lie. A new branch has no
// upstream until it is pushed, and hydra reports that honestly as local-only.
func AddWorktreeNewBranch(barePath, worktreePath, branch, baseRef string) error {
	args := []string{"--git-dir=" + barePath, "worktree", "add", "--no-track", "-b", branch, worktreePath}
	if baseRef != "" {
		args = append(args, baseRef)
	}
	return runGit(args...)
}

// AddWorktreeExistingLocal attaches an existing local-only branch to a new
// worktree. No upstream is invented.
func AddWorktreeExistingLocal(barePath, worktreePath, branch string) error {
	return runGit("--git-dir="+barePath, "worktree", "add", worktreePath, branch)
}

// SetUpstream points a worktree's current branch at origin/<branch>.
func SetUpstream(worktreePath, branch string) error {
	return runGit("-C", worktreePath, "branch", "--set-upstream-to=origin/"+branch, branch)
}

// BranchKind classifies where a branch name exists.
type BranchKind int

const (
	BranchNone   BranchKind = iota // exists nowhere
	BranchRemote                   // refs/remotes/origin/<b> only
	BranchLocal                    // refs/heads/<b> only
	BranchBoth
)

func (k BranchKind) String() string {
	switch k {
	case BranchRemote:
		return "remote"
	case BranchLocal:
		return "local"
	case BranchBoth:
		return "both"
	}
	return "none"
}

// ClassifyBranch reports whether a branch exists locally, on origin, both, or
// nowhere, so callers stop guessing which worktree creator to use.
func ClassifyBranch(barePath, branch string) (BranchKind, error) {
	if barePath == "" || branch == "" {
		return BranchNone, errors.New("bare path and branch are required")
	}
	local := hasRef(barePath, "refs/heads/"+branch)
	remote := hasRef(barePath, "refs/remotes/origin/"+branch)
	switch {
	case local && remote:
		return BranchBoth, nil
	case local:
		return BranchLocal, nil
	case remote:
		return BranchRemote, nil
	}
	return BranchNone, nil
}

// BranchExists reports whether a branch exists locally or on origin.
func BranchExists(bareRepo, branch string) (bool, error) {
	kind, err := ClassifyBranch(bareRepo, branch)
	if err != nil {
		return false, err
	}
	return kind != BranchNone, nil
}

// RefExists reports whether an arbitrary ref exists.
func RefExists(bareRepo, ref string) bool {
	return hasRef(bareRepo, ref)
}

// ResolveBaseRef returns a checkout-able ref for a base branch name,
// preferring the remote-tracking ref.
func ResolveBaseRef(bareRepo, branch string) (string, error) {
	kind, err := ClassifyBranch(bareRepo, branch)
	if err != nil {
		return "", err
	}
	switch kind {
	case BranchRemote, BranchBoth:
		return "origin/" + branch, nil
	case BranchLocal:
		return branch, nil
	}
	return "", fmt.Errorf("branch not found: %s", branch)
}

// ListLocalBranches returns local branches from the bare repository.
func ListLocalBranches(bareRepo string) ([]string, error) {
	output, err := runGitOutput("--git-dir="+bareRepo, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, fmt.Errorf("failed to list local branches: %w", err)
	}
	return parseRefList(output), nil
}

// RemoveWorktree removes a worktree.
func RemoveWorktree(bareRepo, worktreePath string, force bool) error {
	args := []string{"--git-dir=" + bareRepo, "worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, worktreePath)
	if err := runGit(args...); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}
	return nil
}

// PruneWorktrees drops registrations whose directories are gone.
func PruneWorktrees(bareRepo string) error {
	return runGit("--git-dir="+bareRepo, "worktree", "prune")
}

// DeleteBranch deletes a branch from the bare repository.
func DeleteBranch(bareRepo, branch string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	return runGit("--git-dir="+bareRepo, "branch", flag, branch)
}

// GetCurrentBranch returns the current branch in a worktree.
func GetCurrentBranch(worktreePath string) (string, error) {
	output, err := runGitOutput("-C", worktreePath, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(output), nil
}

// HasUncommittedChanges reports whether a worktree has uncommitted changes.
func HasUncommittedChanges(worktreePath string) (bool, int, error) {
	output, err := runGitOutput("-C", worktreePath, "status", "--porcelain")
	if err != nil {
		return false, 0, err
	}

	count := 0
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count > 0, count, nil
}

// IsBranchMerged checks if a branch is merged into another.
func IsBranchMerged(bareRepo, branch, into string) bool {
	return runGit("--git-dir="+bareRepo, "merge-base", "--is-ancestor", branch, into) == nil
}

// GetRemoteURL returns the configured origin URL of a repository directory.
func GetRemoteURL(repoPath string) (string, error) {
	output, err := runGitOutput("-C", repoPath, "remote", "get-url", "origin")
	if err != nil {
		return "", fmt.Errorf("failed to read origin URL of %s: %w", repoPath, err)
	}
	return strings.TrimSpace(output), nil
}

// GetConfig reads a config value from a bare repository, returning "" when unset.
func GetConfig(bareRepo, key string) string {
	output, err := runGitOutput("--git-dir="+bareRepo, "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(output)
}

// SetFetchRefspec restores the standard origin fetch refspec.
func SetFetchRefspec(bareRepo string) error {
	return runGit("--git-dir="+bareRepo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
}

// HasOriginHead reports whether refs/remotes/origin/HEAD is set.
func HasOriginHead(bareRepo string) bool {
	return runGit("--git-dir="+bareRepo, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD") == nil
}

func hasRef(bareRepo, ref string) bool {
	return runGit("--git-dir="+bareRepo, "show-ref", "--verify", "--quiet", ref) == nil
}

// runGit runs git quietly and returns a wrapped error carrying stderr.
//
// gosec flags the variable argv (G204). It is safe and unavoidable here: hydra is a
// git wrapper, the binary is the constant "git", exec.Command does NOT invoke a
// shell, and every element of args is built inside this package from validated
// aliases, branch names, and paths. There is no shell metacharacter to exploit.
func runGit(args ...string) error {
	//nolint:gosec // G204: constant binary, no shell, internally-built argv
	cmd := exec.Command("git", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
		}
		return fmt.Errorf("git %s failed: %s", strings.Join(args, " "), msg)
	}
	return nil
}

// runGitStreaming runs git with stderr attached to the process stderr, for
// long-running operations whose progress the user should see. stdout is
// discarded so it can never corrupt a JSON envelope.
func runGitStreaming(args ...string) error {
	//nolint:gosec // G204: constant binary, no shell, internally-built argv
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runGitOutput(args ...string) (string, error) {
	//nolint:gosec // G204: constant binary, no shell, internally-built argv
	cmd := exec.Command("git", args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s failed: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), msg)
	}
	return string(out), nil
}

func parseRefList(output string) []string {
	lines := strings.Split(strings.TrimSpace(output), "\n")
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

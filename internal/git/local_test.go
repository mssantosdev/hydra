package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitRepositoryCreatesInitialCommit(t *testing.T) {
	root := resolveTempDir(t, t.TempDir())
	repo := filepath.Join(root, "my-project")

	if err := InitRepository(repo, "develop"); err != nil {
		t.Fatalf("InitRepository: %v", err)
	}

	readme := filepath.Join(repo, "README.md")
	data, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("README.md: %v", err)
	}
	if !strings.Contains(string(data), "my project") {
		t.Fatalf("README content = %q, want title derived from directory name", data)
	}

	branch, err := GetCurrentBranch(repo)
	if err != nil || branch != "develop" {
		t.Fatalf("branch = (%q, %v), want (develop, nil)", branch, err)
	}
	if name, err := ConfigUserName(repo); err != nil || name != "Hydra" {
		t.Fatalf("ConfigUserName = (%q, %v), want (Hydra, nil)", name, err)
	}
}

func TestInitBareLocalHasNoRemote(t *testing.T) {
	bare := filepath.Join(resolveTempDir(t, t.TempDir()), "solo.git")
	if err := InitBareLocal(bare, "main"); err != nil {
		t.Fatalf("InitBareLocal: %v", err)
	}
	if HasRemote(bare) {
		t.Fatal("InitBareLocal must not configure origin")
	}
	if !RefExists(bare, "refs/heads/main") {
		t.Fatal("expected main branch in bare repo")
	}
	locals, err := ListLocalBranches(bare)
	if err != nil {
		t.Fatalf("ListLocalBranches: %v", err)
	}
	if len(locals) != 1 || locals[0] != "main" {
		t.Fatalf("local branches = %v, want [main]", locals)
	}
}

func TestHasRemoteReportsConfiguredOrigin(t *testing.T) {
	upstream := newUpstream(t)
	_, bare := newBareWithRemote(t, upstream)
	if !HasRemote(bare) {
		t.Fatal("bare repo with origin should report HasRemote true")
	}
}

func TestConfigUserNamePrefersRepoLocalValue(t *testing.T) {
	dir := resolveTempDir(t, t.TempDir())
	runGitTest(t, dir, "init")
	runGitTest(t, dir, "config", "user.name", "Repo Local")
	name, err := ConfigUserName(dir)
	if err != nil || name != "Repo Local" {
		t.Fatalf("ConfigUserName = (%q, %v), want (Repo Local, nil)", name, err)
	}
}

func TestInProgressGitStateDetectsMerge(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}

	runGitTest(t, worktree, "checkout", "-b", "feature")
	runGitTest(t, worktree, "commit", "--allow-empty", "-m", "feature")
	runGitTest(t, worktree, "checkout", "main")
	runGitTest(t, worktree, "commit", "--allow-empty", "-m", "main moves on")
	if err := runGit("-C", worktree, "merge", "--no-commit", "--no-ff", "feature"); err != nil {
		t.Fatalf("start merge: %v", err)
	}

	states, err := InProgressGitState(worktree)
	if err != nil {
		t.Fatalf("InProgressGitState: %v", err)
	}
	found := false
	for _, s := range states {
		if s == "MERGE_HEAD" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected MERGE_HEAD in %v", states)
	}
}

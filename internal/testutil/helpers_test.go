package testutil

import (
	"path/filepath"
	"testing"
)

// These helpers are what every other package's tests assert through, so a broken one silently
// passes bad tests. Only the passing direction is exercised: they take *testing.T concretely, so
// firing a failure would fail the test doing the verifying.

func TestStringAssertionsAcceptWhatTheyShould(t *testing.T) {
	Contains(t, "worktree created for stage", "stage")
	Contains(t, "anything", "")
	NotContains(t, "worktree created for stage", "main")
	NotContains(t, "", "anything")
}

// The env is the fixture every command test builds on: if it does not produce a real workspace,
// every test above it is testing nothing.
func TestNewTestEnvBuildsARealWorkspace(t *testing.T) {
	env := NewTestEnv(t)
	manifest := env.InitConfig()

	if !env.FileExists(manifest) {
		t.Fatalf("InitConfig reported %s but no manifest exists there", manifest)
	}
	cfg := env.LoadConfig()
	if cfg == nil || cfg.Project == "" {
		t.Fatal("the manifest it wrote does not load into a named project")
	}

	bare, remote, worktree := env.SetupRepo("backend", "api", "main", "stage")
	for name, path := range map[string]string{"bare": bare, "remote": remote, "worktree": worktree} {
		if path == "" {
			t.Errorf("SetupRepo returned an empty %s path", name)
		}
	}
	if !env.DirExists(bare) {
		t.Errorf("the bare repository is missing at %s", bare)
	}
	if !env.DirExists(worktree) {
		t.Errorf("the worktree is missing at %s", worktree)
	}
	// The worktree must be a real directory, not a symlink: hydra's layout guarantee is that
	// worktrees are siblings on disk, and a fixture that fakes it would hide a real regression.
	if env.IsSymlink(worktree) {
		t.Error("the fixture created the worktree as a symlink")
	}
	if got := env.GetBarePath("api"); got != bare {
		t.Errorf("GetBarePath = %q, want %q", got, bare)
	}
}

func TestMakeWorktreeDirtyActuallyDirtiesIt(t *testing.T) {
	env := NewTestEnv(t)
	env.InitConfig()
	_, _, worktree := env.SetupRepo("backend", "api", "main")

	env.MakeWorktreeDirty(worktree)

	// A fixture that claims to dirty a worktree and does not would make every
	// worktree_dirty test pass for the wrong reason.
	if !env.FileExists(filepath.Join(worktree, "dirty-file.txt")) {
		t.Error("MakeWorktreeDirty left no uncommitted file behind")
	}
}

// The remaining fixture surface, exercised so a change to it breaks here rather than in whichever
// command test happens to depend on the part that broke.
func TestFixtureSurfaceProducesUsableGitState(t *testing.T) {
	env := NewTestEnv(t)
	env.InitConfig()

	remote := env.CreateRemoteRepo("api", "main", "stage")
	if !env.DirExists(remote) {
		t.Fatalf("CreateRemoteRepo returned %s which does not exist", remote)
	}
	env.CommitToRemote(remote, "main", "an upstream commit")

	bare, _ := env.CreateBareRepo("api", "main", "stage")
	if !env.DirExists(bare) {
		t.Fatalf("CreateBareRepo returned %s which does not exist", bare)
	}
	env.AddToConfig("backend", "api", remote, "main")
	if cfg := env.LoadConfig(); len(cfg.Groups) == 0 {
		t.Error("AddToConfig registered nothing in the manifest")
	}

	worktree := env.CreateWorktree("backend", "api", "stage", "api-stage")
	if !env.DirExists(worktree) {
		t.Fatalf("CreateWorktree returned %s which does not exist", worktree)
	}
	// A worktree the fixture builds must track its remote branch, or every ahead/behind
	// assertion above it is measuring nothing.
	if upstream := env.Upstream(worktree); upstream == "" {
		t.Error("the created worktree has no upstream")
	}

	env.CreateCommit(worktree, "a local commit")
	env.ChdirTo(filepath.Join("backend", "api-stage"))
}

func TestReadFileReturnsWhatWasWritten(t *testing.T) {
	env := NewTestEnv(t)
	manifest := env.InitConfig()
	if body := env.ReadFile(t, manifest); body == "" {
		t.Error("ReadFile returned nothing for a manifest that exists")
	}
}

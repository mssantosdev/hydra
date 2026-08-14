package git

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestParseRefListDedupesAndSorts(t *testing.T) {
	got := parseRefList("stage\r\nmain\nstage\n\nfeature/x\r")
	want := []string{"feature/x", "main", "stage"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseRefList = %v, want %v", got, want)
	}
	if len(parseRefList("")) != 0 {
		t.Fatal("empty input should yield empty slice")
	}
}

func TestBranchKindString(t *testing.T) {
	tests := []struct {
		kind BranchKind
		want string
	}{
		{BranchNone, "none"},
		{BranchRemote, "remote"},
		{BranchLocal, "local"},
		{BranchBoth, "both"},
		{BranchKind(99), "none"},
	}
	for _, tt := range tests {
		if got := tt.kind.String(); got != tt.want {
			t.Fatalf("%v.String() = %q, want %q", tt.kind, got, tt.want)
		}
	}
}

func TestListWorktreesParsesBareAndLocked(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := runGit("--git-dir="+bare, "worktree", "lock", "--reason", "testing", worktree); err != nil {
		t.Fatalf("lock worktree: %v", err)
	}

	worktrees, err := ListWorktrees(bare)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}

	var bareEntry, locked *WorktreeInfo
	for i := range worktrees {
		wt := &worktrees[i]
		if wt.IsBare {
			bareEntry = wt
		}
		if wt.Path == worktree {
			locked = wt
		}
	}
	if bareEntry == nil || !bareEntry.IsBare {
		t.Fatal("expected bare worktree entry")
	}
	if locked == nil {
		t.Fatalf("missing worktree %s in %v", worktree, worktrees)
	}
	if !locked.Locked || locked.Branch != "main" || locked.Head == "" {
		t.Fatalf("locked worktree = %+v, want Locked with branch main and HEAD set", locked)
	}
}

func TestPruneWorktreesDropsMissingDirectories(t *testing.T) {
	upstream := newUpstream(t)
	root, bare := newBareWithRemote(t, upstream)
	worktree := filepath.Join(root, "api")
	if err := AddWorktreeTracking(bare, worktree, "main"); err != nil {
		t.Fatalf("AddWorktreeTracking: %v", err)
	}
	if err := os.RemoveAll(worktree); err != nil {
		t.Fatalf("remove worktree directory: %v", err)
	}

	worktrees, err := ListWorktrees(bare)
	if err != nil {
		t.Fatalf("ListWorktrees: %v", err)
	}
	foundPrunable := false
	for _, wt := range worktrees {
		if wt.Path == worktree {
			if !wt.Prunable {
				t.Fatalf("missing directory should be prunable: %+v", wt)
			}
			foundPrunable = true
		}
	}
	if !foundPrunable {
		t.Fatalf("expected prunable worktree %s in %v", worktree, worktrees)
	}

	if err := PruneWorktrees(bare); err != nil {
		t.Fatalf("PruneWorktrees: %v", err)
	}
	worktrees, err = ListWorktrees(bare)
	if err != nil {
		t.Fatalf("ListWorktrees after prune: %v", err)
	}
	for _, wt := range worktrees {
		if wt.Path == worktree {
			t.Fatalf("pruned worktree still listed: %+v", wt)
		}
	}
}

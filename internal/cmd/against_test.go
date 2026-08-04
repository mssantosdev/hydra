package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/testutil"
)

// againstEnv builds a repo with an integration branch, a branch merged into it, and a
// branch that is not.
func againstEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()

	// A worktree carrying a commit that "release" will not have.
	unmerged := env.CreateWorktree("backend", "api", "feat/unmerged", "api-unmerged")
	env.CreateCommit(unmerged, "work not in release")

	// release points at main, so main is merged and feat/unmerged is not.
	env.GitInBare(t, "api", "branch", "release", "main")
	return env
}

func statusPayload(t *testing.T, args ...string) statusProjectPayload {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"status"}, args...))
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status %v: %v", args, err)
	}
	var payload statusProjectPayload
	decodeJSONData(t, stdout, &payload)
	return payload
}

// --against answers "is this work in REF yet" without hydra storing an integration
// edge: the relationship is computed from refs git already has, so it cannot go stale.
func TestStatusAgainst_ReportsMergedAndUnmerged(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	payload := statusPayload(t, "--against", "release", "--output", "json")

	var checked int
	for _, wt := range payload.Worktrees {
		if wt.Against == nil {
			t.Errorf("%s has no against block", wt.Branch)
			continue
		}
		if wt.Against.Ref != "release" {
			t.Errorf("%s ref = %q, want release", wt.Branch, wt.Against.Ref)
		}
		switch wt.Branch {
		case "main":
			if !wt.Against.Merged || wt.Against.Ahead != 0 {
				t.Errorf("main must be merged into release, got %+v", *wt.Against)
			}
			checked++
		case "feat/unmerged":
			if wt.Against.Merged {
				t.Errorf("feat/unmerged must NOT be merged, got %+v", *wt.Against)
			}
			if wt.Against.Ahead != 1 {
				t.Errorf("feat/unmerged ahead = %d, want 1", wt.Against.Ahead)
			}
			checked++
		}
	}
	if checked != 2 {
		t.Fatalf("expected to check both branches, checked %d", checked)
	}
}

// Merged is exactly Ahead == 0, and it is reported rather than left for the caller to
// derive from a count.
func TestStatusAgainst_MergedIsAheadZero(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	payload := statusPayload(t, "--against", "release", "--output", "json")
	for _, wt := range payload.Worktrees {
		if wt.Against == nil {
			continue
		}
		if wt.Against.Merged != (wt.Against.Ahead == 0) {
			t.Errorf("%s: merged=%v but ahead=%d", wt.Branch, wt.Against.Merged, wt.Against.Ahead)
		}
	}
}

// An unresolvable ref is a per-worktree WARNING, never fatal. A release branch
// legitimately exists in some repositories and not others, so one missing ref must not
// make the whole workspace unlistable.
func TestStatusAgainst_UnknownRefWarnsAndStillLists(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"status", "--against", "no-such-ref", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("an unknown ref must not fail the command: %v", err)
	}

	var envelope struct {
		Warnings []string             `json:"warnings"`
		Data     statusProjectPayload `json:"data"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(envelope.Data.Worktrees) == 0 {
		t.Fatal("the worktrees must still be listed")
	}
	if len(envelope.Warnings) == 0 {
		t.Error("an unresolvable ref must surface as a warning")
	}
	for _, wt := range envelope.Data.Worktrees {
		if wt.Against != nil {
			t.Errorf("%s must have no against block when the ref does not resolve", wt.Branch)
		}
	}
}

// Without --against the field is OMITTED, not null: a consumer branches on its
// presence, and null would be a value it has to special-case.
func TestStatusAgainst_OmittedWhenNotRequested(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"status", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}

	if got := stdout.String(); strings.Contains(got, `"against"`) {
		t.Errorf("the against key must be absent without --against:\n%s", got)
	}
}

// --against is orthogonal to the selector: it annotates what survived narrowing.
func TestStatusAgainst_CombinesWithASelector(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	payload := statusPayload(t, "--against", "release",
		"--filter", "branch:feat/*", "--output", "json")

	if len(payload.Worktrees) != 1 {
		t.Fatalf("expected only the feat/* worktree, got %d", len(payload.Worktrees))
	}
	if payload.Worktrees[0].Against == nil {
		t.Fatal("the surviving worktree must still be annotated")
	}
	if payload.Worktrees[0].Against.Merged {
		t.Error("feat/unmerged is not merged into release")
	}
}

// list takes --against too, so the two read commands do not diverge.
func TestListAgainst_IsAvailableToo(t *testing.T) {
	resetCommandState(t)
	againstEnv(t)

	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs([]string{"list", "--against", "release", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("list --against: %v", err)
	}

	var payload listProjectPayload
	decodeJSONData(t, stdout, &payload)
	if len(payload.Worktrees) == 0 {
		t.Fatal("no worktrees returned")
	}
	for _, wt := range payload.Worktrees {
		if wt.Against == nil {
			t.Errorf("%s was not annotated by list --against", wt.Branch)
		}
	}
}

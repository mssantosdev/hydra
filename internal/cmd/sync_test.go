package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func TestSync_NoConfig(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.Chdir()
	rootCmd.SetArgs([]string{"sync"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error when no config")
	}
	testutil.Contains(t, err.Error(), "no .hydra/config.yaml")
}

func TestSync_NoWorktrees(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"sync"})
	t.Setenv("HYDRA_OUTPUT", "json")
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync with no worktrees: %v", err)
	}
	var envelope struct {
		Data struct {
			Worktrees []syncWorktreeJSON `json:"worktrees"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if len(envelope.Data.Worktrees) != 0 {
		t.Fatalf("expected no worktrees, got %d", len(envelope.Data.Worktrees))
	}
}

func TestSync_PullsBehind(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	t.Setenv("HYDRA_OUTPUT", "json")
	_, remote, worktreePath := env.SetupRepo("backend", "api", "main")
	env.CommitToRemote(remote, "main", "ahead on remote")
	if err := git.FetchBareRepo(env.GetBarePath("api")); err != nil {
		t.Fatalf("fetch bare repo: %v", err)
	}
	tracking, err := git.WorktreeTracking(worktreePath)
	if err != nil {
		t.Fatalf("tracking before sync: %v", err)
	}
	if tracking.Behind == 0 {
		t.Fatal("expected worktree to be behind upstream before sync")
	}
	env.Chdir()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"sync", "--all", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	tracking, err = git.WorktreeTracking(worktreePath)
	if err != nil {
		t.Fatalf("tracking after sync: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("expected worktree to be up to date, still behind by %d", tracking.Behind)
	}
	var envelope struct {
		Data struct {
			Worktrees []syncWorktreeJSON `json:"worktrees"`
			Summary   syncSummaryJSON    `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if envelope.Data.Summary.Pulled == 0 {
		t.Fatalf("expected pulled worktrees, summary: %+v", envelope.Data.Summary)
	}
	found := false
	for _, wt := range envelope.Data.Worktrees {
		if wt.Path == worktreePath && wt.Status == "pulled" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected pulled status for %s, got %+v", worktreePath, envelope.Data.Worktrees)
	}
}

func TestSync_LocalOnlyBranch(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	t.Setenv("HYDRA_OUTPUT", "json")
	env.SetupRepo("backend", "api", "main")
	localPath := env.CreateWorktree("backend", "api", "feature/local-only", "api-feature-local")
	env.Chdir()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"sync", "--all", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	var envelope struct {
		Data struct {
			Worktrees []syncWorktreeJSON `json:"worktrees"`
			Summary   syncSummaryJSON    `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if envelope.Data.Summary.Failed != 0 {
		t.Fatalf("local-only worktree must not count as failure, summary: %+v", envelope.Data.Summary)
	}
	if envelope.Data.Summary.LocalOnly == 0 {
		t.Fatalf("expected local_only count, summary: %+v", envelope.Data.Summary)
	}
	found := false
	for _, wt := range envelope.Data.Worktrees {
		if wt.Path == localPath {
			if wt.Status != "local-only" {
				t.Fatalf("expected local-only status, got %q", wt.Status)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("local-only worktree missing from output: %+v", envelope.Data.Worktrees)
	}
}

func TestSync_ForceDirtyStashPullRestore(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	t.Setenv("HYDRA_OUTPUT", "json")
	_, remote, worktreePath := env.SetupRepo("backend", "api", "main")
	env.CommitToRemote(remote, "main", "ahead for dirty pull")
	dirtyFile := filepath.Join(worktreePath, "README.md")
	if err := os.WriteFile(dirtyFile, []byte("keep me"), 0644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}
	if err := git.FetchBareRepo(env.GetBarePath("api")); err != nil {
		t.Fatalf("fetch bare repo: %v", err)
	}
	env.Chdir()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"sync", "--all", "--yes", "--force"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	content, err := os.ReadFile(dirtyFile)
	if err != nil {
		t.Fatalf("dirty file should survive stash/pull/restore: %v", err)
	}
	if string(content) != "keep me" {
		t.Fatalf("dirty file content changed: %q", content)
	}
	tracking, err := git.WorktreeTracking(worktreePath)
	if err != nil {
		t.Fatalf("tracking after sync: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("expected worktree to be up to date, still behind by %d", tracking.Behind)
	}
	var envelope struct {
		Data struct {
			Worktrees []syncWorktreeJSON `json:"worktrees"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	found := false
	for _, wt := range envelope.Data.Worktrees {
		if wt.Path == worktreePath && wt.Status == "stashed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected stashed status for dirty worktree, got %+v", envelope.Data.Worktrees)
	}
}

func TestSync_DetectCurrentRepo(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.ChdirTo(filepath.Join("backend", "api"))
	rootCmd.SetArgs([]string{"sync"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync from within worktree: %v", err)
	}
}

func TestSyncFlags(t *testing.T) {
	long := syncCmd.Long
	for _, code := range []string{"partial_failure", "worktree_dirty", "not_in_project", "git_failed", "hook_failed"} {
		if !strings.Contains(long, code) {
			t.Fatalf("sync help missing exit code %q", code)
		}
	}
}

func TestSync_PartialFailureCode(t *testing.T) {
	err := output.Errorf(output.CodePartialFailure, "one failed").WithDetail("worktrees", []map[string]string{{"repo": "api"}})
	var outErr *output.Error
	if !errors.As(err, &outErr) {
		t.Fatalf("expected *output.Error, got %T", err)
	}
	if outErr.Code != output.CodePartialFailure {
		t.Fatalf("expected partial_failure, got %q", outErr.Code)
	}
}

// TestSync_ConcurrentAcrossManyWorktrees gives the race detector something real to
// inspect. sync fans out over two goroutine pools (status gathering, then pulling),
// but every other sync test drives one or two worktrees, so `go test -race` was
// effectively only checking that a single goroutine does not race with itself.
//
// This builds several repos with several behind branches each, syncs them all in
// one command, and asserts every one was pulled.
func TestSync_ConcurrentAcrossManyWorktrees(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	t.Setenv("HYDRA_OUTPUT", "json")

	type wt struct {
		path   string
		branch string
	}
	var worktrees []wt

	// 3 repos x 3 branches = 9 worktrees, so both pools run genuinely wide.
	for _, repo := range []string{"api", "web", "worker"} {
		branches := []string{"feature-a", "feature-b"}
		_, remote, mainPath := env.SetupRepo("backend", repo, "main", branches...)
		worktrees = append(worktrees, wt{mainPath, "main"})

		for _, branch := range branches {
			dir := repo + "-" + branch
			worktrees = append(worktrees, wt{env.CreateWorktree("backend", repo, branch, dir), branch})
		}
		// Put every branch behind its upstream.
		for _, branch := range append([]string{"main"}, branches...) {
			env.CommitToRemote(remote, branch, "remote work on "+branch)
		}
		if err := git.FetchBareRepo(env.GetBarePath(repo)); err != nil {
			t.Fatalf("fetch %s: %v", repo, err)
		}
	}

	for _, w := range worktrees {
		tracking, err := git.WorktreeTracking(w.path)
		if err != nil {
			t.Fatalf("tracking %s: %v", w.path, err)
		}
		if tracking.Behind == 0 {
			t.Fatalf("fixture: %s (%s) should be behind before sync", w.path, w.branch)
		}
	}

	env.Chdir()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"sync", "--all", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("sync: %v", err)
	}

	for _, w := range worktrees {
		tracking, err := git.WorktreeTracking(w.path)
		if err != nil {
			t.Fatalf("tracking after sync %s: %v", w.path, err)
		}
		if tracking.Behind != 0 {
			t.Errorf("%s (%s) still behind by %d after sync", w.path, w.branch, tracking.Behind)
		}
	}

	var envelope struct {
		Data struct {
			Summary syncSummaryJSON `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("decode json: %v\n%s", err, buf.String())
	}
	if got, want := envelope.Data.Summary.Pulled, len(worktrees); got != want {
		t.Errorf("pulled = %d, want %d (summary: %+v)", got, want, envelope.Data.Summary)
	}
	if envelope.Data.Summary.Failed != 0 {
		t.Errorf("failed = %d, want 0", envelope.Data.Summary.Failed)
	}
}

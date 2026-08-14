package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

type syncCovEnvelope struct {
	Outcome  string               `json:"outcome"`
	Summary  string               `json:"summary"`
	Warnings []*output.Diagnostic `json:"warnings"`
	Error    *output.Diagnostic   `json:"error"`
}

func syncCovResetSelectors() {
	topicFilter = ""
	reposFilter = nil
	groupFilter = ""
	stateFilter = nil
}

func syncCovEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	resetCommandState(t)
	syncCovResetSelectors()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()
	return env
}

func syncCovExec(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	resetCommandState(t)
	syncCovResetSelectors()
	return runCmdJSON(t, args...)
}

func syncCovDecode(t *testing.T, stdout *bytes.Buffer) syncCovEnvelope {
	t.Helper()
	var env syncCovEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.String())
	}
	return env
}

func syncCovDirtyBehindEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := syncCovEnv(t)
	_, remote, wt := env.SetupRepo("backend", "api", "main")
	env.CommitToRemote(remote, "main", "upstream")
	syncCovFetchBare(t, env, "api")
	env.MakeWorktreeDirty(wt)
	return env
}

func syncCovHead(t *testing.T, worktreePath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", worktreePath, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD in %s: %v", worktreePath, err)
	}
	return strings.TrimSpace(string(out))
}

func syncCovStashList(t *testing.T, worktreePath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", worktreePath, "stash", "list").Output()
	if err != nil {
		t.Fatalf("stash list in %s: %v", worktreePath, err)
	}
	return strings.TrimSpace(string(out))
}

func syncCovFetchBare(t *testing.T, env *testutil.TestEnv, alias string) {
	t.Helper()
	if err := git.FetchBareRepo(env.GetBarePath(alias)); err != nil {
		t.Fatalf("fetch bare %s: %v", alias, err)
	}
}

func syncCovLoadProject(t *testing.T, env *testutil.TestEnv) {
	t.Helper()
	env.Chdir()
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
}
func syncCovBehindEnv(t *testing.T) (*testutil.TestEnv, string, string) {
	t.Helper()
	env := syncCovEnv(t)
	_, remote, wt := env.SetupRepo("backend", "api", "main")
	env.CommitToRemote(remote, "main", "upstream")
	return env, remote, wt
}

func TestSyncCov_DirtyWithoutPolicyRefuses(t *testing.T) {
	syncCovDirtyBehindEnv(t)

	_, err := syncCovExec(t, "sync", "--yes")
	if err == nil {
		t.Fatal("dirty worktree without --dirty must refuse")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodeNeedsInput {
		t.Fatalf("code=%s, want needs_input", classified.Code)
	}
	if output.ExitFor(classified.Code) != 7 {
		t.Fatalf("exit=%d, want 7", output.ExitFor(classified.Code))
	}
}

func TestSyncCov_DirtyStashPullsAndLeavesRecoverableStash(t *testing.T) {
	env := syncCovDirtyBehindEnv(t)
	wt := env.GetWorktreePath("backend", "api")
	before := syncCovHead(t, wt)

	stdout, err := syncCovExec(t, "sync", "--yes", "--dirty", "stash")
	if err != nil {
		t.Fatalf("sync --dirty stash: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 1 {
		t.Fatalf("pulled=%d, want 1", data.Summary.Pulled)
	}
	if !env.FileExists(wt + "/dirty-file.txt") {
		t.Fatal("stash policy must restore dirty changes in the worktree")
	}
	tracking, err := git.WorktreeTracking(wt)
	if err != nil {
		t.Fatalf("tracking: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("worktree still behind by %d after stash pull", tracking.Behind)
	}
	if after := syncCovHead(t, wt); after == before {
		t.Fatal("upstream advance must move HEAD after pull")
	}
	if syncCovStashList(t, wt) != "" {
		t.Fatal("successful stash sync must pop the stash entry after pull")
	}
}

func TestSyncCov_DirtyStashKeepsRecoverableStashWhenPullRefused(t *testing.T) {
	env := syncCovEnv(t)
	_, remote, wt := env.SetupRepo("backend", "api", "main")
	env.CreateCommit(wt, "local-only")
	env.CommitToRemote(remote, "main", "upstream")
	syncCovFetchBare(t, env, "api")
	env.MakeWorktreeDirty(wt)

	_, err := syncCovExec(t, "sync", "--yes", "--dirty", "stash")
	if err == nil {
		t.Fatal("diverged branch must refuse fast-forward pull")
	}
	if syncCovStashList(t, wt) == "" {
		t.Fatal("stash policy must leave changes recoverable in git stash when pull is refused")
	}
	if env.FileExists(wt + "/dirty-file.txt") {
		t.Fatal("dirty file must remain absent from worktree while stashed")
	}
}

func TestSyncCov_DirtyResetDiscardsAndPulls(t *testing.T) {
	env := syncCovDirtyBehindEnv(t)
	wt := env.GetWorktreePath("backend", "api")
	readme := filepath.Join(wt, "README.md")
	if err := os.WriteFile(readme, []byte("local dirty"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	stdout, err := syncCovExec(t, "sync", "--yes", "--dirty", "reset")
	if err != nil {
		t.Fatalf("sync --dirty reset: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 1 {
		t.Fatalf("pulled=%d, want 1", data.Summary.Pulled)
	}
	content, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("read readme: %v", err)
	}
	if strings.Contains(string(content), "local dirty") {
		t.Fatal("reset policy must discard tracked changes")
	}
	tracking, err := git.WorktreeTracking(wt)
	if err != nil {
		t.Fatalf("tracking: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("reset policy must still pull, behind=%d", tracking.Behind)
	}
}

func TestSyncCov_DirtySkipLeavesWorktreeDirtyAndBehind(t *testing.T) {
	env := syncCovDirtyBehindEnv(t)
	wt := env.GetWorktreePath("backend", "api")
	before := syncCovHead(t, wt)

	stdout, err := syncCovExec(t, "sync", "--yes", "--dirty", "skip")
	if err != nil {
		t.Fatalf("sync --dirty skip: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 0 {
		t.Fatalf("pulled=%d, want 0", data.Summary.Pulled)
	}
	if !env.FileExists(wt + "/dirty-file.txt") {
		t.Fatal("skip must leave dirty changes in place")
	}
	if after := syncCovHead(t, wt); after != before {
		t.Fatal("skip must leave the worktree unpulled")
	}
	tracking, err := git.WorktreeTracking(wt)
	if err != nil {
		t.Fatalf("tracking: %v", err)
	}
	if tracking.Behind == 0 {
		t.Fatal("skip must leave the worktree behind upstream")
	}
}

func TestSyncCov_BareMissingPartialWhileHealthyRepoPulls(t *testing.T) {
	env := syncCovEnv(t)
	_, remoteA, wtA := env.SetupRepo("backend", "api", "main")
	_, remoteB, _ := env.SetupRepo("backend", "web", "main")
	env.CommitToRemote(remoteA, "main", "ahead-a")
	env.CommitToRemote(remoteB, "main", "ahead-b")
	syncCovFetchBare(t, env, "api")
	syncCovFetchBare(t, env, "web")
	if err := os.RemoveAll(env.GetBarePath("web")); err != nil {
		t.Fatalf("remove bare: %v", err)
	}

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err == nil {
		t.Fatal("bare_missing must degrade the outcome")
	}
	classified := output.Classify(err)
	if classified.Code != output.CodePartialFailure {
		t.Fatalf("code=%s, want partial_failure", classified.Code)
	}
	if output.ExitFor(classified.Code) != 4 {
		t.Fatalf("exit=%d, want 4", output.ExitFor(classified.Code))
	}
	payload := syncCovDecode(t, stdout)
	foundBare := false
	for _, w := range payload.Warnings {
		if w != nil && w.Code == output.CodeBareMissing {
			foundBare = true
			break
		}
	}
	if !foundBare {
		t.Fatalf("expected bare_missing warning, got %+v", payload.Warnings)
	}
	tracking, err := git.WorktreeTracking(wtA)
	if err != nil {
		t.Fatalf("tracking api: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("healthy repo must still pull, behind=%d", tracking.Behind)
	}
}

func TestSyncCov_AllFailedIsFailureNotPartial(t *testing.T) {
	env := syncCovEnv(t)
	_, remoteA, wtA := env.SetupRepo("backend", "api", "main")
	_, remoteB, wtB := env.SetupRepo("backend", "web", "main")
	env.CreateCommit(wtA, "local-a")
	env.CreateCommit(wtB, "local-b")
	env.CommitToRemote(remoteA, "main", "remote-a")
	env.CommitToRemote(remoteB, "main", "remote-b")
	syncCovFetchBare(t, env, "api")
	syncCovFetchBare(t, env, "web")

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err == nil {
		t.Fatal("every diverged worktree must fail the sync")
	}
	payload := syncCovDecode(t, stdout)
	if payload.Outcome != string(output.OutcomeFailure) {
		t.Fatalf("outcome=%q, want failure", payload.Outcome)
	}
	if output.Classify(err).Code == output.CodePartialFailure {
		t.Fatal("all-failed must not be reported as partial_failure")
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Failed != 2 {
		t.Fatalf("failed=%d, want 2", data.Summary.Failed)
	}
	if data.Summary.Pulled != 0 {
		t.Fatalf("pulled=%d, want 0", data.Summary.Pulled)
	}
}

func TestSyncCov_PostSyncHookFailureIsNotFetchFailure(t *testing.T) {
	env := syncCovEnv(t)
	_, remote, _ := env.SetupRepo("backend", "api", "main", "stage")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostSync: []config.Hook{{Run: "exit 1"}},
	})
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CommitToRemote(remote, "main", "upstream-main")
	env.CommitToRemote(remote, "stage", "upstream-stage")
	syncCovFetchBare(t, env, "api")
	syncCovLoadProject(t, env)
	trustCurrentWorkspace(t)

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err != nil {
		t.Fatalf("post_sync hook failure must not fail sync: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Failed != 0 {
		t.Fatalf("failed=%d, want 0; git work landed", data.Summary.Failed)
	}
	if data.Summary.Pulled < 2 {
		t.Fatalf("pulled=%d, want both worktrees advanced", data.Summary.Pulled)
	}
	for _, wt := range []string{
		env.GetWorktreePath("backend", "api"),
		env.GetWorktreePath("backend", "api-stage"),
	} {
		tracking, trackErr := git.WorktreeTracking(wt)
		if trackErr != nil {
			t.Fatalf("tracking %s: %v", wt, trackErr)
		}
		if tracking.Behind != 0 {
			t.Fatalf("%s still behind after sync", wt)
		}
	}
	payload := syncCovDecode(t, stdout)
	if len(payload.Warnings) == 0 {
		t.Fatal("hook failure must surface as a warning")
	}
	for _, w := range payload.Warnings {
		if w == nil {
			continue
		}
		if strings.Contains(w.Message, "could not be fetched") {
			t.Fatalf("hook failure must not be reported as fetch failure: %+v", w)
		}
	}
}

func TestSyncCov_NothingToDoReportsUpToDateSummary(t *testing.T) {
	syncCovEnv(t).SetupRepo("backend", "api", "main")

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err != nil {
		t.Fatalf("up-to-date sync must succeed: %v", err)
	}
	payload := syncCovDecode(t, stdout)
	if !strings.Contains(payload.Summary, "up to date") {
		t.Fatalf("summary=%q, want an up-to-date sentence", payload.Summary)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 0 {
		t.Fatalf("pulled=%d, want 0", data.Summary.Pulled)
	}
	if data.Summary.Total == 0 {
		t.Fatal("summary must report inspected worktrees")
	}
}

func TestSyncCov_LocalOnlyBranchIsNotFailure(t *testing.T) {
	env := syncCovEnv(t)
	env.SetupRepo("backend", "api", "main")
	localPath := env.CreateWorktree("backend", "api", "feature/local-only", "api-feature-local")

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err != nil {
		t.Fatalf("local-only branch must not fail sync: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Failed != 0 {
		t.Fatalf("failed=%d, want 0", data.Summary.Failed)
	}
	if data.Summary.LocalOnly == 0 {
		t.Fatalf("local_only=%d, want >0", data.Summary.LocalOnly)
	}
	found := false
	for _, wt := range data.Worktrees {
		if wt.Path == localPath && wt.Status == "local-only" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected local-only status for %s, got %+v", localPath, data.Worktrees)
	}
}

func TestSyncCov_NonFastForwardRefusesMerge(t *testing.T) {
	env, remote, wt := syncCovBehindEnv(t)
	syncCovFetchBare(t, env, "api")
	env.CreateCommit(wt, "local divergence")
	localHead := syncCovHead(t, wt)
	env.CommitToRemote(remote, "main", "remote divergence")
	syncCovFetchBare(t, env, "api")

	stdout, err := syncCovExec(t, "sync", "--all", "--yes")
	if err == nil {
		t.Fatal("non-fast-forward pull must be refused")
	}
	if output.Classify(err).Code != output.CodeGitFailed {
		t.Fatalf("code=%s, want git_failed", output.Classify(err).Code)
	}
	if after := syncCovHead(t, wt); after != localHead {
		t.Fatal("hydra must not merge; local commit must remain at HEAD")
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 0 {
		t.Fatalf("pulled=%d, want 0", data.Summary.Pulled)
	}
	if data.Summary.Failed != 1 {
		t.Fatalf("failed=%d, want 1", data.Summary.Failed)
	}
}

func TestSyncCov_PrintSyncTextSummarizesResults(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	printSyncText([]syncOpResult{
		{entry: syncEntry{repo: "api", branch: "main"}, status: "pulled", pulled: true},
		{entry: syncEntry{repo: "web", branch: "main"}, status: "failed", err: output.Errorf(output.CodeGitFailed, "pull refused")},
	}, syncSummaryJSON{Total: 2, Pulled: 1, Failed: 1})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	text := buf.String()
	if !strings.Contains(text, "Successfully synced") || !strings.Contains(text, "Failed to sync") {
		t.Fatalf("text output missing sync summary, got:\n%s", text)
	}
}

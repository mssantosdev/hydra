package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/global"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
	"github.com/mssantosdev/hydra/internal/topic"
	"github.com/mssantosdev/hydra/internal/trust"
)

type lifecycleEnvelope struct {
	Outcome  string               `json:"outcome"`
	Summary  string               `json:"summary"`
	Warnings []*output.Diagnostic `json:"warnings"`
	Error    *output.Diagnostic   `json:"error"`
	Data     json.RawMessage      `json:"data"`
}

func lifecycleExec(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	return runCmdJSON(t, args...)
}

func lifecycleDecode(t *testing.T, stdout *bytes.Buffer) lifecycleEnvelope {
	t.Helper()
	var env lifecycleEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.String())
	}
	return env
}

func lifecycleEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()
	return env
}

func lifecycleTopicEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := lifecycleEnv(t)
	env.SetupRepo("backend", "api", "main", "stage")
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	return env
}

func lifecycleSyncDirtyEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := lifecycleEnv(t)
	_, remote, _ := env.SetupRepo("backend", "api", "main")
	env.CommitToRemote(remote, "main", "upstream")
	env.MakeWorktreeDirty(env.GetWorktreePath("backend", "api"))
	return env
}

func lifecycleLoadProject(t *testing.T, env *testutil.TestEnv) {
	t.Helper()
	env.Chdir()
	projectConfigPath = config.ManifestPath(env.RootDir)
	projectRoot = env.RootDir
	if err := loadProject(); err != nil {
		t.Fatalf("loadProject: %v", err)
	}
}

func lifecycleCaptureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("copy stdout: %v", err)
	}
	return buf.String()
}

func TestLifecycleRemove_RefusesDirtyWorktree(t *testing.T) {
	env := lifecycleTopicEnv(t)
	wt := env.GetWorktreePath("backend", "api-stage")
	env.MakeWorktreeDirty(wt)

	_, err := lifecycleExec(t, "remove", "api", "stage", "--yes")
	if err == nil {
		t.Fatal("dirty worktree must be refused")
	}
	he := output.Classify(err)
	if he.Code != output.CodeWorktreeDirty || he.Exit != 5 {
		t.Fatalf("code=%s exit=%d, want worktree_dirty/5", he.Code, he.Exit)
	}
	if !env.DirExists(wt) {
		t.Fatal("worktree was removed despite refusal")
	}
}

func TestLifecycleRemove_ForceProceedsThroughDirty(t *testing.T) {
	env := lifecycleTopicEnv(t)
	wt := env.GetWorktreePath("backend", "api-stage")
	env.MakeWorktreeDirty(wt)

	if _, err := lifecycleExec(t, "remove", "api", "stage", "--force", "--yes"); err != nil {
		t.Fatalf("force remove: %v", err)
	}
	if env.DirExists(wt) {
		t.Fatal("worktree should be gone after --force")
	}
}

func TestLifecycleRemove_DeleteBranchUnmergedRefusesAndKeepsBranch(t *testing.T) {
	env := lifecycleTopicEnv(t)
	featurePath := env.CreateWorktree("backend", "api", "feature/unmerged", "api-unmerged")
	env.CreateCommit(featurePath, "unique work")

	_, err := lifecycleExec(t, "remove", "api", "feature/unmerged", "--yes", "--delete-branch")
	if err == nil {
		t.Fatal("unmerged branch deletion must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeGitFailed {
		t.Fatalf("code=%s, want git_failed", code)
	}
	exists, err := git.BranchExists(env.GetBarePath("api"), "feature/unmerged")
	if err != nil {
		t.Fatalf("BranchExists: %v", err)
	}
	if !exists {
		t.Fatal("branch must survive a refused --delete-branch")
	}
	if !env.DirExists(featurePath) {
		t.Fatal("worktree must remain when branch deletion is refused upfront")
	}
}

func TestLifecycleRemove_UnknownWorktreeIsNotSilentSuccess(t *testing.T) {
	_ = lifecycleTopicEnv(t)
	_, err := lifecycleExec(t, "remove", "api", "no-such-branch", "--yes")
	if err == nil {
		t.Fatal("removing a missing worktree must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeUnknown {
		t.Fatalf("code=%s, want worktree_unknown", code)
	}
}

func TestLifecycleRemove_DetachedWorktreeNotesNoBranchToDelete(t *testing.T) {
	env := lifecycleTopicEnv(t)
	stagePath := env.GetWorktreePath("backend", "api-stage")
	if out, err := exec.Command("git", "-C", stagePath, "checkout", "--detach").CombinedOutput(); err != nil {
		t.Fatalf("detach: %v\n%s", err, out)
	}

	stdout, err := lifecycleExec(t, "remove", "backend/api-stage", "--yes", "--delete-branch")
	if err != nil {
		t.Fatalf("remove detached: %v", err)
	}
	env2 := lifecycleDecode(t, stdout)
	found := false
	for _, w := range env2.Warnings {
		if w != nil && strings.Contains(w.Message, "no branch to delete") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected note about no branch to delete, warnings=%+v", env2.Warnings)
	}
}

func TestLifecycleSync_DirtyStashRestoresWorktree(t *testing.T) {
	env := lifecycleSyncDirtyEnv(t)
	stdout, err := lifecycleExec(t, "sync", "--yes", "--dirty", "stash")
	if err != nil {
		t.Fatalf("sync --dirty stash: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Pulled != 1 {
		t.Fatalf("pulled=%d, want 1", data.Summary.Pulled)
	}
	if !env.FileExists(env.GetWorktreePath("backend", "api") + "/dirty-file.txt") {
		t.Fatal("stash policy must restore dirty changes")
	}
}

func TestLifecycleSync_DirtyResetDiscardsChangesAndPulls(t *testing.T) {
	env := lifecycleSyncDirtyEnv(t)
	readme := env.GetWorktreePath("backend", "api") + "/README.md"
	if err := os.WriteFile(readme, []byte("local dirty"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}

	stdout, err := lifecycleExec(t, "sync", "--yes", "--dirty", "reset")
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
}

func TestLifecycleSync_DirtySkipLeavesWorktreeDirtyAndBehind(t *testing.T) {
	env := lifecycleSyncDirtyEnv(t)
	wt := env.GetWorktreePath("backend", "api")

	stdout, err := lifecycleExec(t, "sync", "--yes", "--dirty", "skip")
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
	tracking, err := git.WorktreeTracking(wt)
	if err != nil {
		t.Fatalf("tracking: %v", err)
	}
	if tracking.Behind == 0 {
		t.Fatal("skip must leave the worktree behind upstream")
	}
}

func TestLifecycleSync_BareMissingDegradesToPartialWhileOthersPull(t *testing.T) {
	env := lifecycleEnv(t)
	_, remoteA, _ := env.SetupRepo("backend", "api", "main")
	_, remoteB, _ := env.SetupRepo("backend", "web", "main")
	env.CommitToRemote(remoteA, "main", "ahead-a")
	env.CommitToRemote(remoteB, "main", "ahead-b")
	if err := os.RemoveAll(env.GetBarePath("web")); err != nil {
		t.Fatalf("remove bare: %v", err)
	}

	stdout, err := lifecycleExec(t, "sync", "--all", "--yes")
	if err == nil {
		t.Fatal("bare_missing must degrade the outcome")
	}
	if code := output.Classify(err).Code; code != output.CodePartialFailure {
		t.Fatalf("code=%s, want partial_failure", code)
	}
	payload := lifecycleDecode(t, stdout)
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
	tracking, err := git.WorktreeTracking(env.GetWorktreePath("backend", "api"))
	if err != nil {
		t.Fatalf("tracking api: %v", err)
	}
	if tracking.Behind != 0 {
		t.Fatalf("api should still pull, behind=%d", tracking.Behind)
	}
}

func TestLifecycleSync_PostSyncHookFailureDoesNotReportFetchFailure(t *testing.T) {
	env := lifecycleEnv(t)
	_, remote, _ := env.SetupRepo("backend", "api", "main", "stage")
	writeHooksConfig(t, env, remote, config.Hooks{
		PostSync: []config.Hook{{Run: "exit 1"}},
	})
	env.CreateWorktree("backend", "api", "stage", "api-stage")
	env.CommitToRemote(remote, "main", "upstream-main")
	env.CommitToRemote(remote, "stage", "upstream-stage")
	trustCurrentWorkspace(t)

	stdout, err := lifecycleExec(t, "sync", "--yes")
	if err != nil {
		t.Fatalf("post_sync hook failure must not fail sync: %v", err)
	}
	var data syncJSON
	decodeJSONData(t, stdout, &data)
	if data.Summary.Failed != 0 {
		t.Fatalf("failed=%d, want 0", data.Summary.Failed)
	}
	if data.Summary.Pulled < 2 {
		t.Fatalf("pulled=%d, want both worktrees", data.Summary.Pulled)
	}
	payload := lifecycleDecode(t, stdout)
	if len(payload.Warnings) == 0 {
		t.Fatal("hook failure must surface as a warning")
	}
}

func TestLifecycleRun_ExitStatusArgvJobsTimeoutAndNoShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX utilities required")
	}
	env := lifecycleEnv(t)
	env.SetupRepo("backend", "api", "main")
	env.SetupRepo("backend", "web", "main")

	t.Run("exit status", func(t *testing.T) {
		resetCommandState(t)
		stdout, _ := resetCommandIO()
		rootCmd.SetArgs([]string{"run", "backend/api", "--output", "json", "--", "sh", "-c", "exit 42"})
		if err := rootCmd.Execute(); err == nil {
			t.Fatal("non-zero exit must be reported")
		}
		var payload runJSON
		decodeJSONData(t, stdout, &payload)
		if payload.Results[0].ExitCode != 42 {
			t.Fatalf("exit_code=%d, want 42", payload.Results[0].ExitCode)
		}
	})

	t.Run("argv after --", func(t *testing.T) {
		payload := runPayload(t, "backend/api", "--output", "json", "--", "printf", "%s%s", "arg1", "arg2")
		if got := strings.TrimSpace(payload.Results[0].Stdout); got != "arg1arg2" {
			t.Fatalf("stdout=%q, want arg1arg2", got)
		}
	})

	t.Run("no shell unless asked", func(t *testing.T) {
		marker := env.RootDir + "/must-not-exist"
		payload := runPayload(t, "backend/api", "--output", "json", "--", "echo", "hello; touch "+marker)
		if payload.Failed != 0 {
			t.Fatalf("echo should succeed, got %+v", payload.Results)
		}
		if env.FileExists(marker) {
			t.Fatal("metacharacters must not be interpreted by a shell")
		}
	})

	t.Run("jobs", func(t *testing.T) {
		payload := runPayload(t, "--group", "backend", "--jobs", "2", "--output", "json", "--", "true")
		if payload.Total != 2 {
			t.Fatalf("total=%d, want 2", payload.Total)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		resetCommandState(t)
		stdout, _ := resetCommandIO()
		rootCmd.SetArgs([]string{"run", "backend/api", "--timeout", "100ms", "--output", "json", "--", "sleep", "5"})
		if err := rootCmd.Execute(); err == nil {
			t.Fatal("timeout must fail")
		}
		var payload runJSON
		decodeJSONData(t, stdout, &payload)
		if payload.TimedOut != 1 {
			t.Fatalf("timed_out=%d, want 1", payload.TimedOut)
		}
	})
}

func TestLifecycleRun_PartialFailureAcrossWorktrees(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX utilities required")
	}
	runEnv(t)

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"run", "--group", "backend", "--output", "json", "--", "sh", "-c", `test "$HYDRA_REPO" = api`})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected partial_failure")
	}
	he := output.Classify(err)
	if he.Code != output.CodePartialFailure || he.Exit != 4 {
		t.Fatalf("code=%s exit=%d, want partial_failure/4", he.Code, he.Exit)
	}
}

func TestLifecycleRun_TextModePrintsSummary(t *testing.T) {
	payload := runJSON{
		Command: []string{"true"},
		Total:   1,
		Results: []runResultJSON{{
			Group:  "backend",
			Repo:   "api",
			Branch: "main",
			MS:     2,
		}},
	}
	text := lifecycleCaptureStdout(t, func() {
		printRunText(payload, runSummary(payload))
	})
	if !strings.Contains(text, `"true" succeeded`) {
		t.Fatalf("text output missing summary, got:\n%s", text)
	}
	if !strings.Contains(text, "api/main") {
		t.Fatalf("text output missing worktree row, got:\n%s", text)
	}
}

func TestLifecycleTopic_ListShowAttachDetachCloseRemove(t *testing.T) {
	env := lifecycleTopicEnv(t)

	if err := runTopic(t, "attach", "epic", "backend/api-stage", "--output", "json"); err != nil {
		t.Fatalf("attach: %v", err)
	}
	got, ok, err := topic.Open(env.RootDir).Get("epic")
	if err != nil || !ok || len(got.Members) != 1 {
		t.Fatalf("attach must record membership, got ok=%v members=%+v err=%v", ok, got.Members, err)
	}

	if err := runTopic(t, "list", "--output", "json"); err != nil {
		t.Fatalf("list: %v", err)
	}
	if err := runTopic(t, "show", "epic", "--output", "json"); err != nil {
		t.Fatalf("show: %v", err)
	}

	if err := runTopic(t, "detach", "epic", "backend/api-stage", "--output", "json"); err != nil {
		t.Fatalf("detach: %v", err)
	}
	_, ok, err = topic.Open(env.RootDir).Get("epic")
	if err != nil {
		t.Fatalf("get after detach: %v", err)
	}
	if ok {
		t.Fatal("topic must be deleted when its last member detaches")
	}

	store := topic.Open(env.RootDir)
	if err := store.Attach("epic", topic.Member{Repo: "api", Branch: "main"}); err != nil {
		t.Fatalf("seed parent: %v", err)
	}
	if err := store.Attach("feat", topic.Member{Repo: "api", Branch: "stage"}); err != nil {
		t.Fatalf("seed child: %v", err)
	}
	if err := store.SetParent("feat", "epic"); err != nil {
		t.Fatalf("parent link: %v", err)
	}

	err = runTopic(t, "close", "epic", "--output", "json")
	if err == nil {
		t.Fatal("close must refuse while a child is open")
	}
	he := output.Classify(err)
	if he.Code != output.CodeTopicNotCloseable {
		t.Fatalf("code=%s, want topic_not_closeable", he.Code)
	}
	blocked, ok := he.Details["blocked_by"].([]blocker)
	if !ok || len(blocked) == 0 {
		t.Fatalf("details.blocked_by must name blockers, got %#v", he.Details["blocked_by"])
	}

	if err := store.SetClosed("feat", true); err != nil {
		t.Fatalf("close child: %v", err)
	}
	if err := runTopic(t, "close", "epic", "--output", "json"); err != nil {
		t.Fatalf("close parent after child closed: %v", err)
	}
	if err := runTopic(t, "close", "epic", "--reopen", "--output", "json"); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	parent, _, err := store.Get("epic")
	if err != nil || parent.Closed {
		t.Fatalf("topic must be reopened, closed=%v err=%v", parent.Closed, err)
	}

	err = runTopic(t, "show", "missing-topic", "--output", "json")
	if err == nil {
		t.Fatal("show on unknown id must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeTopicUnknown {
		t.Fatalf("code=%s, want topic_unknown", code)
	}
}

func TestLifecycleTopic_RemoveWithWorktreesFiresPostRemovePerWorktree(t *testing.T) {
	env := lifecycleTopicEnv(t)
	logPath := filepath.Join(env.RootDir, "post-remove.log")
	cfg := env.LoadConfig()
	cfg.Hooks.PostRemove = []config.Hook{{
		Run: `sh -c 'echo "$HYDRA_REPO/$HYDRA_BRANCH" >> "` + logPath + `"'`,
	}}
	env.SaveConfig(cfg)
	lifecycleLoadProject(t, env)
	trustCurrentWorkspace(t)

	store := topic.Open(env.RootDir)
	for _, branch := range []string{"main", "stage"} {
		if err := store.Attach("bundle", topic.Member{Repo: "api", Branch: branch}); err != nil {
			t.Fatalf("attach %s: %v", branch, err)
		}
	}

	if err := runTopic(t, "remove", "bundle", "--with-worktrees", "--yes", "--output", "json"); err != nil {
		t.Fatalf("topic remove: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(env.ReadFile(t, logPath)), "\n")
	if len(lines) != 2 {
		t.Fatalf("post_remove lines=%v, want one per worktree", lines)
	}
	combined := lines[0] + lines[1]
	if !strings.Contains(combined, "api/main") || !strings.Contains(combined, "api/stage") {
		t.Fatalf("post_remove must carry full context: %v", lines)
	}
}

func TestLifecyclePrune_DryRunVsRealAndStaleRecords(t *testing.T) {
	env := lifecycleEnv(t)
	_, _, mainWT := env.SetupRepo("backend", "api", "main", "stage")
	stageWT := env.CreateWorktree("backend", "api", "stage", "api-stage")
	_ = os.RemoveAll(stageWT)

	_, spareRemote := env.CreateBareRepo("spare-holder", "main")
	cfg := env.LoadConfig()
	cfg.SetRepo("spare", "holder", config.Repo{Remote: spareRemote, DefaultBranch: "main"})
	env.SaveConfig(cfg)
	if err := os.MkdirAll(filepath.Join(env.RootDir, "spare"), 0o755); err != nil {
		t.Fatalf("mkdir spare group: %v", err)
	}

	if _, err := lifecycleExec(t, "remove", "api", "main", "--yes"); err != nil {
		t.Fatalf("remove main: %v", err)
	}
	_ = mainWT

	if err := registry.Register("ghost", filepath.Join(env.RootDir, "missing-root")); err != nil {
		t.Fatalf("register ghost: %v", err)
	}

	goneRoot := filepath.Join(env.RootDir, "gone-trust")
	if err := os.MkdirAll(filepath.Join(goneRoot, ".hydra"), 0o755); err != nil {
		t.Fatalf("mkdir gone-trust: %v", err)
	}
	goneCfg := config.DefaultConfig("gone")
	if err := goneCfg.Save(config.ManifestPath(goneRoot)); err != nil {
		t.Fatalf("save gone manifest: %v", err)
	}
	if _, err := trust.Approve(global.GetConfigDir(), goneRoot, goneCfg, ""); err != nil {
		t.Fatalf("approve gone workspace: %v", err)
	}
	if err := os.RemoveAll(goneRoot); err != nil {
		t.Fatalf("remove gone-trust: %v", err)
	}

	dryStdout, err := lifecycleExec(t, "prune", "--dry-run")
	if err != nil {
		t.Fatalf("prune --dry-run: %v", err)
	}
	dry := lifecycleDecode(t, dryStdout)
	if !strings.Contains(dry.Summary, "would remove") {
		t.Fatalf("dry-run summary=%q, want would remove", dry.Summary)
	}
	var dryData pruneJSON
	if err := json.Unmarshal(dry.Data, &dryData); err != nil {
		t.Fatalf("decode dry prune: %v", err)
	}
	if !dryData.DryRun || len(dryData.PrunedWorktrees) == 0 {
		t.Fatalf("dry-run data=%+v", dryData)
	}

	realStdout, err := lifecycleExec(t, "prune")
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	real := lifecycleDecode(t, realStdout)
	if strings.Contains(real.Summary, "would remove") {
		t.Fatalf("real run summary must not say would remove: %q", real.Summary)
	}
	if !strings.Contains(real.Summary, "removed") {
		t.Fatalf("real run summary=%q, want removed", real.Summary)
	}
	var realData pruneJSON
	if err := json.Unmarshal(real.Data, &realData); err != nil {
		t.Fatalf("decode prune: %v", err)
	}
	if len(realData.PrunedWorktrees) == 0 {
		t.Fatal("expected pruned worktree registrations")
	}
	if len(realData.RemovedGroups) == 0 {
		t.Fatal("expected empty group directory removal")
	}
	if len(realData.PrunedProjects) == 0 {
		t.Fatal("expected dangling registry project prune")
	}
	if len(realData.PrunedTrust) == 0 {
		t.Fatal("expected stale trust entry prune")
	}
}

func TestLifecyclePrune_TextModePrintsSummary(t *testing.T) {
	result := pruneJSON{
		DryRun:          true,
		PrunedWorktrees: []string{"backend/api-stage"},
		RemovedGroups:   []string{"spare"},
	}
	text := lifecycleCaptureStdout(t, func() {
		printPruneText(result)
	})
	if !strings.Contains(text, "Prune Results") {
		t.Fatalf("text output missing title, got:\n%s", text)
	}
	if !strings.Contains(text, "api-stage") {
		t.Fatalf("text output missing pruned worktree, got:\n%s", text)
	}
}

func TestLifecycleProject_AddListRmExistsAndUnknown(t *testing.T) {
	env := lifecycleEnv(t)

	if _, err := lifecycleExec(t, "project", "add", "demo"); err != nil {
		t.Fatalf("project add: %v", err)
	}
	reg, err := registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if root, ok := reg.Resolve("demo"); !ok || root != env.RootDir {
		t.Fatalf("registry demo=%q ok=%v", root, ok)
	}

	stdout, err := lifecycleExec(t, "project", "ls")
	if err != nil {
		t.Fatalf("project ls: %v", err)
	}
	var list projectLsPayload
	decodeJSONData(t, stdout, &list)
	if len(list.Projects) != 1 || !list.Projects[0].Exists {
		t.Fatalf("projects=%+v", list.Projects)
	}

	other := filepath.Join(env.RootDir, "other")
	if err := os.MkdirAll(filepath.Join(other, ".hydra"), 0o755); err != nil {
		t.Fatalf("mkdir other: %v", err)
	}
	data := env.ReadFile(t, config.ManifestPath(env.RootDir))
	if err := os.WriteFile(config.ManifestPath(other), []byte(data), 0o600); err != nil {
		t.Fatalf("copy manifest: %v", err)
	}
	_, err = lifecycleExec(t, "project", "add", "demo", other)
	if err == nil {
		t.Fatal("duplicate project name must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeProjectExists {
		t.Fatalf("duplicate add code=%s, want project_exists", code)
	}

	_, err = lifecycleExec(t, "project", "rm", "missing")
	if err == nil {
		t.Fatal("unknown project rm must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeProjectUnknown {
		t.Fatalf("code=%s, want project_unknown", code)
	}

	if _, err := lifecycleExec(t, "project", "rm", "demo"); err != nil {
		t.Fatalf("project rm: %v", err)
	}
	reg, err = registry.Load()
	if err != nil {
		t.Fatalf("registry load: %v", err)
	}
	if _, ok := reg.Resolve("demo"); ok {
		t.Fatal("demo should be removed from registry")
	}
}

func TestLifecycleProject_InitCollisionReportsProjectExists(t *testing.T) {
	env := testutil.NewTestEnv(t)
	env.Chdir()
	if _, err := lifecycleExec(t, "init"); err != nil {
		t.Fatalf("first init: %v", err)
	}
	other := filepath.Join(env.RootDir, "other")
	if err := os.MkdirAll(other, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	_, err := lifecycleExec(t, "init", "--project-name", env.LoadConfig().Project, "--path", other)
	if err == nil {
		t.Fatal("second init with same project name must fail")
	}
	if code := output.Classify(err).Code; code != output.CodeProjectExists {
		t.Fatalf("code=%s, want project_exists", code)
	}
}

func TestLifecycleRepoSetBranches(t *testing.T) {
	env := lifecycleEnv(t)
	env.SetupRepo("backend", "api", "main", "stage", "prod")

	if _, err := lifecycleExec(t, "repo", "set", "api", "--branches", "main,stage"); err != nil {
		t.Fatalf("repo set: %v", err)
	}
	cfg, err := config.Load(config.ManifestPath(env.RootDir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	ref, _ := cfg.FindRepo("api")
	if got := strings.Join(ref.Repo.Branches, ","); got != "main,stage" {
		t.Fatalf("branches=%q, want main,stage", got)
	}
}

func TestLifecycleRepoRemoveKeepsPathsOnDisk(t *testing.T) {
	env := lifecycleEnv(t)
	env.SetupRepo("backend", "api", "main")
	bare := env.GetBarePath("api")
	wt := env.GetWorktreePath("backend", "api")

	stdout, err := lifecycleExec(t, "repo", "remove", "api", "--yes")
	if err != nil {
		t.Fatalf("repo remove: %v", err)
	}
	var payload repoRemoveJSON
	decodeJSONData(t, stdout, &payload)
	if len(payload.Kept) < 2 {
		t.Fatalf("kept=%v, want bare and worktree paths", payload.Kept)
	}
	if !env.DirExists(bare) || !env.DirExists(wt) {
		t.Fatal("repo remove must delete nothing on disk")
	}
	cfg, err := config.Load(config.ManifestPath(env.RootDir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if _, ok := cfg.FindRepo("api"); ok {
		t.Fatal("api should be unregistered")
	}
}

func TestLifecycleRepoRestore_AdditiveJobsAndPresentBare(t *testing.T) {
	env := lifecycleEnv(t)
	_, remoteA, _ := env.SetupRepo("g1", "api", "main")
	_, remoteB, _ := env.SetupRepo("g2", "web", "main")
	_ = remoteA
	_ = remoteB
	if err := config.Update(env.RootDir, func(live *config.Config) error {
		for _, alias := range []string{"api", "web"} {
			ref, _ := live.FindRepo(alias)
			ref.Repo.Branches = []string{"main"}
			live.SetRepo(ref.Group, ref.Alias, ref.Repo)
		}
		return nil
	}); err != nil {
		t.Fatalf("declare branches: %v", err)
	}

	bareA := env.GetBarePath("api")
	_ = os.RemoveAll(bareA)
	_ = os.RemoveAll(env.GetWorktreePath("g1", "api"))

	stdout, err := lifecycleExec(t, "repo", "restore", "--jobs", "2")
	if err != nil {
		t.Fatalf("repo restore: %v", err)
	}
	var payload restoreJSON
	decodeJSONData(t, stdout, &payload)
	if payload.Cloned == 0 {
		t.Fatal("restore should clone missing bare repos")
	}
	if !env.DirExists(bareA) {
		t.Fatal("api bare should be restored")
	}

	before := lifecycleBareModTime(t, bareA)
	stdout, err = lifecycleExec(t, "repo", "restore")
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	decodeJSONData(t, stdout, &payload)
	if payload.Present == 0 {
		t.Fatal("second restore should report present repos")
	}
	after := lifecycleBareModTime(t, bareA)
	if !before.Equal(after) {
		t.Fatal("additive restore must not rewrite an existing bare repo")
	}
}

func lifecycleBareModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}

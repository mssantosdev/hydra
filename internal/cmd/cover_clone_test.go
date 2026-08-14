package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

type cloneCovEnvelope struct {
	Outcome  string               `json:"outcome"`
	Summary  string               `json:"summary"`
	Warnings []*output.Diagnostic `json:"warnings"`
	Data     json.RawMessage      `json:"data"`
}

type cloneCovResult struct {
	Group     string         `json:"group"`
	Repo      string         `json:"repo"`
	Remote    string         `json:"remote"`
	BarePath  string         `json:"bare_path"`
	Worktrees []worktreeJSON `json:"worktrees"`
}

func cloneCovEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.Chdir()
	return env
}

func cloneCovExec(t *testing.T, args ...string) (*bytes.Buffer, error) {
	t.Helper()
	resetCommandState(t)
	stdout, _ := resetCommandIO()
	rootCmd.SetArgs(append([]string{"--output", "json"}, args...))
	err := rootCmd.Execute()
	return stdout, err
}

func cloneCovExecErr(t *testing.T, args ...string) error {
	t.Helper()
	_, err := cloneCovExec(t, args...)
	return err
}

func cloneCovDecode(t *testing.T, stdout *bytes.Buffer) cloneCovEnvelope {
	t.Helper()
	var env cloneCovEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, stdout.String())
	}
	return env
}

func cloneCovDecodeData(t *testing.T, stdout *bytes.Buffer) cloneCovResult {
	t.Helper()
	var payload cloneCovResult
	decodeJSONData(t, stdout, &payload)
	return payload
}

func cloneCovWarningCodes(env cloneCovEnvelope) []string {
	codes := make([]string, 0, len(env.Warnings))
	for _, w := range env.Warnings {
		if w != nil && w.Code != "" {
			codes = append(codes, w.Code)
		}
	}
	return codes
}

func cloneCovWorktreeBranches(payload cloneCovResult) []string {
	branches := make([]string, 0, len(payload.Worktrees))
	for _, wt := range payload.Worktrees {
		branches = append(branches, wt.Branch)
	}
	return branches
}

func TestCloneCov_ReAddSameURLConverges(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	args := []string{"repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main,stage"}

	if _, err := cloneCovExec(t, args...); err != nil {
		t.Fatalf("first repo add: %v", err)
	}
	firstMain := env.GetWorktreePath("backend", "api")
	firstStage := env.GetWorktreePath("backend", "api-stage")
	cfgAfterFirst := env.LoadConfig()
	refFirst, ok := cfgAfterFirst.FindRepo("api")
	if !ok {
		t.Fatal("api must be registered after the first clone")
	}

	stdout, err := cloneCovExec(t, args...)
	if err != nil {
		t.Fatalf("second repo add must exit 0, got: %v", err)
	}
	payload := cloneCovDecodeData(t, stdout)
	if len(payload.Worktrees) != 2 {
		t.Fatalf("converged run reported %d worktrees, want 2", len(payload.Worktrees))
	}
	if !env.DirExists(firstMain) || !env.DirExists(firstStage) {
		t.Fatal("re-add must not destroy existing worktrees")
	}

	cfgAfterSecond := env.LoadConfig()
	refSecond, ok := cfgAfterSecond.FindRepo("api")
	if !ok {
		t.Fatal("api must stay registered after a convergent re-add")
	}
	if refSecond.Group != refFirst.Group || refSecond.Repo.Remote != refFirst.Repo.Remote {
		t.Fatalf("registration changed across convergent re-add: %+v -> %+v", refFirst, refSecond)
	}
}

// TestClone_ResumesInterruptedClone covers an unregistered bare repo; this covers the
// manifest-first registration path where the bare directory is still absent.
func TestCloneCov_ResumesRegisteredWithoutBare(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.AddToConfig("backend", "api", remote, "")

	barePath := env.GetBarePath("api")
	if env.DirExists(barePath) {
		t.Fatalf("precondition: bare repo must not exist yet at %s", barePath)
	}

	if _, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main"); err != nil {
		t.Fatalf("repo add must finish a registered-but-bareless clone, got: %v", err)
	}
	if !env.DirExists(barePath) {
		t.Fatalf("bare repo must exist after resume at %s", barePath)
	}
	if !env.DirExists(env.GetWorktreePath("backend", "api")) {
		t.Fatal("main worktree must exist after resume")
	}
	ref, ok := env.LoadConfig().FindRepo("api")
	if !ok || ref.Repo.DefaultBranch != "main" {
		t.Fatalf("registration after resume = %+v, want default_branch main", ref)
	}
}

func TestCloneCov_ResumesRegisteredWithIncompleteBare(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")
	env.AddToConfig("backend", "api", remote, "")

	barePath := env.GetBarePath("api")
	if err := os.MkdirAll(filepath.Dir(barePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := exec.Command("git", "init", "--bare", barePath).Run(); err != nil {
		t.Fatalf("git init --bare: %v", err)
	}
	if git.GetConfig(barePath, "remote.origin.fetch") != "" {
		t.Fatal("fixture should start with no fetch refspec")
	}

	if _, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main"); err != nil {
		t.Fatalf("repo add must repair an incomplete bare repo, got: %v", err)
	}
	if got := git.GetConfig(barePath, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Errorf("fetch refspec = %q, want the standard refspec", got)
	}
	if !git.HasOriginHead(barePath) {
		t.Error("origin/HEAD must be set after repairing an incomplete bare repo")
	}
}

func TestCloneCov_BranchesCreatesNamedWorktrees(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage", "prod")

	stdout, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main,stage")
	if err != nil {
		t.Fatalf("repo add: %v", err)
	}
	payload := cloneCovDecodeData(t, stdout)
	got := strings.Join(cloneCovWorktreeBranches(payload), ",")
	if got != "main,stage" {
		t.Fatalf("worktree branches = %q, want main,stage", got)
	}
	if env.DirExists(env.GetWorktreePath("backend", "api-prod")) {
		t.Fatal("prod worktree must not exist when it was not requested")
	}
}

func TestCloneCov_AllCreatesEveryRemoteBranch(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage", "prod")

	stdout, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--all")
	if err != nil {
		t.Fatalf("repo add --all: %v", err)
	}
	payload := cloneCovDecodeData(t, stdout)
	if len(payload.Worktrees) != 3 {
		t.Fatalf("reported %d worktrees, want one per remote branch (3)", len(payload.Worktrees))
	}
	for _, branch := range []string{"main", "stage", "prod"} {
		dir := worktreeDirName(repoContext{Alias: "api"}, branch)
		if !env.DirExists(env.GetWorktreePath("backend", dir)) {
			t.Fatalf("worktree for branch %q was not created", branch)
		}
	}
}

func TestCloneCov_AdoptRejectsBranchesOrAll(t *testing.T) {
	env := cloneCovEnv(t)
	checkout := filepath.Join(env.RootDir, "imports", "plain-checkout")
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{
			name: "branches",
			args: []string{"repo", "add", checkout, "--adopt", "--group", "backend", "--branches", "main"},
		},
		{
			name: "all",
			args: []string{"repo", "add", checkout, "--adopt", "--group", "backend", "--all"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := cloneCovExecErr(t, tc.args...)
			if err == nil {
				t.Fatal("expected usage error")
			}
			if code := output.Classify(err).Code; code != output.CodeUsage {
				t.Fatalf("code = %q, want %q", code, output.CodeUsage)
			}
			if env.DirExists(env.GetBarePath("api")) {
				t.Fatal("usage error must not create a bare repository")
			}
		})
	}
}

func TestCloneCov_StaleBranchNameWarnsAndContinues(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")

	stdout, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "main,nope,stage")
	if err != nil {
		t.Fatalf("one stale branch name must not fail the clone, got: %v", err)
	}
	env2 := cloneCovDecode(t, stdout)
	if !strings.Contains(strings.Join(cloneCovWarningCodes(env2), " "), output.CodeBranchUnknown) {
		t.Fatalf("expected branch_unknown warning, got %+v", env2.Warnings)
	}
	foundStale := false
	for _, w := range env2.Warnings {
		if w != nil && strings.Contains(w.Message, "nope") {
			foundStale = true
			break
		}
	}
	if !foundStale {
		t.Fatalf("warning must name the stale branch, warnings=%+v", env2.Warnings)
	}
	payload := cloneCovDecodeData(t, stdout)
	got := strings.Join(cloneCovWorktreeBranches(payload), ",")
	if got != "main,stage" {
		t.Fatalf("worktree branches = %q, want main,stage", got)
	}
}

func TestCloneCov_InvalidAliasRefusesBeforeDisk(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main")

	err := cloneCovExecErr(t, "repo", "add", remote, "--as", "bad/alias", "--group", "backend", "--branches", "main")
	if err == nil {
		t.Fatal("invalid alias must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", code, output.CodeUsage)
	}
	if env.DirExists(env.GetBarePath("bad")) || env.DirExists(env.GetBarePath("bad/alias")) {
		t.Fatal("invalid alias must not create anything on disk")
	}
	if _, ok := env.LoadConfig().FindRepo("bad"); ok {
		t.Fatal("invalid alias must not register a repo")
	}
}

func TestCloneCov_InvalidGroupRefusesBeforeDisk(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main")

	err := cloneCovExecErr(t, "repo", "add", remote, "--as", "api", "--group", "bad/group", "--branches", "main")
	if err == nil {
		t.Fatal("invalid group must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", code, output.CodeUsage)
	}
	if env.DirExists(env.GetBarePath("api")) {
		t.Fatal("invalid group must not create a bare repository")
	}
	if _, ok := env.LoadConfig().FindRepo("api"); ok {
		t.Fatal("invalid group must not register a repo")
	}
}

func TestCloneCov_CrossGroupAliasCollisionRefused(t *testing.T) {
	env := cloneCovEnv(t)
	second := env.CreateRemoteRepo("web-origin", "main")
	env.SetupRepo("backend", "api", "main")

	err := cloneCovExecErr(t, "repo", "add", second, "--as", "api", "--group", "frontend", "--branches", "main")
	if err == nil {
		t.Fatal("cross-group alias collision must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeWorktreeExists {
		t.Fatalf("code = %q, want %q", code, output.CodeWorktreeExists)
	}
	if _, ok := env.LoadConfig().Groups["frontend"]; ok {
		t.Fatal("collision must not create the frontend group")
	}
	ref, _ := env.LoadConfig().FindRepo("api")
	if ref.Group != "backend" {
		t.Fatalf("existing alias moved groups: %+v", ref)
	}
}

func TestCloneCov_AdoptPreservesCheckoutGitData(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("upstream", "main")
	checkout := filepath.Join(env.RootDir, "imports", "api-checkout")
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	clone := exec.Command("git", "clone", remote, checkout) //nolint:gosec // G204: test fixture
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone checkout: %v\n%s", err, out)
	}
	originalGit := filepath.Join(checkout, ".git")
	infoBefore, err := os.Stat(originalGit)
	if err != nil {
		t.Fatalf("checkout .git before adopt: %v", err)
	}

	if _, err := cloneCovExec(t, "repo", "add", checkout, "--adopt", "--group", "backend", "--as", "api"); err != nil {
		t.Fatalf("adopt: %v", err)
	}
	infoAfter, err := os.Stat(originalGit)
	if err != nil {
		t.Fatalf("checkout .git after adopt: %v", err)
	}
	if infoBefore.ModTime() != infoAfter.ModTime() || infoBefore.Size() != infoAfter.Size() {
		t.Fatal("adopt must not rewrite the original checkout's git directory")
	}
}

func TestCloneCov_AdoptRejectsNonGitPath(t *testing.T) {
	env := cloneCovEnv(t)
	notGit := filepath.Join(env.RootDir, "imports", "plain-dir")
	if err := os.MkdirAll(notGit, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err := cloneCovExecErr(t, "repo", "add", notGit, "--adopt", "--group", "backend", "--as", "api")
	if err == nil {
		t.Fatal("non-git path must be refused")
	}
	if code := output.Classify(err).Code; code != output.CodeUsage {
		t.Fatalf("code = %q, want %q", code, output.CodeUsage)
	}
}

func TestCloneCov_UnreachableRemoteGitFailedWithCause(t *testing.T) {
	env := cloneCovEnv(t)

	err := cloneCovExecErr(t, "repo", "add", "file:///nope", "--as", "api", "--group", "backend", "--branches", "main")
	if err == nil {
		t.Fatal("unreachable remote must fail")
	}
	diag := output.Classify(err)
	if diag.Code != output.CodeGitFailed {
		t.Fatalf("code = %q, want %q", diag.Code, output.CodeGitFailed)
	}
	if diag.Cause == "" {
		t.Fatal("git_failed must populate cause with git's diagnosis")
	}
	if strings.Contains(diag.Cause, "exit status") {
		t.Fatalf("cause must carry git's words, not only an exit status: %q", diag.Cause)
	}
	if !strings.Contains(err.Error(), "Could not read from remote repository") {
		t.Fatalf("message must include git's diagnosis, got: %s", err.Error())
	}
	if env.DirExists(env.GetBarePath("api")) {
		t.Fatal("failed clone must roll back a freshly created bare repository")
	}
}

func TestCloneCov_DefaultBranchFallbackWithoutOriginHEAD(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("develop-only", "develop", "feature")

	stdout, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend")
	if err != nil {
		t.Fatalf("repo add without --branches: %v", err)
	}
	payload := cloneCovDecodeData(t, stdout)
	if len(payload.Worktrees) != 1 || payload.Worktrees[0].Branch != "develop" {
		t.Fatalf("worktrees = %+v, want a single develop worktree", payload.Worktrees)
	}

	barePath := env.GetBarePath("api")
	if err := exec.Command("git", "--git-dir="+barePath, "symbolic-ref", "-d", "refs/remotes/origin/HEAD").Run(); err != nil {
		t.Fatalf("unset origin/HEAD: %v", err)
	}

	stdout2, err := cloneCovExec(t, "repo", "add", remote, "--as", "api", "--group", "backend", "--branches", "feature")
	if err != nil {
		t.Fatalf("repo add with unset origin/HEAD must still succeed, got: %v", err)
	}
	payload2 := cloneCovDecodeData(t, stdout2)
	if !strings.Contains(strings.Join(cloneCovWorktreeBranches(payload2), ","), "feature") {
		t.Fatalf("second clone must still create requested worktrees, got %+v", payload2.Worktrees)
	}
}

func TestCloneCov_DryRunReportsPlanMetadata(t *testing.T) {
	env := cloneCovEnv(t)
	remote := env.CreateRemoteRepo("api-origin", "main", "stage")

	stdout, err := cloneCovExec(t, "repo", "add", remote, "--group", "backend", "--branches", "main,stage", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	var data map[string]any
	decodeJSONData(t, stdout, &data)
	if data["dry_run"] != true {
		t.Fatalf("dry_run = %#v, want true", data["dry_run"])
	}
	if data["branches_source"] != "flag" {
		t.Fatalf("branches_source = %#v, want flag", data["branches_source"])
	}
	branches, ok := data["branches"].([]any)
	if !ok || len(branches) != 2 {
		t.Fatalf("branches = %#v, want [main stage]", data["branches"])
	}
	if env.DirExists(env.GetBarePath("api-origin")) {
		t.Fatal("dry run must not create a bare repository")
	}
}

func TestCloneCov_AliasDerivedFromURL(t *testing.T) {
	_ = cloneCovEnv(t)

	stdout, err := cloneCovExec(t, "repo", "add", "https://github.com/acme/api-service.git", "--group", "backend", "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	payload := cloneCovDecodeData(t, stdout)
	if payload.Repo != "api-service" {
		t.Fatalf("derived alias = %q, want api-service", payload.Repo)
	}
}

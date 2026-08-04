package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/testutil"
)

func resetAdoptFlags() {
	adoptGroup = ""
	adoptAlias = ""
	adoptBranch = ""
}

func parseAdoptReport(t *testing.T, raw []byte) adoptJSON {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("invalid JSON envelope: %v\n%s", err, string(raw))
	}
	var report adoptJSON
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("invalid adopt report: %v\n%s", err, string(envelope.Data))
	}
	return report
}

func TestAdopt_ExistingCheckout(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	remote := env.CreateRemoteRepo("upstream", "main")
	checkout := filepath.Join(env.RootDir, "imports", "api-checkout")
	if err := os.MkdirAll(filepath.Dir(checkout), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	clone := exec.Command("git", "clone", remote, checkout) //nolint:gosec // G204: test fixture, constant binary
	if out, err := clone.CombinedOutput(); err != nil {
		t.Fatalf("clone checkout: %v\n%s", err, out)
	}

	env.Chdir()
	resetAdoptFlags()
	adoptGroup = "backend"

	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"repo", "add", checkout, "--adopt", "--group", "backend", "--as", "api"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("adopt: %v", err)
	}

	report := parseAdoptReport(t, buf.Bytes())
	if report.Group != "backend" || report.Repo != "api" {
		t.Fatalf("unexpected adopt metadata: %+v", report)
	}

	barePath := filepath.Join(env.RootDir, ".bare", "api.git")
	if !env.FileExists(barePath) {
		t.Fatalf("bare repo not created at %s", barePath)
	}
	if got := git.GetConfig(barePath, "remote.origin.fetch"); got != "+refs/heads/*:refs/remotes/origin/*" {
		t.Fatalf("unexpected fetch refspec: %q", got)
	}

	cfg := env.LoadConfig()
	if _, ok := cfg.Groups["backend"]["api"]; !ok {
		t.Fatal("alias not registered under group")
	}

	if len(report.Worktrees) == 0 {
		t.Fatal("expected at least one worktree")
	}
	found := false
	for _, wt := range report.Worktrees {
		if wt.Branch == "main" && wt.Upstream != nil && strings.HasSuffix(*wt.Upstream, "origin/main") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected main worktree with origin/main upstream, got %+v", report.Worktrees)
	}
	if env.Upstream(report.Worktrees[0].Path) != "origin/main" {
		t.Fatalf("worktree upstream = %q", env.Upstream(report.Worktrees[0].Path))
	}
}

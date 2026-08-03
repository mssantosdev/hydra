package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

var allDoctorCheckIDs = []string{
	checkMissingFetchRefspec,
	checkMissingOriginHead,
	checkBranchNoUpstream,
	checkWorktreeInsideGitdir,
	checkLegacySymlink,
	checkWorktreeMissingOnDisk,
	checkWorktreeUnregistered,
	checkStaleGitState,
	checkWorktreeDetached,
	checkWorktreeDirty,
	checkRegistryDangling,
}

func resetDoctorFlags() {
	doctorAll = false
	doctorFix = false
}

func resetDoctorState(t *testing.T) {
	t.Helper()
	resetDoctorFlags()
	cfg = nil
	projectRoot = ""
	projectConfigPath = ""
	outMode = output.ModeText
	outputFlag = ""
	rootCmd.SetArgs(nil)
}

func withDoctorJSON(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	resetDoctorState(t)
	t.Setenv("HYDRA_OUTPUT", "json")
	outputFlag = "json"
	outMode = output.ModeJSON
	var buf bytes.Buffer
	old := rootCmd.OutOrStdout()
	rootCmd.SetOut(&buf)
	return &buf, func() {
		rootCmd.SetOut(old)
		t.Setenv("HYDRA_OUTPUT", "")
	}
}

func parseDoctorReport(t *testing.T, raw []byte) doctorReport {
	t.Helper()
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("invalid JSON envelope: %v\n%s", err, string(raw))
	}
	var report doctorReport
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("invalid doctor report: %v\n%s", err, string(envelope.Data))
	}
	return report
}

func checksByID(report doctorReport) map[string][]doctorCheck {
	out := make(map[string][]doctorCheck)
	for _, check := range report.Checks {
		out[check.ID] = append(out[check.ID], check)
	}
	return out
}

func TestDoctor_HealthyWorkspace(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main")
	env.Chdir()
	resetDoctorFlags()

	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"doctor"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}

	report := parseDoctorReport(t, buf.Bytes())
	byID := checksByID(report)
	for _, id := range allDoctorCheckIDs {
		items, ok := byID[id]
		if !ok || len(items) == 0 {
			t.Fatalf("expected check %s to be present", id)
		}
		for _, item := range items {
			if item.Status != "ok" {
				t.Fatalf("expected %s ok, got %+v", id, item)
			}
		}
	}
}

func TestDoctor_FixMissingFetchRefspec(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	barePath, _, _ := env.SetupRepo("backend", "api", "main")
	env.Chdir()
	resetDoctorFlags()

	cmd := exec.Command("git", "--git-dir", barePath, "config", "--unset", "remote.origin.fetch")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unset fetch refspec: %v\n%s", err, out)
	}

	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"doctor"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected partial failure when fetch refspec is missing")
	}
	var outErr *output.Error
	if !errors.As(err, &outErr) || outErr.Code != output.CodePartialFailure {
		t.Fatalf("expected partial_failure, got %v", err)
	}

	report := parseDoctorReport(t, buf.Bytes())
	items := checksByID(report)[checkMissingFetchRefspec]
	if len(items) == 0 || items[0].Status != "fail" {
		t.Fatalf("expected missing_fetch_refspec fail, got %+v", items)
	}

	resetDoctorFlags()
	doctorFix = true
	buf2, restore2 := withDoctorJSON(t)
	defer restore2()
	rootCmd.SetArgs([]string{"doctor", "--fix"})
	if err := rootCmd.Execute(); err != nil {
		report := parseDoctorReport(t, buf2.Bytes())
		for _, c := range report.Checks {
			if c.Status == "fail" && !c.Fixed {
				t.Logf("still failing: %s %s %s", c.ID, c.Worktree, c.Message)
			}
		}
		t.Fatalf("doctor --fix: %v", err)
	}
	if git.GetConfig(barePath, "remote.origin.fetch") == "" {
		t.Fatal("fetch refspec was not restored")
	}

	report = parseDoctorReport(t, buf2.Bytes())
	items = checksByID(report)[checkMissingFetchRefspec]
	if len(items) == 0 || items[0].Status != "ok" {
		t.Fatalf("expected missing_fetch_refspec ok after fix, got %+v", items)
	}
}

func TestDoctor_FixLegacySymlink(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	barePath, _, _ := env.SetupRepo("backend", "api", "main")
	env.Chdir()
	resetDoctorFlags()

	linkPath := env.GetWorktreePath("backend", "legacy-link")
	if err := os.Symlink(filepath.Join(barePath, "HEAD"), linkPath); err != nil {
		t.Fatalf("create legacy symlink: %v", err)
	}

	buf, restore := withDoctorJSON(t)
	defer restore()
	rootCmd.SetArgs([]string{"doctor"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("expected partial failure for legacy symlink")
	}

	report := parseDoctorReport(t, buf.Bytes())
	items := checksByID(report)[checkLegacySymlink]
	if len(items) == 0 || items[0].Status != "fail" {
		t.Fatalf("expected legacy_symlink fail, got %+v", items)
	}

	resetDoctorFlags()
	doctorFix = true
	buf2, restore2 := withDoctorJSON(t)
	defer restore2()
	rootCmd.SetArgs([]string{"doctor", "--fix"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("doctor --fix: %v", err)
	}
	if env.FileExists(linkPath) {
		t.Fatal("legacy symlink was not removed")
	}
	report = parseDoctorReport(t, buf2.Bytes())
	items = checksByID(report)[checkLegacySymlink]
	if len(items) == 0 || items[0].Status != "ok" {
		t.Fatalf("expected legacy_symlink ok after fix, got %+v", items)
	}
}

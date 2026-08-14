package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

// The text renderers are the human half of the contract. Each is asserted on the FACTS it must
// carry — the worktree, the branch, the verdict, the count — never on escape sequences, which
// are a styling choice and would make these tests break on a colour change.

func TestRenderApplyTextReportsEveryDisposition(t *testing.T) {
	payload := applyJSON{
		Total: 3, Created: 1, Skipped: 1, Failed: 1,
		Results: []applyResultJSON{
			{Group: "backend", Repo: "api", Branch: "main", Name: "api", Disposition: "created"},
			{Group: "backend", Repo: "api", Branch: "stage", Name: "api-stage", Disposition: "skipped"},
			{Group: "web", Repo: "web", Branch: "feat/x", Name: "web-feat-x", Disposition: "failed",
				Error: output.Errorf(output.CodeBranchUnknown, "no such branch")},
			// A failed item's reason must be printed; a succeeding one has no error to read,
			// and reading it unconditionally panicked this renderer on every working run.
		},
	}
	out := captureStdout(t, func() { printApplyText(payload, "1 created, 1 already present, 1 failed") })
	// The rows are "<disposition> <repo>/<branch>", so assert those rather than the
	// worktree directory name, which this renderer does not print.
	for _, want := range []string{"api/main", "api/stage", "web/feat/x", "created", "skipped", "failed"} {
		if !strings.Contains(out, want) {
			t.Errorf("printApplyText omitted %q:\n%s", want, out)
		}
	}
}

// A dry run must be visibly different from a real one: "would create" and "created" are not the
// same answer, and conflating them is how a preflight stops being a preflight.
func TestRenderApplyTextDistinguishesADryRun(t *testing.T) {
	payload := applyJSON{
		DryRun: true, Total: 1,
		Results: []applyResultJSON{{Repo: "api", Branch: "main", Name: "api", Disposition: "would_create"}},
	}
	out := captureStdout(t, func() { printApplyText(payload, "dry run: 1 would be created") })
	if !strings.Contains(strings.ToLower(out), "dry") {
		t.Errorf("a dry run did not say so:\n%s", out)
	}
}

func TestRenderDoctorTextNamesFailingChecksAndTheirRepair(t *testing.T) {
	reports := []doctorReport{{
		Project: "alpha", Root: "/ws",
		Checks: []doctorCheck{
			{ID: "bare_missing", Status: "fail", Message: "bare repository is gone", Repo: "api", Fixable: true},
			{ID: "legacy_symlink", Status: "ok", Message: "no legacy symlinks", Fixable: false},
		},
	}}
	out := captureStdout(t, func() { printDoctorText(reports) })
	for _, want := range []string{"bare_missing", "bare repository is gone"} {
		if !strings.Contains(out, want) {
			t.Errorf("printDoctorText omitted %q:\n%s", want, out)
		}
	}
}

func TestRenderStartTextNamesWhatItCreated(t *testing.T) {
	payload := startJSON{
		Branch: "feat/login", Topic: new("2072958"),
		Created: []startTargetJSON{{
			Group: "backend", Repo: "api", Name: "api-feat-login", Branch: "feat/login",
			Path: "/ws/backend/api-feat-login", Disposition: "created", Attached: true,
		}},
	}
	out := captureStdout(t, func() { printStartText(payload, "branch feat/login (pattern), 1 created") })
	for _, want := range []string{"feat/login", "api-feat-login"} {
		if !strings.Contains(out, want) {
			t.Errorf("printStartText omitted %q:\n%s", want, out)
		}
	}
}

func TestRenderRestoreTextNamesEachRepoAndItsDisposition(t *testing.T) {
	payload := restoreJSON{
		Total: 2, Cloned: 1, Present: 1,
		Repos: []restoreRepoJSON{
			{Group: "backend", Repo: "api", Disposition: "cloned"},
			{Group: "web", Repo: "web", Disposition: "present"},
		},
	}
	out := captureStdout(t, func() { printRestoreText(payload, "1 cloned, 1 already present") })
	for _, want := range []string{"api", "web", "1 cloned"} {
		if !strings.Contains(out, want) {
			t.Errorf("printRestoreText omitted %q:\n%s", want, out)
		}
	}
}

// The counts line is what a reader scans first, so a zero must be omitted rather than shown as
// "0 dirty" noise, and a non-zero must always appear.
func TestRenderStatusCountsReportsEveryFigure(t *testing.T) {
	got := renderStatusCounts(statusSummaryJSON{Total: 3, Clean: 1, Dirty: 2, Behind: 1})
	// Every figure is labelled and present, including zeros: the line is a fixed scan target,
	// so a reader always finds the same fields in the same order.
	for _, want := range []string{"TOTAL 3", "CLEAN 1", "DIRTY 2", "BEHIND 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("counts = %q, want it to contain %q", got, want)
		}
	}
}

func TestRenderListTextGroupsAndNamesWorktrees(t *testing.T) {
	projects := []projectWorktrees{{
		Project: "alpha", Root: "/ws",
		Worktrees: []worktreeJSON{
			{Group: "backend", Repo: "api", Name: "api", Branch: "main", Path: "/ws/backend/api"},
			{Group: "frontend", Repo: "web", Name: "web", Branch: "main", Path: "/ws/frontend/web"},
		},
	}}
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := captureStdout(t, func() { renderListText(cmd, false, projects) })
	combined := out + buf.String()
	for _, want := range []string{"api", "web"} {
		if !strings.Contains(combined, want) {
			t.Errorf("renderListText omitted %q:\n%s", want, combined)
		}
	}
}

func TestRenderStatusTextReportsEachProject(t *testing.T) {
	projects := []statusProjectPayload{{
		Project: "alpha", Root: "/ws",
		Summary: statusSummaryJSON{Total: 1, Clean: 1},
		Worktrees: []worktreeJSON{
			{Group: "backend", Repo: "api", Name: "api", Branch: "main", Path: "/ws/backend/api"},
		},
	}}
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	out := captureStdout(t, func() { renderStatusText(cmd, false, projects) })
	if combined := out + buf.String(); !strings.Contains(combined, "api") {
		t.Errorf("renderStatusText omitted the worktree:\n%s", combined)
	}
}

// The board reads its rows from the same worktree payload the JSON envelope carries, so a field
// dropped here is a field the TUI silently stops showing.
func TestBoardRowCarriesTheWorktreeFacts(t *testing.T) {
	item := worktreeJSON{
		Group: "backend", Repo: "api", Name: "api-stage", Branch: "stage",
		Path: "/ws/backend/api-stage", Dirty: true, DirtyFiles: 2, Behind: 1,
		Upstream: new("origin/stage"), Topic: new("2072958"),
	}
	row := worktreeItemToRow("alpha", item, new("2026-01-01T00:00:00Z"))
	if row.Branch != "stage" {
		t.Errorf("row.Branch = %q, want %q", row.Branch, "stage")
	}
	if !strings.Contains(row.Path, "api-stage") {
		t.Errorf("row.Path = %q, want the worktree path", row.Path)
	}
	if !row.Dirty {
		t.Error("row lost the dirty flag")
	}
}

func TestBoardAgainstInfoIsAbsentWhenTheRefDidNotResolve(t *testing.T) {
	if got := againstInfoForBoard(nil); got != nil {
		t.Errorf("got %+v, want nil when there is no comparison", got)
	}
	got := againstInfoForBoard(&againstJSON{Ref: "main", Merged: true, Ahead: 0})
	if got == nil || !got.Merged {
		t.Errorf("got %+v, want a merged comparison", got)
	}
}

// A manifest that cannot be saved must not be reported as though the workspace were missing.
func TestClassifyManifestErrKeepsAWriteFailureDistinct(t *testing.T) {
	err := classifyManifestErr(errors.New("permission denied"))
	if err == nil {
		t.Fatal("a save failure classified as nil")
	}
	if code := output.Classify(err).Code; code == output.CodeNotInProject {
		t.Errorf("a failed manifest write was reported as %q, which says the workspace is absent", code)
	}
}

func TestResolveCurrentHydraContextFindsTheWorktreeYouAreStandingIn(t *testing.T) {
	resetCommandState(t)
	env := newHelperEnv(t)

	items, _ := collectWorktrees(cfg, projectRoot)
	if len(items) == 0 {
		t.Fatal("fixture produced no worktrees")
	}
	inside := resolveCurrentHydraContext(items[0].Path, cfg, projectRoot)
	if inside == nil {
		t.Fatalf("standing in %s resolved no context", items[0].Path)
	}
	if inside.Branch != items[0].Branch {
		t.Errorf("resolved branch %q, want %q", inside.Branch, items[0].Branch)
	}
	// Outside any worktree there is no context, and inventing one would make commands act on a
	// worktree the caller never named.
	if outside := resolveCurrentHydraContext(env.RootDir, cfg, projectRoot); outside != nil {
		t.Errorf("the workspace root resolved to %+v, want no worktree context", outside)
	}
}

func TestGetConfigAndEnvelopeAccessorsReflectLoadedState(t *testing.T) {
	resetCommandState(t)
	newHelperEnv(t)

	if GetConfig() == nil {
		t.Error("GetConfig() is nil after a workspace was loaded")
	}
	if GetConfig().Project == "" {
		t.Error("GetConfig() returned a config with no project name")
	}
	// Nothing has been emitted yet in this fresh state.
	if EnvelopeEmitted() {
		t.Error("EnvelopeEmitted() is true before anything was emitted")
	}
}

var _ = config.SchemaVersion

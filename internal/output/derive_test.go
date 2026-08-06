package output

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The one invariant that subsumes five separately-shipped bugs.
//
// Each was fixed where it was found, so the class reappeared in whichever command
// aggregated next — including in code written specifically to prevent it. The rule is that
// a verdict is DERIVED from coverage, never asserted by the command that produced it.
func TestCoverageDerivesTheOnlyConsistentOutcome(t *testing.T) {
	tests := []struct {
		name string
		c    Coverage
		want Outcome
	}{
		{"everything inspected, nothing failed", Coverage{Claimed: 9, Inspected: 9}, OutcomeSuccess},
		{"nothing claimed is vacuously fine", Coverage{}, OutcomeSuccess},

		// The status bug: 9 registered, 8 inspected, reported "all clean".
		{"fewer inspected than claimed is partial", Coverage{Claimed: 9, Inspected: 8}, OutcomePartial},

		// The run bug: some failed, some landed.
		{"some failed is partial", Coverage{Claimed: 4, Inspected: 4, Failed: 1}, OutcomePartial},

		// The run bug's other half: everything failed but it still said partial.
		{"all failed is failure, not partial", Coverage{Claimed: 4, Inspected: 4, Failed: 4}, OutcomeFailure},
		{"single item failing is failure", Coverage{Claimed: 1, Inspected: 1, Failed: 1}, OutcomeFailure},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Derive(); got != tc.want {
				t.Errorf("Derive() = %q, want %q", got, tc.want)
			}
			if complete := tc.c.Complete(); complete != (tc.want == OutcomeSuccess) {
				t.Errorf("Complete() = %v, disagrees with outcome %q", complete, tc.want)
			}
		})
	}
}

// A fault warning describes the workspace being wrong, not the request being unusual, so it
// may never sit beside `outcome: success`. `status` reported "all clean" with exactly such a
// warning present, and a caller gating on outcome or exit status saw nothing.
func TestFaultWarningsAreDistinguishedFromAdvice(t *testing.T) {
	faults := []string{
		"worktree_unknown: g/api: git status failed: fatal: cannot change to '/gone'",
		"bare_missing: g/api: bare repository missing at .bare/api.git",
		"api.git is not registered in the manifest",
		"topic state unreadable: bad yaml",
	}
	for _, w := range faults {
		if !HasFault([]string{w}) {
			t.Errorf("should be a fault: %q", w)
		}
	}

	advice := []string{
		"--filter branch:nope-* matched none of the 9 worktree(s) in this project",
		"registered in /home/u/.config/hydra/projects.yaml",
		"2 detached worktree(s) skipped: a branchless worktree cannot be described by a branch",
	}
	for _, w := range advice {
		if HasFault([]string{w}) {
			t.Errorf("advice must not force a partial: %q", w)
		}
	}
}

// The enforcement point: a command cannot claim success while carrying an error or a fault
// warning. This is asserted at the envelope rather than per command, because per-command
// assertions are the thing that kept being forgotten.
func TestEmitJSONCorrectsAnOverclaimedOutcome(t *testing.T) {
	tests := []struct {
		name string
		r    Result
		want Outcome
	}{
		{
			name: "an error on the envelope forbids success",
			r:    Result{Outcome: OutcomeSuccess, Err: Errorf(CodeHookFailed, "hook failed")},
			want: OutcomePartial,
		},
		{
			name: "a fault warning forbids success",
			r:    Result{Outcome: OutcomeSuccess, Warnings: []string{"worktree missing on disk"}},
			want: OutcomePartial,
		},
		{
			name: "advice leaves success alone",
			r:    Result{Outcome: OutcomeSuccess, Warnings: []string{"matched none of the 9 worktree(s)"}},
			want: OutcomeSuccess,
		},
		{
			name: "an explicit failure is not downgraded",
			r:    Result{Outcome: OutcomeFailure, Err: Errorf(CodeGitFailed, "all failed")},
			want: OutcomeFailure,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf writerStub
			if err := EmitJSON(&buf, "test", tc.r); err != nil {
				t.Fatalf("EmitJSON: %v", err)
			}
			if got := buf.outcome(t); got != string(tc.want) {
				t.Errorf("outcome = %q, want %q", got, tc.want)
			}
		})
	}
}

// writerStub captures an envelope and reads one field back, so the assertion is about the
// serialised contract rather than the struct.
type writerStub struct{ bytes.Buffer }

func (w *writerStub) outcome(t *testing.T) string {
	t.Helper()
	var envelope struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(w.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not valid JSON: %v\n%s", err, w.String())
	}
	return envelope.Outcome
}

// The exit status is derived from what reached stdout, not from what the command returned.
//
// This closes the last way the aggregate-lie class could regenerate. The outcome was already
// corrected inside the envelope, but the exit came from the command's return value — so a
// command could emit a corrected `partial` and then return nil, exiting 0 while the caller
// had just been told something failed. `sync` did precisely that twice in one release: once
// on its normal path and once on a "nothing to pull" early return that skipped the outcome
// logic altogether. A command that has not been written yet cannot reintroduce it.
func TestEmittedVerdictDrivesTheExitStatus(t *testing.T) {
	tests := []struct {
		name     string
		result   Result
		wantCode string
		wantExit int
	}{
		{
			name:     "a clean success leaves the exit alone",
			result:   Result{Summary: "all good"},
			wantCode: "",
			wantExit: 0,
		},
		{
			name:     "an emitted partial carries the partial exit",
			result:   Result{Outcome: OutcomePartial, Err: Errorf(CodePartialFailure, "some failed")},
			wantCode: CodePartialFailure,
			wantExit: 4,
		},
		{
			name: "an emitted fault warning alone still moves the exit",
			// No error, no explicit outcome: the envelope promotes this to partial, and
			// the exit has to follow even though the command said nothing was wrong.
			result:   Result{Warnings: []string{"worktree_unknown: g/api: gone"}},
			wantCode: CodePartialFailure,
			wantExit: 4,
		},
		{
			name:     "a hook failure keeps its own code and exit",
			result:   Result{Err: Errorf(CodeHookFailed, "hook exited 1")},
			wantCode: CodeHookFailed,
			wantExit: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ResetVerdict()
			var buf writerStub
			if err := EmitJSON(&buf, "test", tc.result); err != nil {
				t.Fatalf("EmitJSON: %v", err)
			}

			outcome, code := EmittedVerdict()
			if code != tc.wantCode {
				t.Errorf("code = %q, want %q", code, tc.wantCode)
			}
			if tc.wantExit == 0 {
				if outcome != OutcomeSuccess {
					t.Errorf("outcome = %q, want success", outcome)
				}
				return
			}
			if outcome == OutcomeSuccess {
				t.Fatalf("outcome = success, but exit %d was expected", tc.wantExit)
			}
			if got := ExitFor(code); got != tc.wantExit {
				t.Errorf("ExitFor(%q) = %d, want %d", code, got, tc.wantExit)
			}
		})
	}
}

// A fresh process starts clean, so a command that emits nothing cannot inherit a verdict.
func TestVerdictStartsClean(t *testing.T) {
	ResetVerdict()
	if outcome, code := EmittedVerdict(); outcome != OutcomeSuccess || code != "" {
		t.Errorf("EmittedVerdict() = (%q, %q), want (success, \"\")", outcome, code)
	}
}

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

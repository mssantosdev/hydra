package branchresolve

import (
	"strings"
	"testing"
)

// These two errors are what a caller sees when branch naming fails, and each is mapped onto a
// distinct output code. The message must name the subject, or the code is all the caller gets.
func TestProviderErrorDistinguishesATimeoutFromAFailure(t *testing.T) {
	timedOut := (&ProviderError{Provider: "./scripts/name", Repo: "api", TimedOut: true}).Error()
	if !strings.Contains(timedOut, "timed out") {
		t.Errorf("a timeout does not say so: %q", timedOut)
	}
	failed := (&ProviderError{Provider: "./scripts/name", Repo: "api", ExitCode: 3}).Error()
	if strings.Contains(failed, "timed out") {
		t.Errorf("a non-zero exit was reported as a timeout: %q", failed)
	}
	// The exit status is the one fact a caller cannot derive, so it must appear.
	if !strings.Contains(failed, "3") {
		t.Errorf("the exit status is missing: %q", failed)
	}
	for _, want := range []string{"./scripts/name", "api"} {
		if !strings.Contains(failed, want) {
			t.Errorf("message %q does not name %q", failed, want)
		}
	}
}

func TestInvalidBranchErrorNamesTheBranchAndTheReason(t *testing.T) {
	err := (&InvalidBranchError{Branch: "feat/..bad", Reason: "git will not accept it"}).Error()
	for _, want := range []string{"feat/..bad", "git will not accept it"} {
		if !strings.Contains(err, want) {
			t.Errorf("message %q does not name %q", err, want)
		}
	}
}

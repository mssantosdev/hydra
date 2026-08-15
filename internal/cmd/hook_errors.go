package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/mssantosdev/hydra/internal/output"
)

// emitTextFailure renders hook failures for text mode and adopts the exit verdict.
// It returns true when err was fully handled and Execute should return nil.
func emitTextFailure(err error) bool {
	if err == nil || machineMode() {
		return false
	}
	e := output.Classify(err)
	if e.Code != output.CodeHookFailed {
		return false
	}
	writeHookFailureText(os.Stderr, e)
	output.AdoptTextFailure(e)
	return true
}

func writeHookFailureText(w io.Writer, e *output.Error) {
	_, _ = fmt.Fprintf(w, "error: %s\n", e.Message)
}

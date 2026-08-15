package main

import (
	"fmt"
	"os"

	"github.com/mssantosdev/hydra/internal/cmd"
	"github.com/mssantosdev/hydra/internal/output"
)

// main is the single exit-code authority: every user-facing failure carries a
// stable code from internal/output, and its exit status comes from that code.
func main() {
	name, err := cmd.Execute()
	if err == nil {
		// A nil return is NOT proof of success. The outcome is corrected inside the
		// envelope, so a command could emit `partial` and still return nil — exiting 0
		// while the caller had just been told something failed. `sync` did that twice in
		// one release, once on its normal path and once on an early return that skipped
		// the outcome logic. Deriving the exit from what actually reached stdout closes
		// that off for every command at once, including ones not yet written.
		if outcome, code := output.EmittedVerdict(); outcome != output.OutcomeSuccess && code != "" {
			os.Exit(output.ExitFor(code))
		}
		return
	}

	e := output.Classify(err)
	switch {
	case !cmd.ErrorsAsEnvelope():
		fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
	case cmd.EnvelopeEmitted():
		// A partial already wrote one envelope carrying both the data and this error.
		// A second envelope on the same stream would be unparseable.
	default:
		// Same format the success path would have used: a script that asked for YAML must
		// not get a JSON error envelope on the failing invocation.
		if emitErr := output.EmitError(os.Stdout, name, e, cmd.WireMode()); emitErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
		}
	}
	os.Exit(e.Exit)
}

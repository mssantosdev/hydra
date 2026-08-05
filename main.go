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
		return
	}

	e := output.Classify(err)
	switch {
	case !cmd.ErrorsAsJSON():
		fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
	case cmd.EnvelopeEmitted():
		// A partial already wrote one envelope carrying both the data and this error.
		// A second envelope on the same stream would be unparseable.
	default:
		if emitErr := output.EmitError(os.Stdout, name, e); emitErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
		}
	}
	os.Exit(e.Exit)
}

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
	if cmd.ErrorsAsJSON() {
		if emitErr := output.EmitError(os.Stderr, name, e); emitErr != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
		}
	} else {
		fmt.Fprintf(os.Stderr, "Error: %v\n", e.Message)
	}
	os.Exit(e.Exit)
}

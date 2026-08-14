package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/huh"

	"github.com/mssantosdev/hydra/internal/output"
)

// promptIn and promptOut are where every prompt reads and draws.
//
// They exist so prompts are not wired directly to the process streams. Two things follow from
// that, and the second is why this file exists at all:
//
//   - A prompt draws on STDERR. stdout carries the JSON envelope, and a form painting into it
//     would corrupt the one output a caller parses.
//   - The interactive paths become testable. Every branch behind a confirm or a select was
//     unreachable from `go test` while `.Run()` read the real terminal, so the code that decides
//     what happens when you say "no" was verified by nobody.
var (
	promptIn  io.Reader = os.Stdin
	promptOut io.Writer = os.Stderr

	// promptPolicy decides whether a question may be asked at all. It defaults to the real
	// check — a text-mode invocation with terminals on both ends — and exists as a variable so
	// the branches behind a prompt are reachable without one.
	promptPolicy = func() bool { return output.Interactive(outMode) }
)

// runForm runs a prompt against the current input and output.
//
// Every prompt in hydra goes through here rather than calling Run() itself, for the same reason
// the trust gate sits at one funnel: a site that bypasses it silently gets different behaviour,
// and nothing would notice.
//
// It also recovers. When TERM=dumb — which CI runners and editor shells set routinely — huh
// switches to accessible mode, where a select reads a TYPED NUMBER instead of arrow keys and
// panics with an index out of range on anything it cannot parse. A mistyped answer to a question
// must not crash the process, so the panic becomes an ordinary cancellation.
func runForm(form *huh.Form) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = output.Errorf(output.CodeCancelled,
				"the prompt could not read an answer (%v); pass the value as a flag instead", r)
		}
	}()
	return form.WithInput(promptIn).WithOutput(promptOut).Run()
}

// runConfirm asks a yes/no question and returns the answer.
func runConfirm(title string) (bool, error) {
	confirm := false
	err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Value(&confirm),
	)))
	return confirm, err
}

// runSelect offers a closed set and returns the choice.
//
// initial positions the cursor. It is not cosmetic: `add`'s branch prompt opens on the repo's
// default branch, and without it the cursor lands on "+ new branch…" — so the fastest keystroke
// would create a branch instead of picking the obvious one. Pass "" when there is no default.
func runSelect(title string, options []huh.Option[string], initial string) (string, error) {
	selected := initial
	err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title(title).Options(options...).Value(&selected),
	)))
	return selected, err
}

// promptf writes chrome to the prompt stream. A failed decoration must not become an error the
// caller has to handle: the question itself reports whether it was answered.
func promptf(format string, args ...any) {
	_, _ = fmt.Fprintf(promptOut, format, args...)
}

// runInput asks for a single value and returns it.
func runInput(title string) (string, error) {
	value := ""
	err := runForm(huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(title).Value(&value),
	)))
	return value, err
}

// Package output owns hydra's machine-readable contract: the --output mode, the
// JSON envelope, TTY/color detection, and the stable error-code enum.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Schema is the envelope schema version.
const Schema = 2

// Mode selects the rendering of a command's result.
type Mode int

const (
	// ModeAuto emits JSON when stdout is not a TTY, text otherwise.
	ModeAuto Mode = iota
	ModeText
	ModeJSON
)

func (m Mode) String() string {
	switch m {
	case ModeText:
		return "text"
	case ModeJSON:
		return "json"
	}
	return "auto"
}

// Resolve parses an --output flag value. An empty value falls back to
// HYDRA_OUTPUT, then to auto.
func Resolve(flag string) (Mode, error) {
	value := strings.TrimSpace(strings.ToLower(flag))
	if value == "" || value == "auto" {
		if env := strings.TrimSpace(strings.ToLower(os.Getenv("HYDRA_OUTPUT"))); env != "" && value == "" {
			value = env
		}
	}
	switch value {
	case "", "auto":
		return ModeAuto, nil
	case "text", "plain":
		return ModeText, nil
	case "json":
		return ModeJSON, nil
	}
	return ModeAuto, Errorf(CodeInternal, "invalid --output value %q (want auto, text, or json)", flag)
}

// Effective collapses ModeAuto against the real terminal state of out.
func Effective(m Mode, out *os.File) Mode {
	if m != ModeAuto {
		return m
	}
	if isTTY(out) {
		return ModeText
	}
	return ModeJSON
}

// Color reports whether ANSI color may be written to out.
func Color(out *os.File) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}
	return isTTY(out)
}

// Interactive reports whether prompts may be shown: stdin and stdout must both
// be terminals, and the effective mode must be text.
func Interactive(m Mode) bool {
	return Effective(m, os.Stdout) == ModeText && isTTY(os.Stdin) && isTTY(os.Stdout)
}

func isTTY(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Outcome is the envelope-level verdict, so a consumer reading stdout alone knows
// how the run went without parsing data or interpreting an exit status.
type Outcome string

const (
	// OutcomeSuccess means every item reached its desired state.
	OutcomeSuccess Outcome = "success"
	// OutcomePartial means some items succeeded and some failed. It appears on a
	// SUCCESS envelope: the data is real and must not be thrown away just because
	// the process will also exit 4 and print an error envelope on stderr.
	OutcomePartial Outcome = "partial"
	// OutcomeFailure is carried by error envelopes.
	OutcomeFailure Outcome = "failure"
)

// Next is a suggested follow-up command.
//
// Named next, not breadcrumbs: breadcrumbs mean where you came from. It may
// suggest attaching after an unknown topic, but hydra must never act on it.
type Next struct {
	Action string `json:"action"`
	Cmd    string `json:"cmd"`
}

type successEnvelope struct {
	Schema   int      `json:"schema"`
	Command  string   `json:"command"`
	Outcome  Outcome  `json:"outcome"`
	Summary  string   `json:"summary"`
	Data     any      `json:"data"`
	Next     []Next   `json:"next,omitempty"`
	Warnings []string `json:"warnings"`
}

type errorEnvelope struct {
	Schema  int     `json:"schema"`
	Command string  `json:"command"`
	Outcome Outcome `json:"outcome"`
	Error   *Error  `json:"error"`
}

// Result is what a command emits on success.
//
// Summary is a required one-line answer. It exists because the alternative — a
// caller reconstructing "what happened" from data — is the exact cost this envelope
// is supposed to remove, for a human reading a terminal and an agent alike.
type Result struct {
	Outcome  Outcome
	Summary  string
	Data     any
	Next     []Next
	Warnings []string
}

// EmitJSON writes a success envelope.
func EmitJSON(w io.Writer, cmd string, r Result) error {
	if r.Outcome == "" {
		r.Outcome = OutcomeSuccess
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	envelope := successEnvelope{
		Schema:   Schema,
		Command:  cmd,
		Outcome:  r.Outcome,
		Summary:  r.Summary,
		Data:     r.Data,
		Next:     r.Next,
		Warnings: r.Warnings,
	}
	if err := enc.Encode(envelope); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	return nil
}

// EmitError writes an error envelope. Callers write it to stderr.
func EmitError(w io.Writer, cmd string, e *Error) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	envelope := errorEnvelope{Schema: Schema, Command: cmd, Outcome: OutcomeFailure, Error: e}
	if err := enc.Encode(envelope); err != nil {
		return fmt.Errorf("failed to encode error output: %w", err)
	}
	return nil
}

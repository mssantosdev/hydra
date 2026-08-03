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
const Schema = 1

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

type successEnvelope struct {
	Schema   int      `json:"schema"`
	Command  string   `json:"command"`
	Data     any      `json:"data"`
	Warnings []string `json:"warnings"`
}

type errorEnvelope struct {
	Schema  int    `json:"schema"`
	Command string `json:"command"`
	Error   *Error `json:"error"`
}

// EmitJSON writes a success envelope.
func EmitJSON(w io.Writer, cmd string, data any, warnings []string) error {
	if warnings == nil {
		warnings = []string{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(successEnvelope{Schema: Schema, Command: cmd, Data: data, Warnings: warnings}); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	return nil
}

// EmitError writes an error envelope. Callers write it to stderr.
func EmitError(w io.Writer, cmd string, e *Error) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(errorEnvelope{Schema: Schema, Command: cmd, Error: e}); err != nil {
		return fmt.Errorf("failed to encode error output: %w", err)
	}
	return nil
}

// Package output owns hydra's machine-readable contract: the --output mode, the
// JSON envelope, TTY/color detection, and the stable error-code enum.
package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"gopkg.in/yaml.v3"
)

// Schema is the envelope schema version.
// Schema is the ENVELOPE version. It is unrelated to the manifest's `version: "2"`,
// which is a separate contract and unchanged.
//
// 3 because this release moved the failure envelope from stderr to stdout, reshaped
// `next[]` from {action, cmd} to {argv, why}, and stopped reporting `outcome: partial`
// when nothing at all succeeded. Each of those breaks a consumer written against 2.
const Schema = 3

// Mode selects the rendering of a command's result.
type Mode int

const (
	// ModeAuto emits JSON when stdout is not a TTY, text otherwise.
	ModeAuto Mode = iota
	ModeText
	ModeJSON
	// ModeYAML emits the SAME envelope as ModeJSON in YAML. It is never inferred: a
	// pipe gets JSON unless YAML was asked for, because JSON is what every existing
	// consumer parses.
	ModeYAML
)

func (m Mode) String() string {
	switch m {
	case ModeText:
		return "text"
	case ModeJSON:
		return "json"
	case ModeYAML:
		return "yaml"
	}
	return "auto"
}

// Machine reports whether the mode emits a parseable envelope rather than prose.
//
// Every branch that used to ask "is this JSON" means this: suppress progress, emit an
// envelope, do not prompt. Asking about JSON specifically is what would leave YAML
// half-supported — a YAML success envelope followed by a JSON error envelope in the same
// script is a broken contract, not a formatting detail.
func (m Mode) Machine() bool { return m == ModeJSON || m == ModeYAML }

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
	case "yaml", "yml":
		return ModeYAML, nil
	}
	return ModeAuto, Errorf(CodeUsage,
		"invalid --output value %q (want auto, text, json, or yaml)", flag)
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
// be terminals, the effective mode must be text, and TERM must not be "dumb".
//
// TERM=dumb is excluded because it changes what an answer MEANS. huh switches to accessible
// mode there, and accessible mode cannot report an abort: Ctrl-C and a closed stdin both come
// back as "the defaults were accepted". `hydra sync` pulls and stashes, so a swallowed abort is
// consent nobody gave. Falling through to the non-interactive path is strictly safer — it
// refuses with needs_input naming the flag to pass instead of acting on a guess.
func Interactive(m Mode) bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
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
	// OutcomePartial means some items succeeded and some failed. Data and error ride
	// the SAME envelope: the data is real, the failure is real, and splitting them
	// across two streams forced a caller to merge stdout and stderr to see both —
	// which corrupted the JSON whenever git progress was also on stderr.
	OutcomePartial Outcome = "partial"
	// OutcomeFailure is carried by error envelopes.
	OutcomeFailure Outcome = "failure"
)

// Next is a suggested follow-up command.
//
// Named next, not breadcrumbs: breadcrumbs mean where you came from. It may
// suggest attaching after an unknown topic, but hydra must never act on it.
//
// Argv is an array, not a command string, so a caller can exec it without parsing a
// shell line — quoting a branch containing a space is the caller's problem the moment
// this is prose. Why says what the invocation is FOR, because an argv with no reason
// attached is a guess the caller has to justify on hydra's behalf.
type Next struct {
	Argv []string `json:"argv"`
	Why  string   `json:"why"`
}

// envelope is the single shape every command emits, on stdout, success or failure.
//
// One envelope, one stream. Errors and data both go to stdout so a caller can parse one
// JSON object without merging stderr. Git fetch progress also uses stderr; mixing streams
// corrupts the envelope. The exit status still carries the code, and `hydra commands`
// still publishes the code→exit table.
type envelope struct {
	Schema   int           `json:"schema"`
	Command  string        `json:"command"`
	Outcome  Outcome       `json:"outcome"`
	Summary  string        `json:"summary,omitempty"`
	Data     any           `json:"data,omitempty"`
	Error    *Error        `json:"error,omitempty"`
	Next     []Next        `json:"next,omitempty"`
	Warnings []*Diagnostic `json:"warnings"`
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
	Warnings []*Diagnostic
	// Err rides a partial: the items that landed are in Data, and this says what did
	// not. Set it and the outcome becomes partial without a second envelope.
	Err *Error
}

// Emit writes the envelope for a success or partial result in the given wire format.
//
// The outcome is CORRECTED here rather than trusted. Every aggregate-reporting bug this
// tool has shipped came from a command deciding its own verdict from "my code path
// finished": four adds reporting success while the manifest held two, doctor exiting 4
// under `outcome: success`, status saying "all clean" with a worktree missing, add exiting
// 1 under success with the failing hook absent from the envelope. Enforcing it at the one
// boundary every command passes through is the fix applied once instead of five times.
//
// The format is a PARAMETER rather than package state: the envelope is the contract, and
// which serialisation carries it is the caller's single decision, made once from the
// resolved --output mode.
func Emit(w io.Writer, cmd string, r Result, m Mode) error {
	if r.Outcome == "" {
		r.Outcome = OutcomeSuccess
	}
	if r.Warnings == nil {
		r.Warnings = []*Diagnostic{}
	}

	// An error on the envelope means at least a partial, whatever the caller said.
	if r.Err != nil && r.Outcome == OutcomeSuccess {
		r.Outcome = OutcomePartial
	}
	// `success` may not co-exist with an error or a warning. Those say the caller did not
	// get what they asked for, and one gating on outcome or exit status would otherwise
	// sail straight past them. A NOTE never degrades: the request was satisfied.
	//
	// This used to be decided by case-insensitively substring-matching English prose in
	// the warning text, so rewording a message silently stopped it degrading anything.
	for _, d := range r.Warnings {
		if r.Outcome == OutcomeSuccess && d.IsFault() {
			r.Outcome = OutcomePartial
			break
		}
	}
	// A partial's error carries its own recovery, and it was being dropped: EmitError lifts
	// an error's next[] onto the envelope, Emit did not, so exactly the guidance a
	// half-completed command needs to hand back never reached the caller. Guidance a caller
	// has to know to ask for is not an affordance.
	if len(r.Next) == 0 && r.Err != nil {
		r.Next = r.Err.Next
	}
	recordVerdict(r.Outcome, r.Err)

	return encode(w, m, envelope{
		Schema:   Schema,
		Command:  cmd,
		Outcome:  r.Outcome,
		Summary:  r.Summary,
		Data:     r.Data,
		Error:    r.Err,
		Next:     r.Next,
		Warnings: r.Warnings,
	})
}

// emittedOutcome and emittedCode remember the verdict that actually reached stdout.
//
// The outcome is corrected in Emit, but the process EXIT comes from whatever the command
// returned to main — so a command could emit a corrected `partial` envelope and then return
// nil, exiting 0. `sync` did exactly that twice in one release: once on its normal path and
// again on its "nothing to pull" early return, which skipped the outcome logic entirely.
// Recording the emitted verdict lets main derive the exit from what the caller was actually
// told, so returning early can no longer bypass it.
var (
	emittedOutcome = OutcomeSuccess
	emittedCode    string
)

func recordVerdict(outcome Outcome, err *Error) {
	emittedOutcome = outcome
	emittedCode = ""
	switch {
	case err != nil:
		emittedCode = err.Code
	case outcome == OutcomePartial:
		emittedCode = CodePartialFailure
	case outcome == OutcomeFailure:
		emittedCode = CodeGitFailed
	}
}

// EmittedVerdict reports the outcome and error code that reached stdout, so the exit status
// can be derived from the envelope rather than trusted from a return value.
func EmittedVerdict() (Outcome, string) { return emittedOutcome, emittedCode }

// ResetVerdict clears the recorded verdict. Tests need it because the state is process-wide.
func ResetVerdict() { emittedOutcome, emittedCode = OutcomeSuccess, "" }

// AdoptTextFailure records a failure that cmd already rendered on stderr in text mode.
// main derives the exit code from EmittedVerdict without printing a second "Error:" line.
func AdoptTextFailure(e *Error) { recordVerdict(OutcomeFailure, e) }

// EmitError writes the envelope for a total failure. Callers write it to STDOUT: a
// failure envelope is as machine-readable as a success one, and putting it on stderr
// made the two impossible to read with one idiom.
func EmitError(w io.Writer, cmd string, e *Error, m Mode) error {
	var next []Next
	if e != nil {
		next = e.Next
	}
	return encode(w, m, envelope{
		Schema:   Schema,
		Command:  cmd,
		Outcome:  OutcomeFailure,
		Error:    e,
		Next:     next,
		Warnings: []*Diagnostic{},
	})
}

func encode(w io.Writer, m Mode, e envelope) error {
	if m == ModeYAML {
		return encodeYAML(w, e)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(e); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	return nil
}

// encodeYAML renders the envelope through its JSON form.
//
// It marshals to JSON first and re-decodes into a generic tree rather than handing the
// structs to the YAML encoder, because the envelope types carry ONLY json tags: yaml.v3
// would lowercase the Go field names instead, silently publishing `blockedby` where the
// contract says `blocked_by` — two dialects of one envelope, which is worse than having no
// YAML at all.
//
// UseNumber keeps integers integral. Decoding into `any` normally yields float64, and a
// large count then renders as 1.2172371e+07; json.Number left alone renders as a quoted
// string. Neither is the number the JSON envelope reports.
func encodeYAML(w io.Writer, e envelope) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}

	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(numbersToScalars(tree)); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}
	return enc.Close()
}

// numbersToScalars replaces every json.Number with the narrowest Go scalar that holds it,
// so YAML shows 3 and 0.5 rather than "3" and "0.5".
func numbersToScalars(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, inner := range t {
			t[k] = numbersToScalars(inner)
		}
		return t
	case []any:
		for i, inner := range t {
			t[i] = numbersToScalars(inner)
		}
		return t
	case json.Number:
		if i, err := strconv.ParseInt(t.String(), 10, 64); err == nil {
			return i
		}
		if f, err := strconv.ParseFloat(t.String(), 64); err == nil {
			return f
		}
		// Unrepresentable as either: keep the literal rather than losing it.
		return t.String()
	}
	return v
}

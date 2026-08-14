package output

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// Stable error codes. Nothing outside this enum may be invented ad hoc:
// internal/skill/skill_test.go asserts the shipped agent skill documents exactly
// this set.
const (
	CodeNotInProject             = "not_in_project"
	CodeConfigVersionUnsupported = "config_version_unsupported"
	// CodeConfigInvalid is a manifest that parses and declares a value hydra refuses to act on.
	// It is separate from not_in_project (there IS a manifest) and from
	// config_version_unsupported (the version is fine): a human must edit the file, and no flag
	// or retry changes that.
	CodeConfigInvalid        = "config_invalid"
	CodeProjectUnknown       = "project_unknown"
	CodeRepoUnknown          = "repo_unknown"
	CodeBareMissing          = "bare_missing"
	CodeBranchUnknown        = "branch_unknown"
	CodeWorktreeExists       = "worktree_exists"
	CodeWorktreeUnknown      = "worktree_unknown"
	CodeWorktreeNameConflict = "worktree_name_conflict"
	CodeWorktreeDirty        = "worktree_dirty"
	CodeHookFailed           = "hook_failed"
	CodeShellHelperMissing   = "shell_helper_missing"
	CodePartialFailure       = "partial_failure"
	CodeGitFailed            = "git_failed"
	// Topic membership, the store behind it, and input the caller must supply.
	CodeTopicUnknown  = "topic_unknown"
	CodeTopicConflict = "topic_conflict"
	// CodeTopicNotCloseable: children are open, or their work has not reached this topic's
	// branch yet. `details.blocked_by` names every reason at once, so a caller fixes them in one
	// pass rather than one refusal at a time. Not retryable — something has to change first.
	CodeTopicNotCloseable       = "topic_not_closeable"
	CodeStateVersionUnsupported = "state_version_unsupported"
	CodeBranchProviderFailed    = "branch_provider_failed"
	// CodeBusy is the ONLY retryable code: a git lock or the topic state lock was
	// held. Callers may retry with backoff; every other code is terminal.
	CodeBusy = "busy"
	// CodeNeedsInput means a prompt would have been required but output is
	// machine-readable, so hydra names the missing flag instead of blocking.
	CodeNeedsInput = "needs_input"
	// CodeUnknownCommand is a mistyped or invented subcommand. It is NOT internal:
	// nothing broke, the caller named something that does not exist, and the recovery
	// is a published list of what does.
	// CodeProjectExists is a name already in the registry. Distinct from project_unknown,
	// which is the opposite problem: that one means the name is absent. Reporting a taken
	// name as "unknown" told the caller to check its spelling when the name was correct and
	// the collision was the point.
	CodeProjectExists  = "project_exists"
	CodeUnknownCommand = "unknown_command"
	// CodeManifestUntrusted means the workspace's manifest can execute something and nobody
	// has approved it, or its executable content changed since approval. Exit 2: a human must
	// look at a diff — no flag and no retry substitutes for that, which is the whole point.
	CodeManifestUntrusted = "manifest_untrusted"
	// CodeUsage is a malformed or contradictory invocation: a bad flag value, missing
	// positional arguments, or flags that exclude each other. It is NOT internal -
	// nothing broke - and it is not needs_input, which means "a prompt would have
	// asked for this". Fix the command line and rerun; no state changed.
	CodeUsage    = "usage"
	CodeInternal = "internal"
)

// exitCodes is the single authority mapping error codes to process exit codes.
var exitCodes = map[string]int{
	CodeNotInProject:             2,
	CodeConfigVersionUnsupported: 2,
	CodeConfigInvalid:            2,
	CodeProjectUnknown:           2,
	CodeUsage:                    2,
	CodeManifestUntrusted:        2,
	CodeRepoUnknown:              1,
	CodeBareMissing:              1,
	CodeBranchUnknown:            1,
	CodeWorktreeExists:           1,
	CodeWorktreeUnknown:          1,
	CodeWorktreeNameConflict:     1,
	CodeWorktreeDirty:            5,
	CodeHookFailed:               1,
	CodeShellHelperMissing:       3,
	CodePartialFailure:           4,
	CodeGitFailed:                1,
	CodeTopicUnknown:             1,
	CodeTopicConflict:            1,
	CodeTopicNotCloseable:        1,
	CodeStateVersionUnsupported:  2,
	CodeBranchProviderFailed:     1,
	CodeBusy:                     6,
	CodeNeedsInput:               7,
	CodeProjectExists:            1,
	CodeUnknownCommand:           1,
	CodeInternal:                 1,
}

// ExitCodes returns a copy of the code -> exit-code map.
func ExitCodes() map[string]int {
	out := make(map[string]int, len(exitCodes))
	for code, exit := range exitCodes {
		out[code] = exit
	}
	return out
}

// Codes returns every error code in stable order.
func Codes() []string {
	codes := make([]string, 0, len(exitCodes))
	for code := range exitCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// ExitFor returns the exit code bound to an error code.
func ExitFor(code string) int {
	if exit, ok := exitCodes[code]; ok {
		return exit
	}
	return exitCodes[CodeInternal]
}

// retryableCodes is the closed set of codes a caller may retry.
//
// Exactly one entry today, and that is the point: "retry me" needs one name, not a
// family. busy covers git lock contention and state-lock timeout alike.
var retryableCodes = map[string]bool{
	CodeBusy: true,
}

// Retryable reports whether a caller may retry an operation that failed with code.
func Retryable(code string) bool { return retryableCodes[code] }

// RetryableCodes returns every retryable code in stable order.
func RetryableCodes() []string {
	codes := make([]string, 0, len(retryableCodes))
	for code := range retryableCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	return codes
}

// Diagnostic is the ONE shape for anything that went wrong, wherever it appears: the
// envelope's fatal error, one entry in warnings[], or one failed item inside a
// partial's data.
//
// It exists because those were three different shapes. `error` was this struct,
// `warnings` was []string with no code to branch on, and a per-item failure was a bare
// `error string` field inside five different command payloads — the same JSON key as
// the envelope's error, at a different type, which a generic consumer cannot walk.
// Three shapes became one; nothing was added to get here.
type Diagnostic struct {
	// Severity is "error" or "warning". Position already implies it in the envelope,
	// but carrying it is what lets a caller flatten every diagnostic from every
	// position into one list and loop once.
	Severity string `json:"severity"`

	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`

	// Subject is what this diagnostic is ABOUT, addressed the way a caller would
	// address it: "worktree:backend/api-stage", "repo:api", "topic:2072958", or for
	// the manifest ".hydra/config.yaml:24". One field answers "which thing", and when
	// the thing is a file that is the file:line every compiler prints. hydra's errors
	// are mostly about worktrees and locks, so a subject is the right anchor and a
	// line number is the special case rather than the shape.
	Subject string `json:"subject,omitempty"`

	// Cause is the underlying tool's own words, verbatim and unparsed — git's stderr,
	// a branch provider's output. Separate from Message because hydra's explanation
	// and git's explanation are two different facts; folding them together is how
	// "git fetch origin failed: exit status 128" ever reached a caller.
	Cause string `json:"cause,omitempty"`

	// Retryable is serialised because it is the one fact a caller cannot derive:
	// the code->exit map is published, but "is it worth trying again" is not
	// inferable from either the code string or the exit status.
	Retryable bool `json:"retryable"`

	// Exit is deliberately NOT serialised. The process exit status already carries
	// it and the code->exit map is published, so a field here would duplicate the
	// exit status in a second place that could disagree with it.
	Exit int `json:"-"`

	// Next carries the invocation that recovers from this diagnostic, as argv rather
	// than prose. It rides the diagnostic rather than the envelope so that in a
	// partial, the recovery for item three is attached to item three.
	Next []Next `json:"-"`

	wrapped error
}

// Error is the fatal diagnostic. It is an ALIAS, not a second type: a warning that
// turns out to be fatal, or a per-item failure promoted to the envelope, needs no
// conversion and cannot drift from the shape it is promoted into.
type Error = Diagnostic

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.wrapped }

// Severity values.
//
// Three, not two, because hydra's warnings[] currently holds three different kinds of
// thing and that is precisely why telling them apart needs a list of English substrings
// were three different kinds of thing mixed together. Making the distinction a field
// makes it exact and deletes the prose matching that used to tell them apart.
//
//   - error:   fatal. The command did not do what was asked.
//   - warning: something is wrong and the command continued. DEGRADES a success to
//     partial, because "success" beside a broken workspace is the same lie in a
//     quieter register.
//   - note:    hydra did something worth saying. Never degrades an outcome.
//
// A code names the CONDITION and is required for an error or a warning. A note omits it
// only when there is no condition to name, just an action hydra took.
const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityNote    = "note"
)

// Errorf builds an Error with the exit code and retry flag bound to code.
//
// Both are derived rather than accepted as arguments, so no call site can create an
// error whose retryability disagrees with its code.
func Errorf(code, format string, args ...any) *Error {
	return &Error{
		Severity:  SeverityError,
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Retryable: Retryable(code),
		Exit:      ExitFor(code),
	}
}

// Warnf builds a WARNING diagnostic: the same shape as an error, so a caller loops
// once over one type, and carrying a code so a warning can be branched on. A
// warnings[] of bare strings could only be logged.
func Warnf(code, format string, args ...any) *Diagnostic {
	return &Diagnostic{
		Severity:  SeverityWarning,
		Code:      code,
		Message:   fmt.Sprintf(format, args...),
		Retryable: Retryable(code),
		// No Exit: a warning does not decide the process status. Promoting one to
		// fatal goes through Errorf, which binds the exit from the same code.
	}
}

// Notef builds a NOTE: the caller got what they asked for, and hydra has something to
// say about how. The code MAY be empty, for a note that reports an action rather than a
// condition ("removed empty group directory"). It is set when there IS a condition worth
// branching on: an `optional: true` hook that failed carries CodeHookFailed as a note,
// because the manifest declared that failure acceptable — so the request was satisfied,
// and severity answers "did the caller get what they asked for" rather than "was anything
// imperfect". The code is what still lets an agent find it.
func Notef(code, format string, args ...any) *Diagnostic {
	return &Diagnostic{
		Severity: SeverityNote,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	}
}

// IsFault reports whether a diagnostic must prevent a success verdict. Exact, where the
// predicate it replaces case-insensitively substring-matched English prose and so
// silently stopped working whenever a warning was reworded.
func (e *Diagnostic) String() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Diagnostic) IsFault() bool {
	return e != nil && (e.Severity == SeverityError || e.Severity == SeverityWarning)
}

// Wrap builds an Error preserving an underlying cause.
//
// The cause is folded into the message for humans AND kept verbatim in Cause, because
// a caller that wants git's own words should not have to split a string hydra built.
func Wrap(code string, cause error, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	verbatim := ""
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, cause)
		verbatim = cause.Error()
	}
	return &Error{
		Severity:  SeverityError,
		Code:      code,
		Message:   msg,
		Cause:     verbatim,
		Retryable: Retryable(code),
		Exit:      ExitFor(code),
		wrapped:   cause,
	}
}

// WithSubject names what the diagnostic is about, as "<kind>:<name>" — or as
// "<path>:<line>" when the subject is the manifest. Callers pass the pair rather than
// a pre-joined string so the separator cannot drift per command.
func (e *Diagnostic) WithSubject(kind, name string) *Diagnostic {
	if kind == "" || name == "" {
		return e
	}
	e.Subject = kind + ":" + name
	return e
}

// WithCause records the underlying tool's own words, unparsed.
func (e *Diagnostic) WithCause(cause string) *Diagnostic {
	e.Cause = cause
	return e
}

// WithDetail attaches a structured detail to the error and returns it.
//
// An empty or nil slice is dropped rather than serialized as null: consumers
// branch on a detail being present, and `"did_you_mean": null` is a value they
// would otherwise have to special-case.
func (e *Error) WithDetail(key string, value any) *Error {
	if isEmptyCollection(value) {
		return e
	}
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// WithNext attaches a recovering invocation and returns the error.
func (e *Error) WithNext(n ...Next) *Error {
	e.Next = append(e.Next, n...)
	return e
}

func isEmptyCollection(value any) bool {
	if value == nil {
		return true
	}
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array:
		return rv.Len() == 0
	}
	return false
}

// Classify converts an arbitrary error into an *Error, defaulting to `internal`.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var target *Error
	if errors.As(err, &target) {
		// Repair a hand-built Error: both fields are derived from the code, so a
		// struct literal that set neither still serialises correctly.
		if target.Exit == 0 {
			target.Exit = ExitFor(target.Code)
		}
		target.Retryable = Retryable(target.Code)
		return target
	}
	return &Error{
		Code:      CodeInternal,
		Message:   err.Error(),
		Retryable: Retryable(CodeInternal),
		Exit:      ExitFor(CodeInternal),
		wrapped:   err,
	}
}

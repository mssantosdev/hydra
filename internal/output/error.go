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
	CodeProjectUnknown           = "project_unknown"
	CodeRepoUnknown              = "repo_unknown"
	CodeBareMissing              = "bare_missing"
	CodeBranchUnknown            = "branch_unknown"
	CodeWorktreeExists           = "worktree_exists"
	CodeWorktreeUnknown          = "worktree_unknown"
	CodeWorktreeNameConflict     = "worktree_name_conflict"
	CodeWorktreeDirty            = "worktree_dirty"
	CodeHookFailed               = "hook_failed"
	CodeShellHelperMissing       = "shell_helper_missing"
	CodePartialFailure           = "partial_failure"
	CodeGitFailed                = "git_failed"
	// Topic membership, the store behind it, and input the caller must supply.
	CodeTopicUnknown            = "topic_unknown"
	CodeTopicConflict           = "topic_conflict"
	CodeStateVersionUnsupported = "state_version_unsupported"
	CodeBranchProviderFailed    = "branch_provider_failed"
	// CodeBusy is the ONLY retryable code: a git lock or the topic state lock was
	// held. Callers may retry with backoff; every other code is terminal.
	CodeBusy = "busy"
	// CodeNeedsInput means a prompt would have been required but output is
	// machine-readable, so hydra names the missing flag instead of blocking.
	CodeNeedsInput = "needs_input"
	CodeInternal   = "internal"
)

// exitCodes is the single authority mapping error codes to process exit codes.
var exitCodes = map[string]int{
	CodeNotInProject:             2,
	CodeConfigVersionUnsupported: 2,
	CodeProjectUnknown:           2,
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
	CodeStateVersionUnsupported:  2,
	CodeBranchProviderFailed:     1,
	CodeBusy:                     6,
	CodeNeedsInput:               7,
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

// Error is a user-facing hydra failure carrying a stable code and exit status.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	Exit    int            `json:"-"`

	wrapped error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.wrapped }

// Errorf builds an Error with the exit code bound to code.
func Errorf(code, format string, args ...any) *Error {
	return &Error{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Exit:    ExitFor(code),
	}
}

// Wrap builds an Error preserving an underlying cause.
func Wrap(code string, cause error, format string, args ...any) *Error {
	msg := fmt.Sprintf(format, args...)
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, cause)
	}
	return &Error{
		Code:    code,
		Message: msg,
		Exit:    ExitFor(code),
		wrapped: cause,
	}
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
		if target.Exit == 0 {
			target.Exit = ExitFor(target.Code)
		}
		return target
	}
	return &Error{Code: CodeInternal, Message: err.Error(), Exit: ExitFor(CodeInternal), wrapped: err}
}

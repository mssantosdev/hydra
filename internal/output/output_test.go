package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestResolveModes(t *testing.T) {
	tests := []struct {
		flag string
		env  string
		want Mode
	}{
		{flag: "", want: ModeAuto},
		{flag: "auto", want: ModeAuto},
		{flag: "text", want: ModeText},
		{flag: "plain", want: ModeText},
		{flag: "JSON", want: ModeJSON},
		{flag: "", env: "json", want: ModeJSON},
		{flag: "", env: "text", want: ModeText},
		// An explicit flag beats the environment.
		{flag: "text", env: "json", want: ModeText},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("flag=%q env=%q", tt.flag, tt.env), func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("HYDRA_OUTPUT", tt.env)
			} else {
				t.Setenv("HYDRA_OUTPUT", "")
			}
			got, err := Resolve(tt.flag)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", tt.flag, err)
			}
			if got != tt.want {
				t.Errorf("Resolve(%q) with env %q = %v, want %v", tt.flag, tt.env, got, tt.want)
			}
		})
	}

	if _, err := Resolve("yaml"); err == nil {
		t.Error("Resolve must reject an unknown mode")
	}
}

// auto means JSON when stdout is not a terminal: that is the agent-first default,
// so anything capturing stdout gets JSON with no flag discovery.
func TestEffectiveCollapsesAutoOffTTY(t *testing.T) {
	pipe, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = pipe.Close() }()

	if got := Effective(ModeAuto, pipe); got != ModeJSON {
		t.Errorf("Effective(auto, pipe) = %v, want json", got)
	}
	if got := Effective(ModeText, pipe); got != ModeText {
		t.Errorf("an explicit mode must survive collapsing, got %v", got)
	}
	if got := Effective(ModeJSON, pipe); got != ModeJSON {
		t.Errorf("Effective(json, pipe) = %v, want json", got)
	}
}

func TestColorOffForPipesAndNoColor(t *testing.T) {
	pipe, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = pipe.Close() }()

	_ = os.Unsetenv("NO_COLOR")
	if Color(pipe) {
		t.Error("Color must be false when stdout is not a terminal")
	}

	// NO_COLOR disables color for ANY value, including empty.
	t.Setenv("NO_COLOR", "")
	if Color(os.Stdout) {
		t.Error("Color must be false when NO_COLOR is set, even to an empty value")
	}
}

func TestEmitJSONEnvelope(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]any{"total": 2}

	if err := EmitJSON(&buf, "list", Result{Summary: "2 worktree(s)", Data: data}); err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}

	var envelope struct {
		Schema   int            `json:"schema"`
		Command  string         `json:"command"`
		Outcome  string         `json:"outcome"`
		Summary  string         `json:"summary"`
		Data     map[string]any `json:"data"`
		Next     []Next         `json:"next"`
		Warnings []string       `json:"warnings"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("envelope is not valid JSON: %v\n%s", err, buf.String())
	}
	if envelope.Schema != Schema {
		t.Errorf("schema = %d, want %d", envelope.Schema, Schema)
	}
	if envelope.Command != "list" {
		t.Errorf("command = %q, want list", envelope.Command)
	}
	// outcome defaults to success so no caller has to special-case its absence.
	if envelope.Outcome != string(OutcomeSuccess) {
		t.Errorf("outcome = %q, want %q", envelope.Outcome, OutcomeSuccess)
	}
	if envelope.Summary != "2 worktree(s)" {
		t.Errorf("summary = %q, want the caller's line", envelope.Summary)
	}
	// warnings must always be present as an array so consumers can index it.
	if envelope.Warnings == nil {
		t.Error("warnings must serialize as [] rather than null")
	}
	// next is omitted rather than null when there is nothing to suggest.
	if envelope.Next != nil {
		t.Errorf("next = %v, want omitted", envelope.Next)
	}
}

// A partial outcome travels on the SUCCESS envelope: the data is real and must not
// be discarded merely because the process will also exit non-zero.
func TestEmitJSONCarriesPartialOutcomeAndNext(t *testing.T) {
	var buf bytes.Buffer
	err := EmitJSON(&buf, "sync", Result{
		Outcome: OutcomePartial,
		Summary: "1 pulled, 1 failed",
		Data:    map[string]any{"total": 2},
		Next:    []Next{{Action: "status", Cmd: "hydra status"}},
	})
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}

	var envelope struct {
		Outcome string `json:"outcome"`
		Next    []Next `json:"next"`
	}
	if jsonErr := json.Unmarshal(buf.Bytes(), &envelope); jsonErr != nil {
		t.Fatalf("envelope is not valid JSON: %v\n%s", jsonErr, buf.String())
	}
	if envelope.Outcome != string(OutcomePartial) {
		t.Errorf("outcome = %q, want %q", envelope.Outcome, OutcomePartial)
	}
	if len(envelope.Next) != 1 || envelope.Next[0].Cmd != "hydra status" {
		t.Errorf("next = %v, want the suggested command", envelope.Next)
	}
}

func TestEmitErrorEnvelope(t *testing.T) {
	var buf bytes.Buffer
	e := Errorf(CodeWorktreeNameConflict, "taken by %q", "stage").WithDetail("path", "/ws/backend/api")

	if err := EmitError(&buf, "add", e); err != nil {
		t.Fatalf("EmitError: %v", err)
	}

	var envelope struct {
		Schema  int    `json:"schema"`
		Command string `json:"command"`
		Outcome string `json:"outcome"`
		Error   struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			Details   map[string]any `json:"details"`
			Retryable *bool          `json:"retryable"`
			Exit      *int           `json:"exit"`
		} `json:"error"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("error envelope is not valid JSON: %v\n%s", err, buf.String())
	}
	if envelope.Error.Code != CodeWorktreeNameConflict {
		t.Errorf("code = %q, want %q", envelope.Error.Code, CodeWorktreeNameConflict)
	}
	if envelope.Outcome != string(OutcomeFailure) {
		t.Errorf("outcome = %q, want %q", envelope.Outcome, OutcomeFailure)
	}
	if envelope.Error.Details["path"] != "/ws/backend/api" {
		t.Errorf("details = %v, want the path detail", envelope.Error.Details)
	}
	// retryable must always be present: its absence is not the same as false, and a
	// caller decides whether to retry from this field alone.
	if envelope.Error.Retryable == nil {
		t.Fatal("retryable must be serialized even when false")
	}
	if *envelope.Error.Retryable {
		t.Error("worktree_name_conflict is terminal, not retryable")
	}
	// Exit is transport-internal: the process status carries it, not the payload.
	if envelope.Error.Exit != nil {
		t.Error("exit must not be serialized into the envelope")
	}
}

// busy is the only retryable code, and the flag is derived from the code so a call
// site cannot set one without the other.
func TestRetryableIsDerivedFromCode(t *testing.T) {
	if got := RetryableCodes(); len(got) != 1 || got[0] != CodeBusy {
		t.Fatalf("RetryableCodes() = %v, want exactly [busy]", got)
	}

	if !Errorf(CodeBusy, "locked").Retryable {
		t.Error("busy must be retryable")
	}
	for _, code := range Codes() {
		if code == CodeBusy {
			continue
		}
		if Errorf(code, "x").Retryable {
			t.Errorf("%s must be terminal", code)
		}
	}

	// A hand-built Error that set neither field is repaired by Classify.
	classified := Classify(&Error{Code: CodeBusy, Message: "locked"})
	if !classified.Retryable || classified.Exit != ExitFor(CodeBusy) {
		t.Errorf("Classify must derive both fields, got retryable=%v exit=%d",
			classified.Retryable, classified.Exit)
	}
}

// The exit code is bound to the error code, and main.go is the only place that
// reads it. Commands never choose an exit status directly.
func TestExitCodesAreBoundToErrorCodes(t *testing.T) {
	want := map[string]int{
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
		// busy is the only retryable code, so a script or agent can tell
		// "another hydra is mid-write, retry me" from a real failure.
		CodeBusy: 6,
		// needs_input replaces blocking on a prompt when output is machine-readable.
		CodeNeedsInput: 7,
		CodeInternal:   1,
	}

	got := ExitCodes()
	if len(got) != len(want) {
		t.Fatalf("ExitCodes() has %d entries, want %d: %v", len(got), len(want), got)
	}
	for code, exit := range want {
		if got[code] != exit {
			t.Errorf("ExitFor(%q) = %d, want %d", code, got[code], exit)
		}
	}

	// Mutating the returned map must not affect the enum.
	got[CodeInternal] = 99
	if ExitFor(CodeInternal) != 1 {
		t.Error("ExitCodes() must return a copy")
	}
	if ExitFor("not_a_real_code") != 1 {
		t.Error("an unknown code must fall back to exit 1")
	}
	if len(Codes()) != len(want) {
		t.Errorf("Codes() = %v, want %d entries", Codes(), len(want))
	}
}

func TestClassifyPreservesCodeAndWrapsPlainErrors(t *testing.T) {
	original := Errorf(CodeWorktreeDirty, "dirty")
	if got := Classify(fmt.Errorf("wrapped: %w", original)); got.Code != CodeWorktreeDirty || got.Exit != 5 {
		t.Errorf("Classify lost the code through wrapping: %+v", got)
	}

	plain := Classify(errors.New("boom"))
	if plain.Code != CodeInternal || plain.Exit != 1 {
		t.Errorf("Classify(plain) = %+v, want internal/1", plain)
	}
	if plain.Message != "boom" {
		t.Errorf("Classify must preserve the message, got %q", plain.Message)
	}
	if Classify(nil) != nil {
		t.Error("Classify(nil) must be nil")
	}
}

func TestWrapKeepsCause(t *testing.T) {
	cause := errors.New("git exploded")
	e := Wrap(CodeGitFailed, cause, "failed to fetch %q", "api")

	if !errors.Is(e, cause) {
		t.Error("Wrap must keep the cause unwrappable")
	}
	if e.Code != CodeGitFailed || e.Exit != 1 {
		t.Errorf("Wrap produced %+v, want git_failed/1", e)
	}
}

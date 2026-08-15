package output

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// YAML is a second serialisation of ONE envelope, not a second contract. The field names
// must be the json-tag names: yaml.v3 handed the structs directly would lowercase the Go
// field names and publish `blockedby` where JSON says `blocked_by`, which is worse than
// having no YAML at all.
func TestYAMLEnvelopeMatchesTheJSONContract(t *testing.T) {
	result := Result{
		Summary: "1 topic",
		Data: map[string]any{
			"topic": "epic-login",
			// A colon in a value is the classic way to emit YAML that will not parse back.
			"summary_line": "epic: login",
			"blocked_by": []map[string]any{
				{"topic": "feat", "reason": "dependency_open"},
			},
			// Integers must survive as integers. Decoding JSON into `any` yields float64,
			// which renders this as 1.2172371e+07.
			"size":  12172371,
			"ratio": 0.5,
		},
		Next:     []Next{{Argv: []string{"hydra", "topic", "close", "epic-login", "--force"}, Why: "close anyway"}},
		Warnings: []*Diagnostic{Warnf(CodeHookFailed, "post_add failed")},
	}

	var asJSON, asYAML bytes.Buffer
	if err := Emit(&asJSON, "topic close", result, ModeJSON); err != nil {
		t.Fatalf("Emit json: %v", err)
	}
	if err := Emit(&asYAML, "topic close", result, ModeYAML); err != nil {
		t.Fatalf("Emit yaml: %v", err)
	}

	var fromJSON, fromYAML map[string]any
	if err := json.Unmarshal(asJSON.Bytes(), &fromJSON); err != nil {
		t.Fatalf("json envelope did not parse: %v", err)
	}
	if err := yaml.Unmarshal(asYAML.Bytes(), &fromYAML); err != nil {
		t.Fatalf("yaml envelope did not parse: %v\n%s", err, asYAML.String())
	}

	// Same keys at the top level, and the same keys inside data.
	for _, key := range []string{"schema", "command", "outcome", "summary", "data", "next", "warnings"} {
		if _, ok := fromYAML[key]; !ok {
			t.Errorf("yaml envelope is missing %q\n%s", key, asYAML.String())
		}
	}
	dataJSON, _ := fromJSON["data"].(map[string]any)
	dataYAML, _ := fromYAML["data"].(map[string]any)
	if len(dataJSON) != len(dataYAML) {
		t.Fatalf("data keys differ:\njson %v\nyaml %v", dataJSON, dataYAML)
	}
	for key := range dataJSON {
		if _, ok := dataYAML[key]; !ok {
			t.Errorf("yaml data is missing %q — the two dialects have forked", key)
		}
	}
	// The snake_case tag survived rather than being lowercased into one word.
	if !strings.Contains(asYAML.String(), "blocked_by:") {
		t.Errorf("yaml lost the json tag name:\n%s", asYAML.String())
	}

	// The value with a colon round-trips.
	if dataYAML["summary_line"] != "epic: login" {
		t.Errorf("summary_line = %#v, want \"epic: login\"", dataYAML["summary_line"])
	}
	// The integer is still an integer, and equal to what JSON reported.
	if got, ok := dataYAML["size"].(int); !ok || got != 12172371 {
		t.Errorf("size = %#v, want int 12172371\n%s", dataYAML["size"], asYAML.String())
	}
	if got, ok := dataYAML["ratio"].(float64); !ok || got != 0.5 {
		t.Errorf("ratio = %#v, want float64 0.5", dataYAML["ratio"])
	}
	if fromYAML["schema"] != Schema {
		t.Errorf("schema = %#v, want %d", fromYAML["schema"], Schema)
	}
	// Warnings is a present (possibly empty) list in both, because consumers iterate it
	// without a nil check.
	if _, ok := fromYAML["warnings"].([]any); !ok {
		t.Errorf("warnings = %#v, want a list", fromYAML["warnings"])
	}
}

// A failing invocation must answer in the format the caller asked for. A YAML success
// envelope followed by a JSON error envelope in the same script is a broken contract, and
// that is exactly the shape a half-applied format change takes.
func TestYAMLErrorEnvelope(t *testing.T) {
	e := Errorf(CodeTopicCycle, "a part_of c would close a cycle: a → c → b → a").
		WithDetail("path", []string{"a", "c", "b", "a"}).
		WithNext(Next{Argv: []string{"hydra", "topic", "link", "a", "part_of", "c", "--force"}, Why: "record it anyway"})

	var buf bytes.Buffer
	if err := EmitError(&buf, "topic link", e, ModeYAML); err != nil {
		t.Fatalf("EmitError yaml: %v", err)
	}

	var got map[string]any
	if err := yaml.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("yaml error envelope did not parse: %v\n%s", err, buf.String())
	}
	if got["outcome"] != string(OutcomeFailure) {
		t.Errorf("outcome = %#v, want failure", got["outcome"])
	}
	errObj, ok := got["error"].(map[string]any)
	if !ok {
		t.Fatalf("error is %#v, want a mapping\n%s", got["error"], buf.String())
	}
	if errObj["code"] != CodeTopicCycle {
		t.Errorf("code = %#v, want %s", errObj["code"], CodeTopicCycle)
	}
	// The override rides the failure, so the caller reads how to proceed from the same
	// document that refused them.
	next, ok := got["next"].([]any)
	if !ok || len(next) != 1 {
		t.Fatalf("next = %#v, want one suggestion", got["next"])
	}
	if !strings.Contains(buf.String(), "--force") {
		t.Errorf("the recovery argv did not survive:\n%s", buf.String())
	}
}

// Machine() is what every "should I print prose" branch asks. If it forgot YAML, those
// branches would print a table into a document a script is parsing.
func TestMachineCoversEveryEnvelopeMode(t *testing.T) {
	for _, m := range []Mode{ModeJSON, ModeYAML} {
		if !m.Machine() {
			t.Errorf("%v.Machine() = false, want true", m)
		}
	}
	for _, m := range []Mode{ModeText, ModeAuto} {
		if m.Machine() {
			t.Errorf("%v.Machine() = true, want false", m)
		}
	}
	if ModeYAML.String() != "yaml" {
		t.Errorf("ModeYAML.String() = %q", ModeYAML.String())
	}
}

// auto must keep resolving to JSON for a pipe. YAML is opt-in precisely because every
// existing consumer parses JSON.
func TestAutoNeverResolvesToYAML(t *testing.T) {
	pipe, _, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer func() { _ = pipe.Close() }()
	if got := Effective(ModeAuto, pipe); got != ModeJSON {
		t.Errorf("Effective(auto, pipe) = %v, want json", got)
	}
	if got := Effective(ModeYAML, pipe); got != ModeYAML {
		t.Errorf("Effective(yaml, pipe) = %v, want yaml", got)
	}
}

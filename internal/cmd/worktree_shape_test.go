package cmd

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Every command that creates or claims a worktree must report the same four facts about
// it. This exists because they diverged in practice: `apply` reported a created worktree
// without saying WHERE, and `start` reported a path but not the `name` that every other
// command accepts as a handle. A caller then has to special-case per command, or derive a
// handle from a path basename — which is exactly the guessing the envelope is meant to
// remove.
//
// A command is free to add fields (`disposition`, `attached`, `ahead`) — this is a floor,
// not a ceiling.
func TestWorktreeReportingShapeIsShared(t *testing.T) {
	required := []string{"group", "repo", "branch", "name", "path"}

	shapes := map[string]any{
		"list/status/repo add (worktreeJSON)": worktreeJSON{},
		"start (startTargetJSON)":             startTargetJSON{},
		"apply (applyResultJSON)":             applyResultJSON{},
	}

	for name, shape := range shapes {
		t.Run(name, func(t *testing.T) {
			got := jsonFieldSet(t, shape)
			for _, field := range required {
				if !got[field] {
					t.Errorf("%s is missing %q; a caller cannot address what it just created",
						name, field)
				}
			}
		})
	}
}

// jsonFieldSet reports the JSON field names a struct serialises, so the assertion is
// about the wire contract rather than Go field names.
func jsonFieldSet(t *testing.T, v any) map[string]bool {
	t.Helper()

	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal %T: %v", v, err)
	}

	// A zero value omits `omitempty` fields, so read the tags for those rather than
	// concluding the field does not exist.
	set := map[string]bool{}
	for key := range decoded {
		set[key] = true
	}
	rt := reflect.TypeOf(v)
	for i := range rt.NumField() {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		for j := range len(tag) {
			if tag[j] == ',' {
				name = tag[:j]
				break
			}
		}
		if name != "" {
			set[name] = true
		}
	}
	return set
}

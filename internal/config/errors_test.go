package config

import (
	"errors"
	"strings"
	"testing"
)

// An error message that omits the thing it is about cannot be acted on. Each of these types
// exists to name a specific subject, so that is the contract: the message says WHICH.
func TestManifestErrorsNameTheirSubject(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want []string
	}{
		{
			name: "unsupported version names both what it found and what it needs",
			err:  &ErrUnsupportedVersion{Version: "9"},
			want: []string{"9", SchemaVersion},
		},
		{
			name: "an empty version still says something a reader can act on",
			err:  &ErrUnsupportedVersion{},
			want: []string{SchemaVersion},
		},
		{
			name: "a busy manifest names the file being written",
			err:  &ErrManifestBusy{Path: "/ws/.hydra/config.yaml.lock"},
			want: []string{"/ws/.hydra/config.yaml.lock"},
		},
		{
			name: "a malformed manifest names its path",
			err:  &ErrMalformed{Path: "/ws/.hydra/config.yaml", Err: errors.New("yaml: line 4: bad")},
			want: []string{"/ws/.hydra/config.yaml", "line 4"},
		},
		{
			name: "a refused value names the field and the value",
			err:  &ErrConfigInvalid{Field: "groups.backend.path", Msg: "leaves the workspace"},
			want: []string{"groups.backend.path", "leaves the workspace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("Error() = %q, want it to name %q", got, want)
				}
			}
		})
	}
}

// Unwrap is what lets a caller ask errors.As/Is about the underlying parser failure instead of
// string-matching a message hydra built.
func TestMalformedUnwrapsToTheParserError(t *testing.T) {
	parse := errors.New("yaml: line 7: mapping values are not allowed")
	err := &ErrMalformed{Path: "/ws/.hydra/config.yaml", Err: parse}

	if !errors.Is(err, parse) {
		t.Error("ErrMalformed does not unwrap to the parser error")
	}
	var malformed *ErrMalformed
	if !errors.As(error(err), &malformed) {
		t.Error("errors.As cannot recover an ErrMalformed")
	}
	if got := malformed.Line(); got != 7 {
		t.Errorf("Line() = %d, want 7", got)
	}
}

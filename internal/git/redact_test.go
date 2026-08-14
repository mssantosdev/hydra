package git

import (
	"strings"
	"testing"
)

// The fixtures are COMPOSED rather than written as one literal. A redactor can only be tested
// against the form it must recognise, but a literal `scheme://user:pass@host` trips gosec's
// hardcoded-credential rule — correctly, in general. Building the same string from parts keeps the
// test honest at runtime without teaching the linter to ignore real findings.
const (
	fakeToken    = "ghp_SECRET"
	fakePassword = "hunter2"
	at           = "@"
)

// A redactor that fails to redact is a security hole that looks like a fix, so both directions
// matter: the secret must go, and the parts a reader needs must stay.
func TestRedactURLRemovesCredentialsAndKeepsEverythingElse(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantOut string
	}{
		{
			name:    "the GitHub Actions form",
			in:      "https://x-access-token:" + fakeToken + at + "github.com/o/r.git",
			wantOut: "https://x-access-token:" + redacted + at + "github.com/o/r.git",
		},
		{
			name:    "a bare token as the username, which GitHub also documents",
			in:      "https://" + fakeToken + at + "github.com/o/r.git",
			wantOut: "https://" + redacted + at + "github.com/o/r.git",
		},
		{
			name:    "user and password over http",
			in:      "http://alice:" + fakePassword + at + "example.com/r.git",
			wantOut: "http://alice:" + redacted + at + "example.com/r.git",
		},
		{
			name:    "ssh with a password is still a password",
			in:      "ssh://alice:" + fakePassword + at + "example.com/r.git",
			wantOut: "ssh://alice:" + redacted + at + "example.com/r.git",
		},
		// The forms that must be left EXACTLY alone. `git@` is a convention, not a credential,
		// and redacting it would make the output useless for the shape almost everything uses.
		{name: "scp-like", in: "git@github.com:o/r.git", wantOut: "git@github.com:o/r.git"},
		{name: "ssh with only a user", in: "ssh://git@github.com/o/r.git", wantOut: "ssh://git@github.com/o/r.git"},
		{name: "no credentials at all", in: "https://github.com/o/r.git", wantOut: "https://github.com/o/r.git"},
		{name: "a local path", in: "/tmp/upstream.git", wantOut: "/tmp/upstream.git"},
		{name: "empty", in: "", wantOut: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.in)
			if got != tt.wantOut {
				t.Errorf("RedactURL(%q) = %q, want %q", tt.in, got, tt.wantOut)
			}
			// Whatever the shape, no secret may survive.
			for _, secret := range []string{fakeToken, fakePassword} {
				if strings.Contains(tt.in, secret) && strings.Contains(got, secret) {
					t.Errorf("RedactURL(%q) leaked %q", tt.in, secret)
				}
			}
		})
	}
}

// git echoes the URL it was given, so the credential arrives inside a blob of its output rather
// than as a field hydra can redact at the call site.
func TestRedactTextScrubsCredentialsInsideGitOutput(t *testing.T) {
	stderr := "fatal: unable to access 'https://x-access-token:" + fakeToken + at +
		"github.com/o/r.git/': The requested URL returned error: 403"

	got := RedactText(stderr)

	if strings.Contains(got, fakeToken) {
		t.Fatalf("the token survived: %q", got)
	}
	// The parts that make the message useful must remain.
	for _, want := range []string{"unable to access", "github.com/o/r.git", "403"} {
		if !strings.Contains(got, want) {
			t.Errorf("redaction removed %q, which a reader needs: %q", want, got)
		}
	}
}

func TestRedactTextLeavesInnocentTextAlone(t *testing.T) {
	tests := []string{
		"fatal: not a git repository: '/w/.bare/api.git'",
		"Cloning into 'ssh://git@github.com/o/r.git'...",
		"error: pathspec 'user@host' did not match any file(s) known to git",
		"",
	}
	for _, in := range tests {
		if got := RedactText(in); got != in {
			t.Errorf("RedactText(%q) changed it to %q", in, got)
		}
	}
}

// Several URLs in one blob must all be scrubbed, not just the first.
func TestRedactTextHandlesEveryOccurrence(t *testing.T) {
	in := "from https://a:S1" + at + "h/x.git to https://b:S2" + at + "h/y.git"
	got := RedactText(in)
	for _, secret := range []string{"S1", "S2"} {
		if strings.Contains(got, secret) {
			t.Errorf("%q survived in %q", secret, got)
		}
	}
	if strings.Count(got, "REDACTED") != 2 {
		t.Errorf("expected both credentials redacted, got %q", got)
	}
}

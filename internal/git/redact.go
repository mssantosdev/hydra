package git

import (
	"net/url"
	"regexp"
	"strings"
)

// redacted replaces credentials in a URL hydra is about to print.
const redacted = "REDACTED"

// RedactURL removes credentials from a remote URL so it can be echoed.
//
// A remote is manifest content, and CI writes tokens straight into it:
// `https://x-access-token:ghp_…@github.com/o/r.git` is what GitHub Actions injects and Azure
// DevOps does the equivalent. `hydra repo list` publishing remotes is the POINT of the command,
// so the fix is not to stop printing them — it is to stop printing the secret part.
//
// The scp-like form is left alone. In `git@github.com:o/r.git` the user is `git`, which is not a
// credential but a convention, and redacting it would make the output useless for the shape
// almost every repository actually uses.
func RedactURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || !strings.Contains(trimmed, "@") {
		return raw
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.User == nil {
		// No scheme means the scp-like form (`git@host:path`), which url.Parse does not
		// understand and which carries no secret. A parse failure is left as-is rather than
		// guessed at: mangling an unrecognised remote would be worse than printing it.
		return raw
	}

	if _, hasPassword := parsed.User.Password(); hasPassword {
		// user:secret@ — the password is the credential whatever the scheme.
		parsed.User = url.UserPassword(parsed.User.Username(), redacted)
		return parsed.String()
	}

	// Over http(s) a bare userinfo IS the token: `https://ghp_…@github.com/o/r.git` is a
	// documented GitHub form. Over ssh a bare user is just a user, so it stays.
	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		parsed.User = url.User(redacted)
		return parsed.String()
	}
	return raw
}

// credentialURL matches a URL carrying userinfo inside arbitrary text.
//
// It exists because git echoes the URL it was given: `git ls-remote` failing on a tokenised
// remote puts that token in its own stderr, which hydra then captures and reports. Redacting the
// remote at every call site would still miss this, because the credential arrives from git rather
// than from the manifest field.
var credentialURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*)://([^/@\s]+)@`)

// RedactText removes credentials from URLs embedded anywhere in a blob of text, such as a
// captured git stderr. Non-secret userinfo is preserved: `ssh://git@host` keeps its user, because
// there `git` is a convention and not a token.
func RedactText(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	return credentialURL.ReplaceAllStringFunc(s, func(match string) string {
		parts := credentialURL.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		scheme, userinfo := parts[1], parts[2]
		lower := strings.ToLower(scheme)
		if strings.Contains(userinfo, ":") {
			user := userinfo[:strings.Index(userinfo, ":")]
			return scheme + "://" + user + ":" + redacted + "@"
		}
		if lower == "http" || lower == "https" {
			return scheme + "://" + redacted + "@"
		}
		return match
	})
}

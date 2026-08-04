package branchresolve

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The whole precedence chain in one table, because the ORDER is the contract and a
// per-level test would not catch two levels swapping.
func TestResolve_Precedence(t *testing.T) {
	base := Request{
		Topic:          "2072958",
		Slug:           "login",
		Kind:           "feat",
		User:           "marcus",
		Repo:           "api",
		Group:          "backend",
		Pattern:        "{user}/{kind}-{slug}",
		MemberBranches: []string{"marcus/feat-existing", "marcus/feat-existing"},
	}

	tests := []struct {
		name       string
		mutate     func(*Request)
		wantBranch string
		wantSource Source
	}{
		{
			name:       "positional beats everything",
			mutate:     func(r *Request) { r.Positional = "hotfix/urgent"; r.Flag = "from-flag" },
			wantBranch: "hotfix/urgent",
			wantSource: SourcePositional,
		},
		{
			name:       "flag beats members and pattern",
			mutate:     func(r *Request) { r.Flag = "marcus/from-flag" },
			wantBranch: "marcus/from-flag",
			wantSource: SourceFlag,
		},
		{
			name:       "unanimous members beat the pattern",
			mutate:     func(r *Request) {},
			wantBranch: "marcus/feat-existing",
			wantSource: SourceUnanimous,
		},
		{
			name:       "pattern applies when there are no members",
			mutate:     func(r *Request) { r.MemberBranches = nil },
			wantBranch: "marcus/feat-login",
			wantSource: SourcePattern,
		},
		{
			name:       "a single member is still unanimous",
			mutate:     func(r *Request) { r.MemberBranches = []string{"solo/branch"} },
			wantBranch: "solo/branch",
			wantSource: SourceUnanimous,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			tc.mutate(&req)
			got, err := Resolve(context.Background(), req)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Branch != tc.wantBranch {
				t.Errorf("branch = %q, want %q", got.Branch, tc.wantBranch)
			}
			if got.Source != tc.wantSource {
				t.Errorf("source = %q, want %q", got.Source, tc.wantSource)
			}
		})
	}
}

// The documented surprising case: a positional argument is a LITERAL branch name
// even when it looks like a topic id, and the pattern never runs. Generating from a
// pattern here would silently ignore what the user typed.
func TestResolve_PositionalIsLiteralNotATopicID(t *testing.T) {
	got, err := Resolve(context.Background(), Request{
		Positional: "2072958",
		Pattern:    "{user}/{kind}-{slug}",
		User:       "marcus",
		Kind:       "feat",
		Slug:       "login",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Branch != "2072958" {
		t.Errorf("branch = %q, want the literal positional value", got.Branch)
	}
	if !got.OffPattern {
		t.Error("a literal name that does not match the pattern must be flagged off-pattern")
	}
}

// There is no fallback past the pattern. An earlier design fell back to the topic id
// verbatim, which would create a branch literally named "2072958" — a name no team
// would choose, produced by hydra guessing.
func TestResolve_NoFallbackBeyondPattern(t *testing.T) {
	_, err := Resolve(context.Background(), Request{Topic: "2072958"})

	var needs *NeedsInputError
	if !errors.As(err, &needs) {
		t.Fatalf("err = %v, want NeedsInputError", err)
	}
	if len(needs.Missing) != 1 || needs.Missing[0] != "--branch" {
		t.Errorf("missing = %v, want [--branch]", needs.Missing)
	}
}

// Members on different branches is the one case where hydra must NOT choose: there
// is no branch to extend, and picking one would fracture the topic.
func TestResolve_DisagreeingMembersAskForBranch(t *testing.T) {
	_, err := Resolve(context.Background(), Request{
		MemberBranches: []string{"marcus/feat-login", "hotfix/login-npe"},
		// A pattern is configured and must still not be used: generating a third
		// name would be worse than asking.
		Pattern: "{user}/{slug}",
		User:    "marcus",
		Slug:    "login",
	})

	var needs *NeedsInputError
	if !errors.As(err, &needs) {
		t.Fatalf("err = %v, want NeedsInputError", err)
	}
	if len(needs.Missing) != 1 || needs.Missing[0] != "--branch" {
		t.Errorf("missing = %v, want [--branch]", needs.Missing)
	}
}

// An unanimous member branch is never validated against the pattern: it already
// exists in the topic, so refusing it would make an existing topic unextendable.
func TestResolve_UnanimousBranchIsNotPatternChecked(t *testing.T) {
	got, err := Resolve(context.Background(), Request{
		MemberBranches: []string{"legacy-name", "legacy-name"},
		Pattern:        "{user}/{slug}",
		Strict:         true,
		User:           "marcus",
		Slug:           "login",
	})
	if err != nil {
		t.Fatalf("extending a topic must not be blocked by strict mode: %v", err)
	}
	if got.Branch != "legacy-name" {
		t.Errorf("branch = %q, want legacy-name", got.Branch)
	}
}

// A pattern needing a value nobody supplied names the exact flag.
func TestApply_MissingPlaceholderNamesItsFlag(t *testing.T) {
	tests := []struct {
		pattern string
		req     Request
		want    string
	}{
		{pattern: "{slug}", req: Request{}, want: "--slug"},
		{pattern: "{kind}/x", req: Request{}, want: "--kind"},
		{pattern: "{topic}", req: Request{}, want: "--topic"},
		{pattern: "{user}/x", req: Request{}, want: "--user"},
	}

	for _, tc := range tests {
		t.Run(tc.pattern, func(t *testing.T) {
			_, err := Apply(tc.pattern, tc.req)
			var needs *NeedsInputError
			if !errors.As(err, &needs) {
				t.Fatalf("err = %v, want NeedsInputError", err)
			}
			if len(needs.Missing) != 1 || needs.Missing[0] != tc.want {
				t.Errorf("missing = %v, want [%s]", needs.Missing, tc.want)
			}
		})
	}
}

// The placeholder set is CLOSED. An unknown one is a configuration error, not
// literal text left in the branch name — silently producing "feat/{ticket}" would
// create a branch with braces in it.
func TestApply_UnknownPlaceholderIsRefused(t *testing.T) {
	_, err := Apply("{ticket}/{slug}", Request{Slug: "login"})

	var invalid *InvalidBranchError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidBranchError", err)
	}
	if !strings.Contains(invalid.Reason, "ticket") {
		t.Errorf("reason must name the unknown placeholder, got %q", invalid.Reason)
	}
}

func TestApply_SubstitutesEveryPlaceholder(t *testing.T) {
	got, err := Apply("{group}/{repo}/{user}/{kind}-{topic}-{slug}", Request{
		Group: "backend", Repo: "api", User: "marcus",
		Kind: "feat", Topic: "2072958", Slug: "login",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := "backend/api/marcus/feat-2072958-login"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Free-form values are slugified so a human-typed title cannot produce an unusable
// ref. repo and group are already safe and must pass through untouched.
func TestSlugSegment(t *testing.T) {
	tests := []struct{ in, want string }{
		{in: "Login Page", want: "login-page"},
		{in: "  padded  ", want: "padded"},
		{in: "snake_case", want: "snake-case"},
		{in: "Ação Rápida", want: "acao-rapida"},
		{in: "a...b", want: "a...b"},
		{in: "multiple   spaces", want: "multiple-spaces"},
		{in: "trailing-", want: "trailing"},
		{in: "-leading", want: "leading"},
		{in: "wild~^:?*[chars]", want: "wildchars"},
		{in: "keep/slash", want: "keep/slash"},
		{in: "UPPER", want: "upper"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := slugSegment(tc.in); got != tc.want {
				t.Errorf("slugSegment(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Every name git refuses must be refused BEFORE a mutation is attempted, so the
// failure is a clean error rather than a half-created worktree.
func TestValidateRef(t *testing.T) {
	valid := []string{"main", "feat/login", "marcus/feat-login", "v1.2.3", "a.b.c"}
	for _, branch := range valid {
		if err := ValidateRef(branch); err != nil {
			t.Errorf("ValidateRef(%q) = %v, want nil", branch, err)
		}
	}

	invalid := []string{
		"", " ", "has space", "-leading-dash", "trailing.",
		"tilde~", "caret^", "colon:", "question?", "star*",
		"bracket[", "back\\slash", "at@{brace", "double..dot",
		"double//slash", "/leading-slash", "trailing/", "ends.lock", "@",
	}
	for _, branch := range invalid {
		if err := ValidateRef(branch); err == nil {
			t.Errorf("ValidateRef(%q) = nil, want an error", branch)
		}
	}
}

// Strict mode is opt-in and turns an off-pattern name into a refusal. By default the
// same name is accepted: branch shape belongs to the team, not to hydra.
func TestResolve_StrictRefusesOffPatternExplicitBranch(t *testing.T) {
	req := Request{
		Positional: "whatever-i-like",
		Pattern:    "{user}/{slug}",
		User:       "marcus",
		Slug:       "login",
	}

	lenient, err := Resolve(context.Background(), req)
	if err != nil {
		t.Fatalf("off-pattern names are accepted by default: %v", err)
	}
	if !lenient.OffPattern {
		t.Error("the resolution must still report that it was off-pattern")
	}

	req.Strict = true
	if _, err := Resolve(context.Background(), req); err == nil {
		t.Fatal("strict mode must refuse an off-pattern branch")
	}
}

// An explicit name that DOES match the pattern's shape is not flagged.
func TestResolve_OnPatternExplicitBranchIsNotFlagged(t *testing.T) {
	got, err := Resolve(context.Background(), Request{
		Positional: "marcus/anything",
		Pattern:    "{user}/{slug}",
		Strict:     true,
		User:       "marcus",
		Slug:       "login",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.OffPattern {
		t.Error("a name matching the pattern shape must not be flagged off-pattern")
	}
}

// writeProvider creates an executable script and returns its path.
func writeProvider(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("provider tests need a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "provider.sh")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatalf("write provider: %v", err)
	}
	return path
}

// The provider's first stdout line is the branch, and it outranks the pattern.
func TestResolve_ProviderSuppliesTheBranch(t *testing.T) {
	provider := writeProvider(t, `echo "from/provider"`)

	got, err := Resolve(context.Background(), Request{
		Provider: provider,
		Pattern:  "{user}/{slug}",
		User:     "marcus",
		Slug:     "login",
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Branch != "from/provider" {
		t.Errorf("branch = %q, want from/provider", got.Branch)
	}
	if got.Source != SourceProvider {
		t.Errorf("source = %q, want %q", got.Source, SourceProvider)
	}
}

// The provider receives the documented JSON on stdin.
func TestResolve_ProviderReceivesJSONOnStdin(t *testing.T) {
	provider := writeProvider(t, `
payload=$(cat)
echo "$payload" | grep -q '"repo":"api"' || { echo "missing repo" >&2; exit 1; }
echo "$payload" | grep -q '"topic":"2072958"' || { echo "missing topic" >&2; exit 1; }
echo "$payload" | grep -q '"schema":1' || { echo "missing schema" >&2; exit 1; }
echo ok/branch
`)

	got, err := Resolve(context.Background(), Request{
		Provider: provider, Repo: "api", Topic: "2072958", Group: "backend",
	})
	if err != nil {
		t.Fatalf("the provider did not see the documented payload: %v", err)
	}
	if got.Branch != "ok/branch" {
		t.Errorf("branch = %q, want ok/branch", got.Branch)
	}
}

// A failing provider fails the operation. No generated fallback: a wrong branch name
// is worse than a refusal, and this happens before any git mutation.
func TestResolve_ProviderFailureIsNotPapered(t *testing.T) {
	provider := writeProvider(t, `echo "boom" >&2; exit 3`)

	_, err := Resolve(context.Background(), Request{
		Provider: provider, Repo: "api",
		Pattern: "{user}/{slug}", User: "marcus", Slug: "login",
	})

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if provErr.ExitCode != 3 {
		t.Errorf("exit_code = %d, want 3", provErr.ExitCode)
	}
	if !strings.Contains(provErr.Stderr, "boom") {
		t.Errorf("stderr = %q, want the provider's diagnostics", provErr.Stderr)
	}
}

// A provider that exits 0 without printing is a failure, not an empty branch name.
func TestResolve_ProviderSilentSuccessIsAFailure(t *testing.T) {
	provider := writeProvider(t, `exit 0`)

	_, err := Resolve(context.Background(), Request{Provider: provider, Repo: "api"})
	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
}

// A hanging provider is bounded, and the timeout is reported as such so a caller can
// tell "broken" from "slow".
func TestResolve_ProviderTimeout(t *testing.T) {
	provider := writeProvider(t, `sleep 5; echo too/late`)

	start := time.Now()
	_, err := Resolve(context.Background(), Request{
		Provider: provider, Repo: "api", Timeout: 100 * time.Millisecond,
	})
	elapsed := time.Since(start)

	var provErr *ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("err = %v, want ProviderError", err)
	}
	if !provErr.TimedOut {
		t.Error("the error must report that it timed out, not just that it failed")
	}
	if elapsed > 2*time.Second {
		t.Errorf("took %v; the timeout was not enforced", elapsed)
	}
}

// A provider returning a name git would refuse must be caught here, not by git after
// a partial mutation.
func TestResolve_ProviderInvalidBranchIsRefused(t *testing.T) {
	provider := writeProvider(t, `echo "bad~name"`)

	_, err := Resolve(context.Background(), Request{Provider: provider, Repo: "api"})
	var invalid *InvalidBranchError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want InvalidBranchError", err)
	}
}

// An explicit branch short-circuits the provider entirely, so the common path never
// pays for a subprocess.
func TestResolve_ExplicitBranchSkipsTheProvider(t *testing.T) {
	provider := writeProvider(t, `echo "should/not/run"; exit 1`)

	got, err := Resolve(context.Background(), Request{
		Positional: "explicit/branch",
		Provider:   provider,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Branch != "explicit/branch" {
		t.Errorf("branch = %q, want explicit/branch", got.Branch)
	}
}

// NeedsInputError must render the one_of case differently: "pass --repos" is wrong
// when any of three flags would do.
func TestNeedsInputError_OneOfMessage(t *testing.T) {
	err := &NeedsInputError{
		OneOf:  []string{"--repos", "--group", "--all"},
		Reason: "no repositories selected",
	}
	if !strings.Contains(err.Error(), "one of") {
		t.Errorf("message = %q, want it to say one of", err.Error())
	}
}

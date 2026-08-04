// Package branchresolve decides what a new branch is called.
//
// It exists so the decision lives in one testable place rather than inside the
// command that creates worktrees, and so the boundary between substitution and
// arbitrary logic is enforced by the type system: a Pattern is one literal string
// with a closed placeholder set, and anything more expressive must be an external
// Provider. That line is deliberate — conditionals, per-kind pattern maps,
// counters, date arithmetic and embedded shell are exactly how a template DSL
// grows, and this package makes them unrepresentable rather than discouraged.
package branchresolve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Source names where a resolved branch came from, so a caller can explain itself
// and a test can assert the precedence chain rather than only its output.
type Source string

const (
	// SourcePositional is an explicit branch argument. It beats everything,
	// including --branch, and it means the pattern never runs.
	SourcePositional Source = "positional"
	// SourceFlag is --branch.
	SourceFlag Source = "branch_flag"
	// SourceUnanimous is the branch every existing member of the topic shares. It
	// deliberately outranks the pattern: generating a fresh name when extending a
	// topic would fracture the one thing the topic exists to hold together.
	SourceUnanimous Source = "topic_members"
	// SourceProvider is an external command's stdout.
	SourceProvider Source = "branch_provider"
	// SourcePattern is placeholder substitution.
	SourcePattern Source = "branch_pattern"
)

// Request is everything the chain may consult. Nothing else is consulted, which is
// what makes the outcome predictable.
type Request struct {
	Positional string
	Flag       string

	// MemberBranches are the branches of the topic's existing members. Unanimity is
	// computed here rather than trusted from the caller.
	MemberBranches []string

	Pattern  string
	Provider string
	Strict   bool

	// Placeholder values. Each has exactly one source and hydra never infers one:
	// Kind is free-form and never guessed from a title, Slug is never derived from
	// one either.
	Topic string
	Kind  string
	Slug  string
	User  string
	Repo  string
	Group string

	Project     string
	ProjectRoot string

	// Timeout bounds the provider. Zero uses DefaultProviderTimeout.
	Timeout time.Duration
}

// Resolution is the outcome of the chain.
type Resolution struct {
	Branch string
	Source Source
	// OffPattern is true when an explicit branch does not match the configured
	// pattern's shape. Advisory by default; an error under Strict.
	OffPattern bool
}

// NeedsInputError reports that a value must come from the user, naming the exact
// flags that would satisfy it.
//
// OneOf exists because some requirements are satisfiable several ways — a single
// "missing" list cannot express "any of --repos, --group or --all". A caller that
// cannot tell which flag to supply has not been given an actionable error.
type NeedsInputError struct {
	Missing []string
	OneOf   []string
	Reason  string
}

func (e *NeedsInputError) Error() string {
	if len(e.OneOf) > 0 {
		return fmt.Sprintf("%s; pass one of %s", e.Reason, strings.Join(e.OneOf, ", "))
	}
	return fmt.Sprintf("%s; pass %s", e.Reason, strings.Join(e.Missing, ", "))
}

// ProviderError reports that the configured provider failed. The branch is never
// invented as a fallback: a wrong branch name is worse than a refusal.
type ProviderError struct {
	Provider string
	Repo     string
	ExitCode int
	TimedOut bool
	Stderr   string
}

func (e *ProviderError) Error() string {
	if e.TimedOut {
		return fmt.Sprintf("branch provider %q timed out for %s", e.Provider, e.Repo)
	}
	return fmt.Sprintf("branch provider %q failed for %s (exit %d)", e.Provider, e.Repo, e.ExitCode)
}

// InvalidBranchError reports a name git will not accept, or an off-pattern name
// under strict mode.
type InvalidBranchError struct {
	Branch string
	Reason string
}

func (e *InvalidBranchError) Error() string {
	return fmt.Sprintf("branch %q is invalid: %s", e.Branch, e.Reason)
}

const (
	// DefaultProviderTimeout bounds a provider that does not answer.
	DefaultProviderTimeout = 5 * time.Second
	// MaxProviderTimeout caps what a caller may ask for. A provider is consulted
	// before any git mutation, so a long hang is a hang with nothing done.
	MaxProviderTimeout = 30 * time.Second
	// providerSchema versions the stdin contract independently of the envelope.
	providerSchema = 1
)

// placeholders is the CLOSED set. A pattern naming anything else is a
// configuration error, reported when the pattern runs rather than silently left as
// literal text.
var placeholders = map[string]func(Request) string{
	"topic": func(r Request) string { return r.Topic },
	"kind":  func(r Request) string { return r.Kind },
	"slug":  func(r Request) string { return r.Slug },
	"user":  func(r Request) string { return r.User },
	"repo":  func(r Request) string { return r.Repo },
	"group": func(r Request) string { return r.Group },
}

// placeholderFlag maps a placeholder to the flag that supplies it, so an absent
// value names something the caller can actually pass.
var placeholderFlag = map[string]string{
	"topic": "--topic",
	"kind":  "--kind",
	"slug":  "--slug",
	"user":  "--user",
}

var placeholderPattern = regexp.MustCompile(`\{([a-z]+)\}`)

// Resolve applies the precedence chain:
//
//  1. positional branch
//  2. --branch
//  3. the unanimous branch of the topic's existing members
//  4. the branch provider
//  5. the branch pattern
//
// There is no step 6. With none of these available the caller is asked for
// --branch, because guessing a branch name is worse than asking for one: an earlier
// design fell back to the topic id verbatim, which would create a branch literally
// named "2072958".
func Resolve(ctx context.Context, r Request) (Resolution, error) {
	if explicit := firstNonEmpty(r.Positional, r.Flag); explicit != "" {
		source := SourceFlag
		if strings.TrimSpace(r.Positional) != "" {
			source = SourcePositional
		}
		return validateExplicit(strings.TrimSpace(explicit), source, r)
	}

	if branch, ok := unanimous(r.MemberBranches); ok {
		// Not validated against the pattern: the branch already exists in this topic,
		// so refusing it now would make an existing topic unextendable.
		return Resolution{Branch: branch, Source: SourceUnanimous}, nil
	}
	if len(r.MemberBranches) > 1 {
		return Resolution{}, &NeedsInputError{
			Missing: []string{"--branch"},
			Reason:  "the topic's members are on different branches, so there is no branch to extend",
		}
	}

	if r.Provider != "" {
		branch, err := runProvider(ctx, r)
		if err != nil {
			return Resolution{}, err
		}
		if err := ValidateRef(branch); err != nil {
			return Resolution{}, err
		}
		return Resolution{Branch: branch, Source: SourceProvider}, nil
	}

	if r.Pattern != "" {
		branch, err := Apply(r.Pattern, r)
		if err != nil {
			return Resolution{}, err
		}
		if err := ValidateRef(branch); err != nil {
			return Resolution{}, err
		}
		return Resolution{Branch: branch, Source: SourcePattern}, nil
	}

	return Resolution{}, &NeedsInputError{
		Missing: []string{"--branch"},
		Reason:  "no branch given, no topic members to extend, and no branch_pattern configured",
	}
}

// validateExplicit checks a user-supplied name and reports whether it matches the
// configured pattern's shape.
func validateExplicit(branch string, source Source, r Request) (Resolution, error) {
	if err := ValidateRef(branch); err != nil {
		return Resolution{}, err
	}

	resolution := Resolution{Branch: branch, Source: source}
	if r.Pattern == "" {
		return resolution, nil
	}

	// Shape comparison only: an explicit name is compared against the pattern with
	// placeholders treated as wildcards, because the point is "does this look like
	// our convention", not "is it exactly what we would have generated".
	if !matchesShape(branch, r.Pattern) {
		resolution.OffPattern = true
		if r.Strict {
			return Resolution{}, &InvalidBranchError{
				Branch: branch,
				Reason: fmt.Sprintf("does not match branch_pattern %q and branch_pattern_strict is on", r.Pattern),
			}
		}
	}
	return resolution, nil
}

// Apply substitutes the closed placeholder set into a pattern.
func Apply(pattern string, r Request) (string, error) {
	var missing []string
	var unknown []string

	out := placeholderPattern.ReplaceAllStringFunc(pattern, func(match string) string {
		name := match[1 : len(match)-1]
		get, ok := placeholders[name]
		if !ok {
			unknown = append(unknown, name)
			return match
		}
		value := strings.TrimSpace(get(r))
		if value == "" {
			if flag, ok := placeholderFlag[name]; ok {
				missing = append(missing, flag)
			} else {
				missing = append(missing, name)
			}
			return match
		}
		// repo and group are already ref-safe directory-ish names; the free-form
		// values are slugified so a title typed by a human cannot produce an
		// unusable ref.
		if name == "slug" || name == "kind" || name == "user" {
			value = slugSegment(value)
		}
		return value
	})

	if len(unknown) > 0 {
		sort.Strings(unknown)
		return "", &InvalidBranchError{
			Branch: pattern,
			Reason: fmt.Sprintf("branch_pattern uses unknown placeholder(s) %s; the set is {topic} {kind} {slug} {user} {repo} {group}",
				strings.Join(unknown, ", ")),
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		missing = dedupe(missing)
		return "", &NeedsInputError{
			Missing: missing,
			Reason:  fmt.Sprintf("branch_pattern %q needs values that were not supplied", pattern),
		}
	}
	return out, nil
}

// matchesShape reports whether branch could have come from pattern, treating each
// placeholder as a non-empty run of ref-legal characters.
func matchesShape(branch, pattern string) bool {
	var sb strings.Builder
	sb.WriteString("^")
	last := 0
	for _, loc := range placeholderPattern.FindAllStringIndex(pattern, -1) {
		sb.WriteString(regexp.QuoteMeta(pattern[last:loc[0]]))
		sb.WriteString(`[^/]+`)
		last = loc[1]
	}
	sb.WriteString(regexp.QuoteMeta(pattern[last:]))
	sb.WriteString("$")

	re, err := regexp.Compile(sb.String())
	if err != nil {
		// An uncompilable shape must not reject a name the user chose; treat it as
		// "cannot tell" rather than "does not match".
		return true
	}
	return re.MatchString(branch)
}

// slugSegment makes one path segment safe for a git ref.
//
// Separate from the worktree directory slug on purpose: a directory name and a ref
// have different legal characters, and collapsing them once produced names that
// were valid as one and not the other.
func slugSegment(in string) string {
	decomposed, _, err := transform.String(
		transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)), norm.NFC),
		in,
	)
	if err != nil {
		decomposed = in
	}

	var sb strings.Builder
	for _, r := range decomposed {
		switch {
		case r == ' ' || r == '_':
			sb.WriteRune('-')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' || r == '/':
			sb.WriteRune(unicode.ToLower(r))
		default:
			// Dropped rather than replaced: a run of punctuation should not become a
			// run of dashes.
		}
	}

	out := collapseDashes(sb.String())
	return strings.Trim(out, "-.")
}

func collapseDashes(in string) string {
	var sb strings.Builder
	var prevDash bool
	for _, r := range in {
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// refInvalid matches what git refuses outright in a branch name.
var refInvalid = regexp.MustCompile(`[\x00-\x20~^:?*\[\\\x7f]|@\{|\.\.|//|/$|^/|\.lock$|^@$`)

// ValidateRef rejects a name git would refuse, before any mutation is attempted.
func ValidateRef(branch string) error {
	switch {
	case strings.TrimSpace(branch) == "":
		return &InvalidBranchError{Branch: branch, Reason: "empty"}
	case branch != strings.TrimSpace(branch):
		return &InvalidBranchError{Branch: branch, Reason: "has leading or trailing whitespace"}
	case strings.HasPrefix(branch, "-"):
		return &InvalidBranchError{Branch: branch, Reason: "starts with a dash"}
	case strings.HasSuffix(branch, "."):
		return &InvalidBranchError{Branch: branch, Reason: "ends with a dot"}
	case refInvalid.MatchString(branch):
		return &InvalidBranchError{Branch: branch, Reason: "contains characters git does not allow in a ref"}
	}
	return nil
}

// providerRequest is the JSON written to a provider's stdin.
type providerRequest struct {
	Schema         int    `json:"schema"`
	Project        string `json:"project"`
	ProjectRoot    string `json:"project_root"`
	Group          string `json:"group"`
	Repo           string `json:"repo"`
	Topic          string `json:"topic"`
	Slug           string `json:"slug"`
	Kind           string `json:"kind"`
	ExplicitBranch string `json:"explicit_branch"`
	Pattern        string `json:"pattern"`
	Strict         bool   `json:"strict"`
}

// runProvider asks an external command for the branch name.
//
// One line on stdout is the answer; stderr is diagnostics only. A non-zero exit
// fails the whole operation before any git mutation, because a provider that cannot
// answer must not be papered over with a generated name.
func runProvider(ctx context.Context, r Request) (string, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultProviderTimeout
	}
	if timeout > MaxProviderTimeout {
		timeout = MaxProviderTimeout
	}

	payload, err := json.Marshal(providerRequest{
		Schema:      providerSchema,
		Project:     r.Project,
		ProjectRoot: r.ProjectRoot,
		Group:       r.Group,
		Repo:        r.Repo,
		Topic:       r.Topic,
		Slug:        r.Slug,
		Kind:        r.Kind,
		Pattern:     r.Pattern,
		Strict:      r.Strict,
	})
	if err != nil {
		return "", fmt.Errorf("failed to encode the branch provider request: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// No shell: the provider is an argv, so a path with spaces or a value containing
	// a metacharacter cannot become a second command.
	//nolint:gosec // G204: the provider is operator-configured workspace policy, like a hook
	command := exec.CommandContext(runCtx, r.Provider)
	command.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	// WaitDelay is REQUIRED for the timeout to mean anything.
	//
	// CommandContext kills the provider itself, but Run also waits for the stdout and
	// stderr pipes to close — and a grandchild the provider spawned (a `sleep`, a
	// hung network call) inherits those pipes. Without this, a 5s provider ignored a
	// 100ms timeout entirely, which a test caught. WaitDelay force-closes the pipes
	// shortly after the kill so hydra returns promptly.
	//
	// A grandchild may briefly outlive us; that is the operator's process to own, and
	// it is strictly better than hydra hanging.
	command.WaitDelay = 200 * time.Millisecond

	runErr := command.Run()
	if runCtx.Err() != nil {
		return "", &ProviderError{
			Provider: r.Provider, Repo: r.Repo, TimedOut: true,
			Stderr: strings.TrimSpace(stderr.String()),
		}
	}
	if runErr != nil {
		return "", &ProviderError{
			Provider: r.Provider, Repo: r.Repo,
			ExitCode: command.ProcessState.ExitCode(),
			Stderr:   strings.TrimSpace(stderr.String()),
		}
	}

	// First line only. A provider that prints more is using stdout for diagnostics,
	// which is what stderr is for.
	branch := strings.TrimSpace(firstLine(stdout.String()))
	if branch == "" {
		return "", &ProviderError{
			Provider: r.Provider, Repo: r.Repo,
			Stderr: "provider exited 0 but printed no branch name on stdout",
		}
	}
	return branch, nil
}

func firstLine(in string) string {
	if idx := strings.IndexByte(in, '\n'); idx >= 0 {
		return in[:idx]
	}
	return in
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// unanimous reports the single branch every member shares.
func unanimous(branches []string) (string, bool) {
	if len(branches) == 0 {
		return "", false
	}
	first := branches[0]
	for _, branch := range branches[1:] {
		if branch != first {
			return "", false
		}
	}
	return first, first != ""
}

func dedupe(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, value := range in {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

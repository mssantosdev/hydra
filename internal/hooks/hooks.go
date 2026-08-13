// Package hooks runs the declarative per-event shell commands configured in
// .hydra/config.yaml.
package hooks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

// DefaultTimeout bounds a single hook. Every other wait in hydra is bounded — the state lock
// at 5s, the manifest lock at 10s, `run --timeout` — and hooks were the exception: a hook that
// hung hung hydra, with no way to say "give up". That is worst exactly where hooks are most
// useful, on an instance bootstrap whose own deadline is measured in minutes, because the boot
// then hangs on a network hiccup in someone's `npm ci`.
//
// Generous rather than tight: a dependency install on a cold cache genuinely takes minutes, and
// a bound that fires during normal work would be worse than none. Override per hook with
// `timeout:`.
const DefaultTimeout = 10 * time.Minute

// Context is the environment a hook chain runs against.
type Context struct {
	Event        string
	Project      string
	ProjectRoot  string
	Group        string
	Repo         string
	Branch       string
	WorktreePath string
	BarePath     string

	// Worktree is the short handle accepted by `hydra hooks run --worktree`.
	Worktree string

	// Topic is the unit of work this operation belongs to, empty when there is none. Its
	// absence meant a post_add fired by `start --topic X` could not name X, so "do something
	// for this unit of work" was impossible in a hook — a hole in the extension surface that
	// is the product.
	Topic string

	// SourceWorktree is the worktree a new one was derived from, empty when there is none.
	// Without it a hook had to rebuild `<root>/<group>/<repo>` to find the originator, which
	// SKILL.md lists as an anti-pattern in the same breath, because `--as` can override the
	// derived name.
	SourceWorktree string
}

// EnvKeys names every variable a hook is given, in the order Env emits them.
//
// It exists so the contract is discoverable rather than only documented: `hooks ls` publishes
// it, which lets an agent learn what a hook receives without reading the guide, and lets the
// docs gate assert the guide's list against the binary. The published page listed eight
// variables for one commit after two more were added — a self-describing surface is how that
// stops being possible.
func EnvKeys() []string {
	return []string{
		"HYDRA_EVENT",
		"HYDRA_PROJECT",
		"HYDRA_PROJECT_ROOT",
		"HYDRA_GROUP",
		"HYDRA_REPO",
		"HYDRA_BRANCH",
		"HYDRA_WORKTREE_PATH",
		"HYDRA_BARE_PATH",
		"HYDRA_TOPIC",
		"HYDRA_SOURCE_WORKTREE",
	}
}

// Env renders the injected HYDRA_* variables.
func (c Context) Env() []string {
	return []string{
		"HYDRA_EVENT=" + c.Event,
		"HYDRA_PROJECT=" + c.Project,
		"HYDRA_PROJECT_ROOT=" + c.ProjectRoot,
		"HYDRA_GROUP=" + c.Group,
		"HYDRA_REPO=" + c.Repo,
		"HYDRA_BRANCH=" + c.Branch,
		"HYDRA_WORKTREE_PATH=" + c.WorktreePath,
		"HYDRA_BARE_PATH=" + c.BarePath,
		// Always exported, even when empty: a hook testing `-n "$HYDRA_TOPIC"` behaves the
		// same whether the variable is unset or blank, and an unset variable under `set -u`
		// would abort the hook instead.
		"HYDRA_TOPIC=" + c.Topic,
		"HYDRA_SOURCE_WORKTREE=" + c.SourceWorktree,
	}
}

// Result reports what a hook chain did.
type Result struct {
	Ran      int                  `json:"ran"`
	Warnings []*output.Diagnostic `json:"warnings,omitempty"`
}

// Run executes a hook chain in cwd, streaming hook output to w (stderr, so it
// can never corrupt a JSON envelope on stdout).
//
// An optional hook that fails produces a warning and the chain continues. A
// required hook that fails stops the chain and returns hook_failed. A failing
// hook never rolls back work hydra already completed successfully.
func Run(hs []config.ResolvedHook, ctx Context, cwd string, w io.Writer) (Result, error) {
	var result Result
	if len(hs) == 0 {
		return result, nil
	}

	env := append(os.Environ(), ctx.Env()...)
	for _, hook := range hs {
		if hook.Run == "" {
			continue
		}

		timeout, timeoutErr := hookTimeout(hook.Hook)
		if timeoutErr != nil {
			return result, hookConfigError(hook, ctx, timeoutErr,
				fmt.Sprintf("hook at %s has an invalid timeout %q", hook.Path, hook.Timeout))
		}

		hookCtx := context.Background()
		var cancel context.CancelFunc
		if timeout > 0 {
			hookCtx, cancel = context.WithTimeout(hookCtx, timeout)
		}

		//nolint:gosec // G204: running the user's own configured hook through sh -c IS the feature
		cmd := exec.CommandContext(hookCtx, "sh", "-c", hook.Run)
		cmd.Dir = cwd
		cmd.Env = env
		cmd.Stdout = w
		cmd.Stderr = w
		cmd.Stdin = nil
		// Bound the wait AFTER the context kills the shell. Without this, a hook that
		// backgrounds a child holding the output pipe keeps cmd.Run blocked forever, which
		// defeats the timeout it just enforced. Short, because by this point the process has
		// already been signalled and this only covers draining its pipes.
		cmd.WaitDelay = 2 * time.Second

		err := cmd.Run()
		if cancel != nil {
			cancel()
		}
		result.Ran++
		if err == nil {
			continue
		}

		// A timeout is reported as the timeout it is, not as whatever exit status killing the
		// process produced — "signal: killed" tells a reader nothing about the bound they hit.
		if timeout > 0 && errors.Is(hookCtx.Err(), context.DeadlineExceeded) {
			if hook.Optional {
				note := optionalHookNote(hook, ctx, fmt.Errorf("timed out after %s", timeout))
				result.Warnings = append(result.Warnings, note)
				_, _ = fmt.Fprintf(w, "warning: %s\n", note)
				continue
			}
			return result, hookTimedOutError(hook, ctx, timeout)
		}
		if hook.Optional {
			note := optionalHookNote(hook, ctx, err)
			result.Warnings = append(result.Warnings, note)
			_, _ = fmt.Fprintf(w, "warning: %s\n", note)
			continue
		}

		return result, hookFailureError(hook, ctx, err)
	}

	return result, nil
}

func hookConfigError(hook config.ResolvedHook, ctx Context, cause error, message string) *output.Error {
	return withHookName(hook, output.Wrap(output.CodeInternal, cause, "%s", message).
		WithDetail("event", ctx.Event).
		WithDetail("path", hook.Path))
}

func hookTimedOutError(hook config.ResolvedHook, ctx Context, timeout time.Duration) *output.Error {
	err := withHookName(hook, output.Errorf(output.CodeHookFailed, "hook timed out at %s after %s", hook.Path, timeout).
		WithDetail("event", ctx.Event).
		WithDetail("path", hook.Path).
		WithDetail("exit", -1))
	return withHookRetry(err, ctx)
}

func hookFailureError(hook config.ResolvedHook, ctx Context, cause error) *output.Error {
	exit := hookExitCode(cause)
	err := withHookName(hook, output.Errorf(output.CodeHookFailed, "%s", hookFailureMessage(hook, ctx, exit)).
		WithDetail("event", ctx.Event).
		WithDetail("path", hook.Path).
		WithDetail("exit", exit))
	return withHookRetry(err, ctx)
}

// optionalHookNote records an optional hook failure as a note with CodeHookFailed: the
// manifest declared the failure acceptable, so the request was satisfied, but agents still
// need a code to find it.
func optionalHookNote(hook config.ResolvedHook, ctx Context, cause error) *output.Diagnostic {
	exit := hookExitCode(cause)
	msg := "optional " + hookFailureMessage(hook, ctx, exit)
	d := output.Notef(output.CodeHookFailed, "%s", msg)
	if cause != nil {
		d = d.WithCause(cause.Error())
	}
	if hook.Name != "" {
		if d.Details == nil {
			d.Details = map[string]any{}
		}
		d.Details["name"] = hook.Name
	}
	if ctx.Worktree != "" {
		d = d.WithSubject("worktree", ctx.Worktree)
	}
	return d
}

func hookFailureMessage(hook config.ResolvedHook, ctx Context, exit int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "hook failed at %s", hook.Path)
	if hook.Name != "" {
		fmt.Fprintf(&b, "\n  name: %s", hook.Name)
	}
	fmt.Fprintf(&b, "\n  exit: %d", exit)
	if ctx.Worktree != "" {
		fmt.Fprintf(&b, "\n  hint: fix the hook, then run \"hydra hooks run %s --worktree %s\"", ctx.Event, ctx.Worktree)
	} else {
		fmt.Fprintf(&b, "\n  hint: fix the hook, then run \"hydra hooks run %s\"", ctx.Event)
	}
	return b.String()
}

func withHookName(hook config.ResolvedHook, err *output.Error) *output.Error {
	if hook.Name == "" {
		return err
	}
	return err.WithDetail("name", hook.Name)
}

func withHookRetry(err *output.Error, ctx Context) *output.Error {
	next := []string{"hydra", "hooks", "run", ctx.Event}
	if ctx.Worktree != "" {
		next = append(next, "--worktree", ctx.Worktree)
	}
	return err.WithNext(output.Next{
		Argv: next,
		Why:  "re-run the hook chain after fixing it",
	})
}

func hookExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// hookTimeout resolves a hook's bound: its own `timeout:` when set, otherwise DefaultTimeout.
// An explicit "0" means unbounded, which is the old behaviour kept available as a deliberate
// choice rather than as the only one.
func hookTimeout(h config.Hook) (time.Duration, error) {
	if h.Timeout == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(h.Timeout)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("timeout must not be negative")
	}
	return d, nil
}

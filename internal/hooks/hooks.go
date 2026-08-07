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
	Ran      int      `json:"ran"`
	Warnings []string `json:"warnings,omitempty"`
}

// Run executes a hook chain in cwd, streaming hook output to w (stderr, so it
// can never corrupt a JSON envelope on stdout).
//
// An optional hook that fails produces a warning and the chain continues. A
// required hook that fails stops the chain and returns hook_failed. A failing
// hook never rolls back work hydra already completed successfully.
func Run(hs []config.Hook, ctx Context, cwd string, w io.Writer) (Result, error) {
	var result Result
	if len(hs) == 0 {
		return result, nil
	}

	env := append(os.Environ(), ctx.Env()...)
	for i, hook := range hs {
		if hook.Run == "" {
			continue
		}

		timeout, timeoutErr := hookTimeout(hook)
		if timeoutErr != nil {
			return result, output.Wrap(output.CodeInternal, timeoutErr,
				"%s hook %d has an invalid timeout %q", ctx.Event, i+1, hook.Timeout).
				WithDetail("event", ctx.Event).
				WithDetail("hook", hook.Run).
				WithDetail("index", i+1)
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
			err = fmt.Errorf("timed out after %s", timeout)
		}
		if hook.Optional {
			warning := fmt.Sprintf("optional %s hook %d (%s) failed: %v", ctx.Event, i+1, hook.Run, err)
			result.Warnings = append(result.Warnings, warning)
			_, _ = fmt.Fprintf(w, "warning: %s\n", warning)
			continue
		}

		return result, output.Wrap(output.CodeHookFailed, err,
			"%s hook %d (%s) failed; fix it and run \"hydra hooks run %s\"",
			ctx.Event, i+1, hook.Run, ctx.Event).
			WithDetail("event", ctx.Event).
			WithDetail("hook", hook.Run).
			WithDetail("index", i+1)
	}

	return result, nil
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

// Package hooks runs the declarative per-event shell commands configured in
// .hydra.yaml.
package hooks

import (
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/output"
)

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

		cmd := exec.Command("sh", "-c", hook.Run)
		cmd.Dir = cwd
		cmd.Env = env
		cmd.Stdout = w
		cmd.Stderr = w
		cmd.Stdin = nil

		err := cmd.Run()
		result.Ran++
		if err == nil {
			continue
		}

		if hook.Optional {
			warning := fmt.Sprintf("optional %s hook %d (%s) failed: %v", ctx.Event, i+1, hook.Run, err)
			result.Warnings = append(result.Warnings, warning)
			fmt.Fprintf(w, "warning: %s\n", warning)
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

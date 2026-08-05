package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mssantosdev/hydra/internal/fanout"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	runJobs    int
	runTimeout time.Duration
	runKeepGo  bool
)

var runCmd = &cobra.Command{
	Use:   "run [<worktree>] [selector] -- <command> [args…]",
	Short: "Run a command in each selected worktree",
	Long: `Run one command across worktrees.

DESCRIPTION
  Everything after -- is the command, passed as an argv array to exec. There is NO
  shell: hydra never wraps it in "sh -c", so a path containing a space, a branch with
  a metacharacter, or an argument with a quote cannot become a second command. If you
  want shell features, ask for a shell explicitly:

    hydra run --topic 2072958 -- sh -c 'go build ./... && go test ./...'

  Each invocation gets an explicit working directory — the worktree — and the same
  context in the environment that hooks receive:

    HYDRA_TOPIC   the topic, empty when the worktree is unassigned
    HYDRA_REPO    repo alias
    HYDRA_GROUP   group name
    HYDRA_BRANCH  branch, empty when detached
    HYDRA_PATH    the worktree's absolute path

  A bare worktree handle runs in exactly that worktree, so "run" addresses one thing
  the same way every other command does. A handle matching several worktrees is an
  error, never a silent first match.

EXAMPLES
  # one worktree
  $ hydra run api-stage -- go build ./...

  # every worktree in a topic
  $ hydra run --topic 2072958 -- go test ./...

  # a whole group, four at a time, and do not stop at the first failure
  $ hydra run --group backend --jobs 4 --keep-going -- make lint

  # shell features, asked for explicitly
  $ hydra run --repos api -- sh -c 'git log --oneline -1 | cat'

EXIT CODES
  0  every command exited 0
  1  no command was given, or every invocation failed
  2  not_in_project
  4  partial_failure (some invocations failed; details.failed lists them)
  7  needs_input

SEE ALSO
  • hydra hooks run - lifecycle hooks configured in the manifest
  • hydra list      - which worktrees a selector matches`,
	// Args are not validated here: cobra hands us everything after --, and an empty
	// command is reported as a hydra error with a code rather than a usage string.
	RunE: runRun,
}

func init() {
	rootCmd.AddCommand(runCmd)
	registerSelectorFlags(runCmd.Flags())
	runCmd.Flags().IntVarP(&runJobs, "jobs", "j", 0,
		"Max worktrees to run in parallel (0 = one per repository, capped)")
	runCmd.Flags().DurationVar(&runTimeout, "timeout", 0,
		"Per-invocation timeout, for example 30s or 5m (0 = no limit)")
	runCmd.Flags().BoolVar(&runKeepGo, "keep-going", false,
		"Report every failure instead of stopping at the first")
	runCmd.ValidArgsFunction = completeWorktreeNames
}

type runResultJSON struct {
	Group    string `json:"group"`
	Repo     string `json:"repo"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	Topic    string `json:"topic,omitempty"`
	ExitCode int    `json:"exit_code"`
	// Stdout and stderr ARE captured under --output json, because a fan-out runner
	// that reports only exit codes is an exit-code poller: with several worktrees the
	// passthrough output arrives unattributed, and under --jobs it interleaves
	// mid-line, so no caller can tell which worktree said what. Under a TTY the
	// streams still pass through live, where a human is watching them arrive.
	//
	// stdout keeps its HEAD and stderr its TAIL: a compiler prints the useful part
	// first, while an error is the last thing written before a non-zero exit.
	Stdout      string `json:"stdout,omitempty"`
	Stderr      string `json:"stderr,omitempty"`
	StdoutBytes int64  `json:"stdout_bytes"`
	StderrBytes int64  `json:"stderr_bytes"`
	StdoutTrunc bool   `json:"stdout_truncated,omitempty"`
	StderrTrunc bool   `json:"stderr_truncated,omitempty"`
	Failed      bool   `json:"failed"`
	Error       string `json:"error,omitempty"`
	MS          int64  `json:"duration_ms"`
}

type runJSON struct {
	Command  []string        `json:"command"`
	Total    int             `json:"total"`
	Failed   int             `json:"failed"`
	Results  []runResultJSON `json:"results"`
	TimedOut int             `json:"timed_out"`
}

func runRun(cmd *cobra.Command, args []string) error {
	if cfg == nil || projectRoot == "" {
		return output.Errorf(output.CodeNotInProject, "no hydra project loaded")
	}

	handle, command := splitRunArgs(cmd, args)
	if len(command) == 0 {
		return output.Errorf(output.CodeNeedsInput,
			"a command is required: hydra run [selector] -- <command> [args…]").
			WithDetail("missing", []string{"-- <command>"})
	}

	targets, warnings, err := runTargets(handle)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return output.Errorf(output.CodeWorktreeUnknown,
			"the selector matched no worktrees").
			WithDetail("topic", topicFilter).
			WithDetail("repos", reposFilter).
			WithDetail("group", groupFilter)
	}

	// One command per worktree, so the engine's per-repo gate is what bounds
	// concurrency. SerialPerRepo is false: running a build in two worktrees of the
	// same repo touches no shared git state, unlike creating one.
	results := fanout.Run(context.Background(), targets, fanout.Config{
		Jobs:          runJobs,
		SerialPerRepo: false,
		Timeout:       runTimeout,
	}, func(ctx context.Context, t fanout.Target) fanout.ItemResult {
		return runOne(ctx, t, command)
	})

	payload := runJSON{Command: command, Total: len(results)}
	for _, result := range results {
		entry := runResultJSON{
			Group:  result.Target.Group,
			Repo:   result.Target.Repo,
			Branch: result.Target.Branch,
			Path:   result.Target.Path,
			MS:     result.Duration.Milliseconds(),
		}
		if id, ok := runTopics[result.Target.Key()]; ok {
			entry.Topic = id
		}
		if c := takeCapture(result.Target.Key()); c.StdoutBytes > 0 || c.StderrBytes > 0 {
			entry.Stdout, entry.StdoutBytes, entry.StdoutTrunc = c.Stdout, c.StdoutBytes, c.StdoutTrunc
			entry.Stderr, entry.StderrBytes, entry.StderrTrunc = c.Stderr, c.StderrBytes, c.StderrTrunc
		}
		if result.Disposition == fanout.Failed {
			entry.Failed = true
			entry.Error = result.Reason
			entry.ExitCode = exitCodeOf(result.Err)
			payload.Failed++
			if entry.ExitCode == -1 {
				payload.TimedOut++
			}
		}
		payload.Results = append(payload.Results, entry)
	}
	runTopics = nil
	resetCaptures()

	summary := runSummary(payload)

	// The outcome has to know whether ANYTHING landed. Deriving it from `Failed > 0`
	// reported "partial" when every worktree failed, so a caller reading stdout
	// concluded some work succeeded when none had.
	var runErr *output.Error
	outcome := output.OutcomeSuccess
	switch payload.Failed {
	case 0:
	case payload.Total:
		outcome = output.OutcomeFailure
		runErr = output.Errorf(output.CodeGitFailed,
			"the command failed in every worktree").
			WithDetail("command", command).
			WithDetail("failed", failedRunTargets(payload))
	default:
		outcome = output.OutcomePartial
		runErr = output.Errorf(output.CodePartialFailure,
			"the command failed in %d of %d worktrees", payload.Failed, payload.Total).
			WithDetail("command", command).
			WithDetail("failed", failedRunTargets(payload))
	}

	if emitErr := emitResult(cmd, output.Result{
		Outcome:  outcome,
		Summary:  summary,
		Data:     payload,
		Warnings: warnings,
		Err:      runErr,
	}, func() { printRunText(payload, summary) }); emitErr != nil {
		return emitErr
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

// splitRunArgs separates an optional worktree handle from the command after --.
//
// cobra's ArgsLenAtDash is the only reliable way to tell "hydra run api -- ls" from
// "hydra run api ls": both arrive as one args slice, and guessing by position would
// make a command named like a worktree ambiguous.
func splitRunArgs(cmd *cobra.Command, args []string) (handle string, command []string) {
	dash := cmd.ArgsLenAtDash()
	if dash < 0 {
		// No -- at all: everything is a positional, and there is no command.
		if len(args) > 0 {
			return args[0], nil
		}
		return "", nil
	}
	if dash > 0 {
		handle = strings.TrimSpace(args[0])
	}
	return handle, args[dash:]
}

// runTopics carries membership from resolution into the payload.
var runTopics map[string]string

// runTargets resolves what to run in, accepting either a bare handle or a selector.
func runTargets(handle string) ([]fanout.Target, []string, error) {
	if handle != "" {
		// A handle addresses exactly one worktree, and ambiguity is an error — the same
		// rule every other command follows.
		items, _ := collectWorktrees(cfg, projectRoot)
		wt, err := resolveOneWorktree(items, handle)
		if err != nil {
			return nil, nil, err
		}
		runTopics = map[string]string{}
		target := fanout.Target{
			Group: wt.RepoContext.Group, Repo: wt.RepoContext.Alias,
			Branch: wt.Branch, Path: wt.Path, BareRepo: wt.RepoContext.BareRepo,
		}
		if idx, idxErr := newTopicIndex(projectRoot); idxErr == nil {
			if id, ok := idx[topicKey(wt.RepoContext.Alias, wt.Branch)]; ok {
				runTopics[target.Key()] = id
			}
		}
		return []fanout.Target{target}, nil, nil
	}

	session := currentSession()
	selector := currentSelector()
	if err := requireTopicInTargets([]projectTarget{{Cfg: session.Cfg, Root: session.Root}}, selector.Topic); err != nil {
		return nil, nil, err
	}

	// tracking=false: running a command needs no ahead/behind, and paying for a git
	// call per worktree before doing the real work is waste.
	resolved, warnings, _, err := resolveTargets(session, selector, false)
	if err != nil {
		return nil, warnings, err
	}

	runTopics = make(map[string]string, len(resolved))
	targets := make([]fanout.Target, 0, len(resolved))
	for _, entry := range resolved {
		target := fanout.Target{
			Group:    entry.Context.RepoContext.Group,
			Repo:     entry.Context.RepoContext.Alias,
			Branch:   entry.Context.Branch,
			Path:     entry.Context.Path,
			BareRepo: entry.Context.RepoContext.BareRepo,
		}
		if entry.Item.Topic != nil {
			runTopics[target.Key()] = *entry.Item.Topic
		}
		targets = append(targets, target)
	}
	return targets, warnings, nil
}

// runOne executes the command in one worktree.
func runOne(ctx context.Context, t fanout.Target, command []string) fanout.ItemResult {
	//nolint:gosec // G204: an argv array from the user, run without a shell — that is
	// the point. There is no interpolation for a metacharacter to escape from.
	proc := exec.CommandContext(ctx, command[0], command[1:]...)
	proc.Dir = t.Path
	proc.Env = append(os.Environ(),
		"HYDRA_TOPIC="+runTopics[t.Key()],
		"HYDRA_REPO="+t.Repo,
		"HYDRA_GROUP="+t.Group,
		"HYDRA_BRANCH="+t.Branch,
		"HYDRA_PATH="+t.Path,
	)

	// Under a TTY the streams pass straight through: a human watching a long run needs
	// to see it arrive. Under --output json they are CAPTURED per worktree instead,
	// because passthrough output from several worktrees is unattributable — and with
	// --jobs it interleaves mid-line, so no caller can reconstruct who wrote what.
	if jsonMode() {
		out, errOut := &headWriter{}, &tailWriter{}
		proc.Stdout, proc.Stderr = out, errOut
		defer func() {
			o, ob, ot := out.captured()
			e, eb, et := errOut.captured()
			recordCapture(t.Key(), capturedOutput{
				Stdout: o, StdoutBytes: ob, StdoutTrunc: ot,
				Stderr: e, StderrBytes: eb, StderrTrunc: et,
			})
		}()
	} else {
		proc.Stdout, proc.Stderr = os.Stdout, os.Stderr
	}

	// WaitDelay bounds the wait for inherited pipes after a timeout kill. Without it a
	// grandchild holding stdout keeps hydra waiting long past the deadline.
	proc.WaitDelay = 200 * time.Millisecond

	err := proc.Run()
	switch {
	case ctx.Err() != nil:
		return fanout.ItemResult{
			Disposition: fanout.Failed,
			Reason:      fmt.Sprintf("timed out after %s", runTimeout),
			Err:         ctx.Err(),
		}
	case err != nil:
		return fanout.ItemResult{
			Disposition: fanout.Failed,
			Reason:      fmt.Sprintf("exit %d", exitCodeOf(err)),
			Err:         err,
		}
	}
	return fanout.ItemResult{Disposition: fanout.Created, Reason: "exit 0"}
}

// exitCodeOf extracts a process exit status, using -1 for "killed or never ran" so a
// timeout is distinguishable from a command that chose to exit non-zero.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func failedRunTargets(payload runJSON) []map[string]any {
	var out []map[string]any
	for _, result := range payload.Results {
		if !result.Failed {
			continue
		}
		out = append(out, map[string]any{
			"repo": result.Repo, "branch": result.Branch,
			"exit_code": result.ExitCode, "error": result.Error,
		})
	}
	return out
}

func runSummary(payload runJSON) string {
	name := strings.Join(payload.Command, " ")
	switch {
	case payload.Failed == 0:
		return fmt.Sprintf("%q succeeded in %d worktree(s)", name, payload.Total)
	case payload.TimedOut > 0:
		return fmt.Sprintf("%q failed in %d of %d worktree(s), %d timed out",
			name, payload.Failed, payload.Total, payload.TimedOut)
	default:
		return fmt.Sprintf("%q failed in %d of %d worktree(s)", name, payload.Failed, payload.Total)
	}
}

func printRunText(payload runJSON, summary string) {
	fmt.Println()
	if payload.Failed == 0 {
		fmt.Println(styles.Success.Render("✓ " + summary))
	} else {
		fmt.Println(styles.Error.Render("✗ " + summary))
	}
	fmt.Println()
	for _, result := range payload.Results {
		status := styles.Success.Render("ok  ")
		detail := ""
		if result.Failed {
			status = styles.Error.Render("fail")
			detail = "  " + result.Error
		}
		fmt.Printf("  %s %-24s %s%s\n", status, result.Repo+"/"+result.Branch,
			fmt.Sprintf("%dms", result.MS), detail)
	}
	fmt.Println()
}

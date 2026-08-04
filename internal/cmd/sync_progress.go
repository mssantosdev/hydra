package cmd

import (
	"fmt"
	"os"
	"sync"

	"github.com/mssantosdev/hydra/internal/fanout"
	"github.com/mssantosdev/hydra/internal/ui/components"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// syncReporter renders fan-out progress as it happens.
//
// What it replaces was not progress at all: the old code waited for every pull to
// finish and only then printed a numbered list, so a slow repo showed nothing for
// the whole run and the numbering reflected channel-drain order rather than
// anything stable. Each item now prints once, when it actually completes.
//
// Deliberately line-based rather than a Bubble Tea program. Output must stay usable
// when redirected to a file or a CI log, and the alt-screen TUI was removed for the
// same reason. components.Task supplies the per-item timing.
type syncReporter struct {
	mu    sync.Mutex
	total int
	done  int
	tasks map[string]*components.Task
	// quiet suppresses progress under --output json, where the envelope is the
	// output and a machine consumer wants nothing else.
	//
	// It is deliberately NOT gated on a TTY: progress lines go to stderr, so they
	// cannot corrupt a piped stdout, and a CI log is exactly where "which repo is
	// slow" is worth knowing. Gating on interactive() would silence it there.
	quiet bool
}

func newSyncReporter(total int) *syncReporter {
	return &syncReporter{
		total: total,
		tasks: make(map[string]*components.Task, total),
		quiet: jsonMode(),
	}
}

// Start records when an item began. Nothing is printed: with items completing out
// of order, a "starting" line would interleave with completion lines and make the
// output unreadable.
func (r *syncReporter) Start(t fanout.Target) {
	if r.quiet {
		return
	}
	task := components.NewTask(t.Key())
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done == 0 && len(r.tasks) == 0 {
		// stderr, matching the completion lines below: a header on stdout would
		// split one block across two streams and pollute a piped result.
		_, _ = fmt.Fprintln(os.Stderr)
		_, _ = fmt.Fprintln(os.Stderr, styles.Title.Render("Pulling Updates"))
		_, _ = fmt.Fprintln(os.Stderr)
	}
	r.tasks[t.Key()] = &task
}

// Finish prints one completion line for an item.
//
// Writes go to stderr so `hydra sync` remains pipeable: progress is diagnostic
// output, and stdout belongs to the result.
func (r *syncReporter) Finish(result fanout.ItemResult) {
	if r.quiet {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := result.Target.Key()
	task, ok := r.tasks[key]
	if !ok {
		task = &components.Task{Name: key}
	}
	if result.Err != nil {
		task.Fail(result.Err)
	} else {
		task.Complete()
	}

	r.done++
	status := styles.Success.Render("ok")
	if result.Disposition == fanout.Failed {
		status = styles.Error.Render("fail")
	}
	_, _ = fmt.Fprintf(os.Stderr, "  %s %d/%d %s/%s (%s)\n",
		status, r.done, r.total, result.Target.Repo, result.Target.Branch, task.DurationString())
}

// finish closes the block so following output is not glued to the last item.
func (r *syncReporter) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.quiet || r.done == 0 {
		return
	}
	_, _ = fmt.Fprintln(os.Stderr)
}

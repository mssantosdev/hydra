// Package components holds shared UI value types.
//
// It used to also hold Bubble Tea models — a Spinner and two Progress bars — which
// were unreachable for the whole of v1. `git log -S` shows one commit wired a spinner
// into clone's bare fetch and a later rewrite removed the call sites. Restoring them
// was considered and rejected on evidence: clone's fetch streams git's OWN output
// (`Receiving objects: 47% … 3.4 MiB/s`), which is real progress. A tea.Program would
// either fight git for the terminal or have to suppress that output, and a spinner
// saying "Cloning bare repository" is strictly less informative than a transfer rate.
//
// What survived is the one type something actually uses.
package components

import (
	"fmt"
	"time"
)

// Task tracks one unit of work for a progress renderer: its name, how long it took,
// and whether it failed.
//
// Deliberately not a Bubble Tea model. The fan-out engine reports items as they
// complete and its renderer writes plain lines to stderr, so nothing here needs an
// event loop, and a value type stays usable when output is piped.
type Task struct {
	Name      string
	StartTime time.Time
	EndTime   *time.Time
	Error     error
}

// NewTask starts timing a task.
func NewTask(name string) Task {
	return Task{Name: name, StartTime: time.Now()}
}

// Complete stops the clock.
func (t *Task) Complete() {
	now := time.Now()
	t.EndTime = &now
}

// Fail stops the clock and records why.
func (t *Task) Fail(err error) {
	t.Error = err
	t.Complete()
}

// Duration is the elapsed time, still running or finished.
func (t Task) Duration() time.Duration {
	if t.EndTime != nil {
		return t.EndTime.Sub(t.StartTime)
	}
	return time.Since(t.StartTime)
}

// DurationString formats the duration for a progress line.
//
// Sub-second work gets milliseconds: the previous implementation branched on
// `d < time.Second` and then returned the SAME "%.1fs" format in both arms, so every
// fast item rendered as a useless "0.0s".
func (t Task) DurationString() string {
	d := t.Duration()
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

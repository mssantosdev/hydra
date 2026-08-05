package cmd

import "sync"

// runCaptureCap bounds what one worktree contributes to the envelope, per stream.
//
// A build log can be megabytes and there may be dozens of worktrees, so an uncapped
// capture would put the whole log in a JSON document a caller has to parse in memory.
// 64 KiB is far more than a diagnostic needs and small enough that a fan-out across a
// large workspace stays a reasonable document.
const runCaptureCap = 64 << 10

// capturedOutput is what one child wrote, bounded, with the true sizes preserved so a
// caller knows how much it is NOT seeing rather than silently reading a prefix.
type capturedOutput struct {
	Stdout      string
	Stderr      string
	StdoutBytes int64
	StderrBytes int64
	StdoutTrunc bool
	StderrTrunc bool
}

// runCaptures collects per-worktree output during a fan-out. fanout runs items
// concurrently under --jobs, so unlike runTopics — written once before the run — this is
// written from several goroutines and needs the mutex.
var (
	runCapturesMu sync.Mutex
	runCaptures   map[string]capturedOutput
)

func recordCapture(key string, out capturedOutput) {
	runCapturesMu.Lock()
	defer runCapturesMu.Unlock()
	if runCaptures == nil {
		runCaptures = map[string]capturedOutput{}
	}
	runCaptures[key] = out
}

func takeCapture(key string) capturedOutput {
	runCapturesMu.Lock()
	defer runCapturesMu.Unlock()
	return runCaptures[key]
}

func resetCaptures() {
	runCapturesMu.Lock()
	defer runCapturesMu.Unlock()
	runCaptures = nil
}

// headWriter keeps the FIRST cap bytes and counts every byte written. Used for stdout,
// where a command's useful output starts at the beginning.
type headWriter struct {
	buf   []byte
	total int64
}

func (w *headWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	if room := runCaptureCap - len(w.buf); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.buf = append(w.buf, p[:room]...)
	}
	return len(p), nil
}

func (w *headWriter) captured() (string, int64, bool) {
	return string(w.buf), w.total, w.total > int64(len(w.buf))
}

// tailWriter keeps the LAST cap bytes. Used for stderr: the reason a command failed is
// the last thing it writes, so capping the head would discard exactly the diagnostic
// that makes a failure legible.
type tailWriter struct {
	buf   []byte
	total int64
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.total += int64(len(p))
	w.buf = append(w.buf, p...)
	if len(w.buf) > runCaptureCap {
		w.buf = w.buf[len(w.buf)-runCaptureCap:]
	}
	return len(p), nil
}

func (w *tailWriter) captured() (string, int64, bool) {
	return string(w.buf), w.total, w.total > int64(len(w.buf))
}

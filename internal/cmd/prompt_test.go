package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/huh"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// scriptPrompts feeds keystrokes to every prompt for the duration of a test and captures what
// they draw. Before the prompt funnel existed, each of these branches read the real terminal and
// so was unreachable from a test: the code deciding what happens when you answer "no" was
// verified by nobody.
func scriptPrompts(t *testing.T, keys string) *bytes.Buffer {
	t.Helper()
	drawn := &bytes.Buffer{}
	savedIn, savedOut := promptIn, promptOut
	promptIn, promptOut = strings.NewReader(keys), drawn
	t.Cleanup(func() { promptIn, promptOut = savedIn, savedOut })
	return drawn
}

// Keystrokes bubbletea reads from a plain io.Reader.
const (
	keyEnter = "\r"
	keyYes   = "y"
	keyNo    = "n"
	keyCtrlC = "\x03"
)

func TestPromptConfirmReturnsWhatWasAnswered(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want bool
	}{
		{"yes", keyYes + keyEnter, true},
		{"no", keyNo + keyEnter, false},
		// A bare Enter takes the default, which for a destructive confirm must be NO.
		{"enter alone defaults to no", keyEnter, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCommandState(t)
			scriptPrompts(t, tt.keys)

			got, err := runConfirm("Remove it?")
			if err != nil {
				t.Fatalf("runConfirm: %v", err)
			}
			if got != tt.want {
				t.Errorf("runConfirm = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPromptNeverDrawsOnStdout(t *testing.T) {
	// stdout carries the JSON envelope. A form painting into it would corrupt the one output a
	// caller parses, so the default destination must be the other stream. Asserting the default
	// rather than a rendered frame: what huh draws depends on TERM, where the bytes go does not.
	if promptOut != os.Stderr {
		t.Errorf("prompts draw on %v, want os.Stderr", promptOut)
	}
	if promptIn != os.Stdin {
		t.Errorf("prompts read from %v, want os.Stdin", promptIn)
	}

	resetCommandState(t)
	drawn := scriptPrompts(t, keyYes+keyEnter)
	if _, err := runConfirm("A question a human reads"); err != nil {
		t.Fatalf("runConfirm: %v", err)
	}
	if drawn.Len() == 0 {
		t.Error("the prompt drew nothing to the writer it was given")
	}
}

// With TERM=dumb — CI runners and editor shells set it — huh runs in accessible mode, where a
// select reads a TYPED NUMBER rather than arrow keys. That is the shape this path really has.
func TestPromptSelectTakesTheNumberedChoice(t *testing.T) {
	resetCommandState(t)
	t.Setenv("TERM", "dumb")
	scriptPrompts(t, "2"+keyEnter)

	got, err := runSelect("Pick one", huh.NewOptions("first", "second", "third"), "")
	if err != nil {
		t.Fatalf("runSelect: %v", err)
	}
	if got != "second" {
		t.Errorf("answering 2 selected %q, want %q", got, "second")
	}
}

// A mistyped answer must be an ordinary error, not a crash. huh panics with an index out of
// range on unparseable input in accessible mode, and a prompt taking the process down with it is
// never acceptable.
func TestPromptSurvivesAnUnparseableAnswer(t *testing.T) {
	resetCommandState(t)
	t.Setenv("TERM", "dumb")
	scriptPrompts(t, "not-a-number"+keyEnter)

	got, err := runSelect("Pick one", huh.NewOptions("first", "second"), "")
	if err == nil {
		t.Fatalf("an unparseable answer returned %q and no error", got)
	}
	if code := output.Classify(err).Code; code != output.CodeCancelled {
		t.Errorf("code = %q, want %q", code, output.CodeCancelled)
	}
	if !strings.Contains(err.Error(), "flag") {
		t.Errorf("the error does not say how to avoid the prompt: %q", err.Error())
	}
}

func TestPromptInputReturnsWhatWasTyped(t *testing.T) {
	resetCommandState(t)
	scriptPrompts(t, "feat/typed"+keyEnter)

	got, err := runInput("Branch name")
	if err != nil {
		t.Fatalf("runInput: %v", err)
	}
	if got != "feat/typed" {
		t.Errorf("runInput = %q, want %q", got, "feat/typed")
	}
}

// Answering "no" to a destructive confirm must abort and delete nothing. This is the branch that
// mattered most and could not be reached before.
func TestRemoveDeclinedAtThePromptDeletesNothing(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	worktree := env.GetWorktreePath("backend", "api-stage")
	if !env.DirExists(worktree) {
		t.Fatalf("fixture did not create %s", worktree)
	}

	// Force the interactive path, then answer no.
	resetCommandState(t)
	resetCommandIO()
	restore := withInteractive(t)
	defer restore()
	scriptPrompts(t, keyNo+keyEnter)

	rootCmd.SetArgs([]string{"remove", "api-stage"})
	err := rootCmd.Execute()

	if err == nil {
		t.Fatal("declining the confirm must not report success")
	}
	if code := output.Classify(err).Code; code != output.CodeCancelled {
		t.Errorf("code = %q, want %q", code, output.CodeCancelled)
	}
	if !env.DirExists(worktree) {
		t.Error("the worktree was deleted after the confirm was declined")
	}
}

// And answering yes goes through, so the test above is not passing because the prompt never ran.
func TestRemoveConfirmedAtThePromptDeletesIt(t *testing.T) {
	resetCommandState(t)
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()

	resetCommandState(t)
	resetCommandIO()
	rootCmd.SetArgs([]string{"add", "api", "stage", "--output", "json"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("add: %v", err)
	}
	worktree := env.GetWorktreePath("backend", "api-stage")

	resetCommandState(t)
	resetCommandIO()
	restore := withInteractive(t)
	defer restore()
	scriptPrompts(t, keyYes+keyEnter)

	rootCmd.SetArgs([]string{"remove", "api-stage"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("confirming the removal failed: %v", err)
	}
	if env.DirExists(worktree) {
		t.Error("the worktree survived a confirmed removal")
	}
}

// withInteractive forces the prompt policy on, so a test can reach the branches behind a question.
func withInteractive(t *testing.T) func() {
	t.Helper()
	saved := promptPolicy
	promptPolicy = func() bool { return true }
	return func() { promptPolicy = saved }
}

// selectWorktreesToSync pre-selects the clean worktrees and leaves the dirty ones out, because a
// dirty worktree needs a --dirty policy chosen deliberately. Accepting the defaults must honour
// that, and nothing verified it while the multiselect read a real terminal.
func TestSyncSelectorPreselectsCleanWorktreesOnly(t *testing.T) {
	resetCommandState(t)
	t.Setenv("TERM", "xterm-256color")
	drawn := scriptPrompts(t, keyEnter)

	// group/name is the selector's key — it is what makes two worktrees of the SAME repo
	// distinguishable, so a fixture that omits it collides every entry onto one key.
	entries := []syncEntry{
		{group: "backend", name: "api", repo: "api", branch: "main", behind: 2},
		{group: "backend", name: "api-stage", repo: "api", branch: "stage", behind: 1, dirty: true, changes: 3},
		{group: "frontend", name: "web", repo: "web", branch: "main", behind: 4},
	}
	got := selectWorktreesToSync(entries)

	if len(got) != len(entries) {
		t.Fatalf("selector returned %d entries, want %d", len(got), len(entries))
	}
	selected := map[string]bool{}
	for _, e := range got {
		if e.selected {
			selected[e.repo+"@"+e.branch] = true
		}
	}
	for _, want := range []string{"api@main", "web@main"} {
		if !selected[want] {
			t.Errorf("clean worktree %s was not pre-selected", want)
		}
	}
	if selected["api@stage"] {
		t.Error("the dirty worktree was pre-selected; it needs a --dirty policy chosen first")
	}
	// The header introducing the question must land on the same stream as the question.
	if !strings.Contains(drawn.String(), "Worktrees with Available Updates") {
		t.Error("the selector header did not go to the prompt stream")
	}
}

// Ctrl-C at the selector selects nothing, rather than falling through to sync the pre-selected
// set. Pulling and stashing after the human aborted the question would be acting without consent.
//
// EOF is deliberately NOT tested here: selectWorktreesToSync is reached only through
// interactive(), which requires a real terminal on stdin, so a closed stdin never gets this far.
func TestSyncSelectorAbortedSelectsNothing(t *testing.T) {
	resetCommandState(t)
	// A terminal huh can actually drive. Under TERM=dumb this selector is unreachable, because
	// output.Interactive refuses a terminal where an abort cannot be told from an answer.
	t.Setenv("TERM", "xterm-256color")
	scriptPrompts(t, keyCtrlC)

	got := selectWorktreesToSync([]syncEntry{
		{group: "backend", name: "api", repo: "api", branch: "main", behind: 2},
	})
	if got != nil {
		t.Errorf("an aborted selector returned %d entries, want none", len(got))
	}
}

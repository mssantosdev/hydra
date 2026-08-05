package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// surfacePath locates the committed snapshot from the package directory.
func surfacePath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	// internal/cmd -> repository root.
	return filepath.Join(wd, "..", "..", "SURFACE.txt")
}

// The committed SURFACE.txt must match what hydra actually offers.
//
// This is the contract test the whole snapshot exists for: adding a command, renaming
// a flag, or changing an exit code becomes a reviewable DIFF in the pull request
// instead of arriving unnoticed. It is deliberately a byte comparison — a looser check
// would pass while the published surface drifted from reality.
//
// When it fails and the change was intended, regenerate:
//
//	hydra commands --output text > SURFACE.txt
func TestSurfaceSnapshotMatchesReality(t *testing.T) {
	resetCommandState(t)

	want, err := os.ReadFile(surfacePath(t))
	if err != nil {
		t.Fatalf("SURFACE.txt is missing; regenerate it with \"hydra commands --output text > SURFACE.txt\": %v", err)
	}
	got := renderSurfaceText(describeSurface())

	if got == string(want) {
		return
	}

	// Report the first differing line rather than dumping 144 lines twice: a reviewer
	// needs to know WHAT moved.
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(string(want), "\n")
	for i := 0; i < len(gotLines) || i < len(wantLines); i++ {
		g, w := "", ""
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if g != w {
			t.Fatalf("SURFACE.txt is stale at line %d:\n  committed: %q\n  actual:    %q\n\n"+
				"If the surface change was intended, regenerate:\n"+
				"  hydra commands --output text > SURFACE.txt", i+1, w, g)
		}
	}
	t.Fatalf("SURFACE.txt differs in length: %d committed lines, %d actual", len(wantLines), len(gotLines))
}

// The published error-code table must be generated from the enum, not hand-listed, or
// it can say one thing while the code raises another.
func TestSurfaceErrorCodesComeFromTheEnum(t *testing.T) {
	resetCommandState(t)
	payload := describeSurface()

	if len(payload.ErrorCodes) == 0 {
		t.Fatal("the surface must publish the error-code table")
	}

	var retryable []string
	for _, entry := range payload.ErrorCodes {
		if entry.Exit == 0 {
			t.Errorf("%s maps to exit 0; a failure code cannot mean success", entry.Code)
		}
		if entry.Retryable {
			retryable = append(retryable, entry.Code)
		}
	}
	// busy is the only retryable code. Asserting the WHOLE set means adding a second
	// one has to be a deliberate change here too.
	if len(retryable) != 1 || retryable[0] != "busy" {
		t.Errorf("retryable codes = %v, want exactly [busy]", retryable)
	}
}

// Every command the surface describes must be addressable exactly as printed, so an
// agent can copy a name out and run it.
func TestSurfaceNamesArePaths(t *testing.T) {
	resetCommandState(t)
	payload := describeSurface()

	var found bool
	for _, command := range payload.Commands {
		if command.Name != "topic attach" {
			continue
		}
		found = true
		if command.HasSubcommands {
			t.Error("topic attach is runnable, not a group")
		}
	}
	if !found {
		t.Fatal("a subcommand must be listed by its full path, e.g. \"topic attach\"")
	}

	for _, command := range payload.Commands {
		if strings.TrimSpace(command.Name) != command.Name {
			t.Errorf("command name %q has surrounding whitespace", command.Name)
		}
		if command.Short == "" {
			t.Errorf("command %q has no summary", command.Name)
		}
	}
}

// The global flags are listed once rather than repeated on every command.
func TestSurfaceDoesNotRepeatGlobalFlags(t *testing.T) {
	resetCommandState(t)
	payload := describeSurface()

	global := make(map[string]bool, len(payload.GlobalFlags))
	for _, flag := range payload.GlobalFlags {
		global[flag.Name] = true
	}
	if !global["output"] || !global["project"] {
		t.Fatalf("global flags look wrong: %+v", payload.GlobalFlags)
	}

	for _, command := range payload.Commands {
		for _, flag := range command.Flags {
			if global[flag.Name] {
				t.Errorf("%s repeats the global flag --%s", command.Name, flag.Name)
			}
		}
	}
}

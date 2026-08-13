package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/output"
)

func TestWriteHookFailureText(t *testing.T) {
	err := output.Errorf(output.CodeHookFailed, "hook failed at groups.backend.hooks.post_add[0]\n  name: install dependencies\n  exit: 1\n  hint: fix the hook, then run \"hydra hooks run post_add --worktree api-stage\"")

	var buf bytes.Buffer
	writeHookFailureText(&buf, err)

	got := buf.String()
	if !strings.HasPrefix(got, "error: hook failed at groups.backend.hooks.post_add[0]\n") {
		t.Fatalf("got %q", got)
	}
	if !strings.Contains(got, "  name: install dependencies\n") {
		t.Fatalf("missing name line: %q", got)
	}
}

package carry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
)

// A manifest-authored `to:` value must not be able to write outside the worktree, whatever it is
// spelled like. CVE-2026-39822 was a root escape in os through a symlink plus a TRAILING SLASH,
// so the trailing-slash spellings are pinned here rather than left to the toolchain.
func TestCarryRefusesEveryEscapeSpelling(t *testing.T) {
	for _, dest := range []string{
		"../outside.txt",
		"../outside.txt/",
		"link/../../outside.txt",
		"link/",
		"/etc/hydra-owned",
	} {
		t.Run(dest, func(t *testing.T) {
			workspace := t.TempDir()
			worktree := filepath.Join(workspace, "wt")
			if err := os.MkdirAll(worktree, 0o750); err != nil {
				t.Fatal(err)
			}
			// A committed symlink pointing out of the worktree: the shape the escape needs.
			if err := os.Symlink("..", filepath.Join(worktree, "link")); err != nil {
				t.Fatal(err)
			}
			secret := filepath.Join(workspace, "secret.env")
			if err := os.WriteFile(secret, []byte("TOKEN=1"), 0o600); err != nil {
				t.Fatal(err)
			}

			results, _ := Apply([]config.CarryEntry{{From: "secret.env", To: dest}}, Plan{
				WorkspaceRoot: workspace,
				WorktreePath:  worktree,
			})

			for _, r := range results {
				if r.Disposition == Placed {
					t.Errorf("carry reported Placed for %q; nothing was written there", dest)
				}
			}
			// Nothing may appear anywhere above the worktree.
			entries, err := os.ReadDir(workspace)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "outside") || e.Name() == "hydra-owned" {
					t.Errorf("%q escaped the worktree and landed in the workspace", e.Name())
				}
			}
		})
	}
}

// A destination that already exists is Skipped, whatever the trailing slash. Reporting Placed
// would make `carried: N` include files nobody wrote — the same lie in the other direction.
func TestCarryReportsAnExistingDestinationAsSkipped(t *testing.T) {
	for _, dest := range []string{"kept.env", "kept.env/"} {
		t.Run(dest, func(t *testing.T) {
			workspace := t.TempDir()
			worktree := filepath.Join(workspace, "wt")
			if err := os.MkdirAll(worktree, 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "kept.env"), []byte("NEW=1"), 0o600); err != nil {
				t.Fatal(err)
			}
			existing := filepath.Join(worktree, "kept.env")
			if err := os.WriteFile(existing, []byte("MINE=1"), 0o600); err != nil {
				t.Fatal(err)
			}

			results, _ := Apply([]config.CarryEntry{{From: "kept.env", To: dest}}, Plan{
				WorkspaceRoot: workspace,
				WorktreePath:  worktree,
			})

			if len(results) != 1 {
				t.Fatalf("got %d results, want 1", len(results))
			}
			if results[0].Disposition != Skipped {
				t.Errorf("disposition = %q, want %q", results[0].Disposition, Skipped)
			}
			body, err := os.ReadFile(existing)
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != "MINE=1" {
				t.Errorf("the existing file was overwritten: %q", body)
			}
		})
	}
}

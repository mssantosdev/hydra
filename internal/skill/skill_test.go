package skill_test

import (
	"sort"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/cmd"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/skill"
)

// The whole point of embedding the skill is that it cannot silently diverge from
// the binary shipping it. These tests fail the build when a command or an error
// code is added without documenting it, which is exactly the rot this repo
// suffered from with its hand-maintained agent guide.

func TestSkillDocumentsEveryCommand(t *testing.T) {
	var registered []string
	for _, c := range cmd.RootCommand().Commands() {
		// Hidden commands count as registered: they remain invocable, so the skill must
		// document them. `ui` is hidden as a deprecated alias of `status` and still works.
		if c.Name() == "help" {
			continue
		}
		registered = append(registered, c.Name())
	}
	sort.Strings(registered)

	documented := skill.DocumentedCommands()
	sort.Strings(documented)

	if len(documented) == 0 {
		t.Fatal("the skill's Commands table parsed as empty; the table format changed")
	}

	missing := difference(registered, documented)
	extra := difference(documented, registered)
	if len(missing) > 0 {
		t.Errorf("commands registered on rootCmd but missing from skills/hydra/SKILL.md: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("commands documented in skills/hydra/SKILL.md but not registered: %v", extra)
	}
}

func TestSkillDocumentsEveryErrorCode(t *testing.T) {
	real := output.ExitCodes()
	documented := skill.DocumentedErrorCodes()

	if len(documented) == 0 {
		t.Fatal("the skill's error-code table parsed as empty; the table format changed")
	}

	for code, exit := range real {
		got, ok := documented[code]
		if !ok {
			t.Errorf("error code %q is missing from skills/hydra/SKILL.md", code)
			continue
		}
		if got != exit {
			t.Errorf("error code %q documents exit %d, but output.ExitFor says %d", code, got, exit)
		}
	}
	for code := range documented {
		if _, ok := real[code]; !ok {
			t.Errorf("error code %q is documented but not in the output package enum", code)
		}
	}
}

// TestSkillIsThin enforces "thin" mechanically rather than hoping for it.
func TestSkillIsThin(t *testing.T) {
	content := skill.Content()

	if lines := strings.Count(strings.TrimRight(content, "\n"), "\n") + 1; lines > 120 {
		t.Errorf("SKILL.md has %d lines, limit is 120", lines)
	}
	if size := len(content); size > 8*1024 {
		t.Errorf("SKILL.md is %d bytes, limit is 8192", size)
	}
}

func TestSkillHasFrontmatter(t *testing.T) {
	content := skill.Content()

	if !strings.HasPrefix(content, "---\n") {
		t.Fatal("SKILL.md must open with YAML frontmatter")
	}
	head, _, ok := strings.Cut(strings.TrimPrefix(content, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("SKILL.md frontmatter is not terminated")
	}
	if !strings.Contains(head, "name: "+skill.Name) {
		t.Errorf("frontmatter must declare name: %s, got %q", skill.Name, head)
	}
	if !strings.Contains(head, "description:") {
		t.Error("frontmatter must declare a description")
	}
}

func TestInstallWritesSkill(t *testing.T) {
	dir := t.TempDir()

	path, err := skill.Install(dir)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := dir + "/hydra/SKILL.md"; path != want {
		t.Fatalf("Install wrote %q, want %q", path, want)
	}

	// Overwriting is intentional: the embedded copy is authoritative.
	if _, err := skill.Install(dir); err != nil {
		t.Fatalf("Install is not idempotent: %v", err)
	}
}

func difference(a, b []string) []string {
	set := make(map[string]struct{}, len(b))
	for _, item := range b {
		set[item] = struct{}{}
	}
	var out []string
	for _, item := range a {
		if _, ok := set[item]; !ok {
			out = append(out, item)
		}
	}
	return out
}

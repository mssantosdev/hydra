package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/testutil"
)

// captureStdout swaps os.Stdout for a pipe. The text renderers write with fmt.Println rather
// than through cobra's writer, so a cobra buffer cannot see them — this is the only way to
// assert what a human is shown.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	saved := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()
	_ = w.Close()
	os.Stdout = saved
	out := <-done
	_ = r.Close()
	return out
}

// newHelperEnv builds a workspace with one repo and two worktrees and loads it into the
// command globals, which is what the helpers under test read.
func newHelperEnv(t *testing.T) *testutil.TestEnv {
	t.Helper()
	env := testutil.NewTestEnv(t)
	env.InitConfig()
	env.SetupRepo("backend", "api", "main", "stage")
	env.Chdir()
	projectRoot = env.RootDir
	projectConfigPath = config.ManifestPath(env.RootDir)
	cfg = env.LoadConfig()
	return env
}

func TestHelperGroupWorktreesKeepsEveryItemUnderItsGroup(t *testing.T) {
	items := []worktreeJSON{
		{Group: "backend", Name: "api", Branch: "main"},
		{Group: "backend", Name: "api-stage", Branch: "stage"},
		{Group: "frontend", Name: "web", Branch: "main"},
	}
	groups := groupWorktrees(items)
	if len(groups) != 2 {
		t.Fatalf("got %d groups, want 2: %v", len(groups), groups)
	}
	if len(groups["backend"]) != 2 || len(groups["frontend"]) != 1 {
		t.Errorf("items landed in the wrong groups: %v", groups)
	}
	// No item may be dropped: the table is built from this map, and a lost worktree is a
	// worktree the caller is never told about.
	total := 0
	for _, g := range groups {
		total += len(g)
	}
	if total != len(items) {
		t.Errorf("grouping lost items: %d in, %d out", len(items), total)
	}
}

// Group order must be deterministic, or the same workspace prints differently between runs and
// a diff of two listings is unreadable.
func TestHelperSortedGroupNamesIsDeterministic(t *testing.T) {
	groups := map[string][]worktreeJSON{"web": nil, "api": nil, "infra": nil}
	first := sortedGroupNames(groups)
	want := []string{"api", "infra", "web"}
	if strings.Join(first, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", first, want)
	}
	for i := 0; i < 5; i++ {
		if got := sortedGroupNames(groups); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("order changed between calls: %v", got)
		}
	}
}

// A branch with no upstream is a branch that was never pushed, not a failure, so its label must
// say so rather than rendering blank or "null".
func TestHelperUpstreamLabelNamesTheLocalOnlyCase(t *testing.T) {
	local := upstreamLabelJSON(worktreeJSON{Branch: "feat/new"})
	if strings.TrimSpace(local) == "" || strings.Contains(local, "null") {
		t.Errorf("local-only upstream label = %q, want something a reader understands", local)
	}
	tracked := upstreamLabelJSON(worktreeJSON{Branch: "main", Upstream: new("origin/main")})
	if !strings.Contains(tracked, "origin/main") {
		t.Errorf("tracked upstream label = %q, want it to name the upstream", tracked)
	}
}

func TestHelperStringsJoinSortedIsStable(t *testing.T) {
	got := stringsJoinSorted([]string{"web", "api", "infra"})
	if got != "api, infra, web" {
		t.Errorf("got %q, want a sorted, comma-joined list", got)
	}
	if got := stringsJoinSorted(nil); got != "" {
		t.Errorf("empty input = %q, want empty", got)
	}
}

func TestHelperRelativeToStaysReadableOutsideTheRoot(t *testing.T) {
	root := filepath.Join("/ws")
	if got := relativeTo(root, filepath.Join("/ws", "backend", "api")); got != filepath.Join("backend", "api") {
		t.Errorf("inside the root = %q, want a relative path", got)
	}
	// A path outside the root has no useful relative form; it must still render something a
	// human can act on rather than a pile of "..".
	outside := relativeTo(root, filepath.Join("/elsewhere", "api"))
	if strings.HasPrefix(outside, "../..") {
		t.Errorf("outside the root = %q, want an absolute or plain path instead of ..-chains", outside)
	}
}

func TestHelperShellSourceHintNamesTheRightRcFile(t *testing.T) {
	for shell, want := range map[string]string{
		"bash": "bashrc",
		"zsh":  "zshrc",
		"fish": "config.fish",
	} {
		if got := shellSourceHint(shell); !strings.Contains(got, want) {
			t.Errorf("shellSourceHint(%q) = %q, want it to mention %q", shell, got, want)
		}
	}
}

func TestHelperBoardProjectLabelDistinguishesOneFromAll(t *testing.T) {
	one := []projectTarget{{Name: "alpha"}}
	if got := boardProjectLabel(one, false); !strings.Contains(got, "alpha") {
		t.Errorf("single project label = %q, want it to name the project", got)
	}
	many := []projectTarget{{Name: "alpha"}, {Name: "beta"}}
	if got := boardProjectLabel(many, true); got == "" {
		t.Error("all-projects label is empty; the board would show no scope at all")
	}
}

func TestHelperBoardInitialFilterMirrorsTheSelector(t *testing.T) {
	if got := boardInitialFilter(Selector{}); got != "" {
		t.Errorf("an empty selector = %q, want no initial filter", got)
	}
	if got := boardInitialFilter(Selector{Filter: []string{"dirty"}}); !strings.Contains(got, "dirty") {
		t.Errorf("got %q, want the filter carried into the board", got)
	}
}

func TestHelperRegisteredAliasesReadsTheLoadedManifest(t *testing.T) {
	resetCommandState(t)
	env := newHelperEnv(t)
	_ = env

	aliases := registeredAliases()
	if len(aliases) == 0 {
		t.Fatal("no aliases from a manifest that registers one")
	}
	joined := strings.Join(aliases, ",")
	if !strings.Contains(joined, "api") {
		t.Errorf("aliases = %v, want the registered alias", aliases)
	}
}

func TestHelperNavigationHintsPointAtTheWorktree(t *testing.T) {
	resetCommandState(t)
	env := newHelperEnv(t)

	items, _ := collectWorktrees(cfg, projectRoot)
	if len(items) == 0 {
		t.Fatal("fixture produced no worktrees")
	}
	cdHint, switchHint := navigationHints(env.RootDir, items[0])
	if cdHint == "" || switchHint == "" {
		t.Fatalf("hints are empty: cd=%q switch=%q", cdHint, switchHint)
	}
	if !strings.Contains(switchHint, "hydra") {
		t.Errorf("switch hint = %q, want it to name the command to run", switchHint)
	}
}

// The text renderers are what a human sees. They are asserted on CONTENT — the worktree, the
// branch, the verdict — never on escape sequences, which are a styling choice.
func TestHelperTextRenderersReportTheFactsTheyAreGiven(t *testing.T) {
	t.Run("where", func(t *testing.T) {
		out := captureStdout(t, func() {
			printWhereText(whereJSON{Project: "alpha", Root: "/ws", Manifest: "/ws/.hydra/config.yaml", InProject: true})
		})
		for _, want := range []string{"alpha", "/ws"} {
			if !strings.Contains(out, want) {
				t.Errorf("printWhereText omitted %q:\n%s", want, out)
			}
		}
	})

	t.Run("trust, in each of its three states", func(t *testing.T) {
		cases := []struct {
			name    string
			payload trustJSON
			want    string
		}{
			{"trusted", trustJSON{Workspace: "/ws", Trusted: true, Executable: 2, ApprovedAt: "2026-01-01T00:00:00Z"}, "trusted"},
			{"untrusted", trustJSON{Workspace: "/ws", Executable: 2}, "hydra trust"},
			{"changed names the path, never the hook", trustJSON{Workspace: "/ws", Executable: 1, Changed: []string{"hooks.post_add[0]"}}, "hooks.post_add[0]"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				out := captureStdout(t, func() { printTrustText(tc.payload) })
				if !strings.Contains(out, tc.want) {
					t.Errorf("omitted %q:\n%s", tc.want, out)
				}
			})
		}
	})
}

// Completion is a user-facing surface: a wrong key list sends someone down a path the command
// will then refuse. Only `theme` has a closed set — an editor command line cannot be enumerated,
// so offering values there would be a lie.
func TestHelperConfigSetCompletionOffersOnlyWhatIsEnumerable(t *testing.T) {
	keys, _ := completeConfigSetArgs(nil, nil, "")
	if strings.Join(keys, ",") != "theme,editor" {
		t.Errorf("key completion = %v, want the two settable keys", keys)
	}

	themeValues, _ := completeConfigSetArgs(nil, []string{"theme"}, "")
	if len(themeValues) == 0 {
		t.Error("theme has a closed set of values and completed none")
	}

	editorValues, _ := completeConfigSetArgs(nil, []string{"editor"}, "")
	if len(editorValues) != 0 {
		t.Errorf("editor completed %v; an editor command line cannot be enumerated", editorValues)
	}

	beyond, _ := completeConfigSetArgs(nil, []string{"theme", "already-chosen"}, "")
	if len(beyond) != 0 {
		t.Errorf("completed %v past the last argument", beyond)
	}
}

// loadProjectAt is how --project and the registry reach a workspace that is not the ambient one.
// Loading a directory that is not a workspace must report that, not leave stale globals behind.
func TestHelperLoadProjectAtSwitchesTheActiveWorkspace(t *testing.T) {
	resetCommandState(t)
	env := newHelperEnv(t)

	if err := loadProjectAt(env.RootDir); err != nil {
		t.Fatalf("loading a real workspace failed: %v", err)
	}
	if projectRoot != env.RootDir {
		t.Errorf("projectRoot = %q, want %q", projectRoot, env.RootDir)
	}
	if cfg == nil || cfg.Project == "" {
		t.Error("the manifest was not loaded into the command globals")
	}

	// A directory with no manifest must be refused rather than silently keeping the previous
	// project loaded, which would make the command act on a workspace nobody named.
	if err := loadProjectAt(t.TempDir()); err == nil {
		t.Error("loading a directory with no manifest succeeded")
	}
}

// replaceInstallation is how `init-shell` upgrades an existing block in a user's rc file. Getting
// it wrong either duplicates the block on every run or eats the surrounding configuration.
func TestHelperReplaceInstallationRewritesOnlyItsOwnBlock(t *testing.T) {
	block := helperMarkerStart + "\nold helper\n" + helperMarkerEnd
	rc := "export EDITOR=vim\n" + block + "\nalias ll='ls -l'\n"

	got := replaceInstallation(rc, helperMarkerStart+"\nnew helper\n"+helperMarkerEnd)

	if strings.Contains(got, "old helper") {
		t.Errorf("the previous block survived:\n%s", got)
	}
	if !strings.Contains(got, "new helper") {
		t.Errorf("the new block is missing:\n%s", got)
	}
	// The user's own lines on both sides must be untouched — this edits a file someone else owns.
	for _, want := range []string{"export EDITOR=vim", "alias ll='ls -l'"} {
		if !strings.Contains(got, want) {
			t.Errorf("surrounding configuration %q was lost:\n%s", want, got)
		}
	}
	// Exactly one block, so repeated installs do not accumulate.
	if n := strings.Count(got, helperMarkerStart); n != 1 {
		t.Errorf("found %d start markers, want exactly 1:\n%s", n, got)
	}
}

func TestHelperReplaceInstallationAppendsWhenThereIsNoBlockYet(t *testing.T) {
	rc := "export EDITOR=vim\n"
	block := helperMarkerStart + "\nhelper\n" + helperMarkerEnd

	got := replaceInstallation(rc, block)

	if !strings.Contains(got, "export EDITOR=vim") {
		t.Error("appending dropped the existing configuration")
	}
	if !strings.Contains(got, block) {
		t.Error("the block was not appended")
	}
}

// The interactive branch picker reads what origin has and what is already checked out, so it can
// mark the ones that would be no-ops. Wrong data here offers the user branches that cannot be taken.
func TestHelperBranchChoicesReflectTheRemoteAndWhatIsCheckedOut(t *testing.T) {
	resetCommandState(t)
	newHelperEnv(t)

	repo, err := resolveRepoByAlias(cfg, projectRoot, "api")
	if err != nil {
		t.Fatalf("resolve api: %v", err)
	}
	choices, defaultBranch, err := branchChoicesForRepo(repo)
	if err != nil {
		t.Fatalf("branchChoicesForRepo: %v", err)
	}
	if len(choices) == 0 {
		t.Fatal("no choices from a repo with branches on origin")
	}
	if defaultBranch == "" {
		t.Error("no default branch was resolved")
	}
	names := make([]string, 0, len(choices))
	for _, c := range choices {
		names = append(names, c.Name)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"main", "stage"} {
		if !strings.Contains(joined, want) {
			t.Errorf("choices %v omit %q, which origin has", names, want)
		}
	}
}

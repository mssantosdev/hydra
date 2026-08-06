package browser

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// The filter vocabulary is shared with --filter on the non-interactive commands. A human
// who learns `dirty` here must get the same rows from `hydra list --filter dirty`, or the
// interactive surface has taught them something false.
func TestFilterVocabularyMatchesTheFlag(t *testing.T) {
	rows := []Row{
		{Repo: "checkout-api", Name: "checkout-api", Branch: "main", Topic: ""},
		{Repo: "checkout-api", Name: "checkout-api-feat", Branch: "feat/split", Topic: "PAY-4417", Dirty: true, Changes: 1},
		{Repo: "storefront-web", Name: "storefront-web", Branch: "main", Behind: 2},
		{Repo: "billing-worker", Name: "billing-worker", Branch: "stage", Ahead: 1},
	}

	tests := []struct {
		filter string
		want   []string
	}{
		{"", []string{"checkout-api", "checkout-api-feat", "storefront-web", "billing-worker"}},
		{"dirty", []string{"checkout-api-feat"}},
		{"behind", []string{"storefront-web"}},
		{"branch:feat/*", []string{"checkout-api-feat"}},
		{"branch:feat*", nil}, // * must not cross a / — path.Match, same as --filter
		{"branch:main", []string{"checkout-api", "storefront-web"}},
		{"ahead", nil}, // NOT a state word: --filter rejects it, so neither accepts it
		{"topic:pay-4417", []string{"checkout-api-feat"}},
		{"topic:", nil}, // an empty topic query must match nothing, not everything
		{"checkout", []string{"checkout-api", "checkout-api-feat"}},
		{"STAGE", []string{"billing-worker"}}, // case-insensitive over branch
		{"nope", nil},
	}

	for _, tc := range tests {
		t.Run(tc.filter, func(t *testing.T) {
			m := &model{rows: rows, filter: tc.filter, loaded: true}
			m.applyFilter()

			var got []string
			for _, i := range m.view {
				got = append(got, m.rows[i].Name)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("filter %q matched %v, want %v", tc.filter, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("filter %q: row %d = %q, want %q", tc.filter, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// An unassigned topic is a permanent first-class state, so a bare substring search must not
// accidentally match on the placeholder glyph the view draws for it.
func TestUnassignedTopicIsNotSearchable(t *testing.T) {
	m := &model{rows: []Row{{Repo: "api", Name: "api", Branch: "main", Topic: ""}}, loaded: true}
	for _, q := range []string{"—", "-", "none", "null"} {
		m.filter = q
		m.applyFilter()
		if len(m.view) != 0 {
			t.Errorf("filter %q matched an unassigned row; the placeholder leaked into the index", q)
		}
	}
}

// The cursor must stay inside the filtered view. Filtering down to fewer rows than the
// cursor's position used to index past the end of the slice.
func TestCursorSurvivesAFilterThatShrinksTheView(t *testing.T) {
	rows := make([]Row, 8)
	for i := range rows {
		rows[i] = Row{Repo: "r", Name: string(rune('a' + i)), Branch: "main"}
	}
	rows[0].Dirty = true

	m := &model{rows: rows, loaded: true, width: 100, height: 30}
	m.applyFilter()
	m.cursor = 7
	m.clampCursor()

	m.filter = "dirty"
	m.applyFilter()

	if m.cursor != 0 {
		t.Errorf("cursor = %d after filtering 8 rows down to 1, want 0", m.cursor)
	}
	if _, ok := m.current(); !ok {
		t.Error("current() reported no row while one is visible")
	}
}

// An empty board must not report a selection, and must not panic on navigation.
func TestEmptyBoardIsNavigable(t *testing.T) {
	m := &model{loaded: true, width: 80, height: 24}
	m.applyFilter()
	m.move(1)
	m.move(-5)
	if _, ok := m.current(); ok {
		t.Error("current() returned a row from an empty board")
	}
	if v := m.View(); v == "" {
		t.Error("an empty board rendered nothing; it must say so")
	}
}

// Until the first load lands, the view must not claim the workspace is empty. Collapsing
// "not read yet" into "nothing here" printed "no worktrees in this project" over a workspace
// holding nine of them.
func TestUnloadedIsNotReportedAsEmpty(t *testing.T) {
	m := &model{width: 80, height: 24}
	got := m.View()
	if got == "" {
		t.Fatal("rendered nothing before the first load")
	}
	if contains(got, "no worktrees in this project") {
		t.Errorf("claimed the workspace is empty before reading it:\n%s", got)
	}
}

func TestSummarizeRows(t *testing.T) {
	tests := []struct {
		name string
		rows []Row
		want Counts
	}{
		{
			name: "clean tracked worktree",
			rows: []Row{{Name: "api", Upstream: "origin/main"}},
			want: Counts{Total: 1, Clean: 1},
		},
		{
			name: "mixed states",
			rows: []Row{
				{Name: "clean", Upstream: "origin/main"},
				{Name: "dirty", Upstream: "origin/main", Dirty: true},
				{Name: "behind", Upstream: "origin/main", Behind: 2},
				{Name: "ahead", Upstream: "origin/main", Ahead: 1},
				{Name: "local", Upstream: "local-only", Branch: "feat/x"},
				{Name: "detached", Detached: true},
			},
			want: Counts{Total: 6, Clean: 1, Dirty: 1, Behind: 1, Ahead: 1, LocalOnly: 1, Detached: 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := summarizeRows(tc.rows)
			if got != tc.want {
				t.Fatalf("summarizeRows() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestGroupHeadersAppearOncePerGroup(t *testing.T) {
	rows := []Row{
		{Group: "services", Name: "api-a", Branch: "main", Upstream: "origin/main"},
		{Group: "services", Name: "api-b", Branch: "main", Upstream: "origin/main"},
		{Group: "web", Name: "web-a", Branch: "main", Upstream: "origin/main"},
	}
	m := &model{
		loaded: true, width: 120, height: 30, project: "demo",
		rows: rows, summary: summarizeRows(rows),
	}
	m.applyFilter()

	out := m.View()
	if strings.Count(out, "▸ SERVICES") != 1 {
		t.Fatalf("expected one SERVICES header, got %d in:\n%s", strings.Count(out, "▸ SERVICES"), out)
	}
	if strings.Count(out, "▸ WEB") != 1 {
		t.Fatalf("expected one WEB header, got %d in:\n%s", strings.Count(out, "▸ WEB"), out)
	}
}

func TestGroupHeadersDoNotConsumeCursorIndices(t *testing.T) {
	rows := []Row{
		{Group: "alpha", Name: "a1", Branch: "main", Upstream: "origin/main"},
		{Group: "alpha", Name: "a2", Branch: "main", Upstream: "origin/main"},
		{Group: "beta", Name: "b1", Branch: "main", Upstream: "origin/main"},
		{Group: "beta", Name: "b2", Branch: "main", Upstream: "origin/main"},
	}
	m := &model{
		loaded: true, width: 120, height: 30, project: "demo",
		rows: rows, summary: summarizeRows(rows),
	}
	m.applyFilter()

	for wantCursor := 0; wantCursor < len(m.view); wantCursor++ {
		m.cursor = wantCursor
		row, ok := m.current()
		if !ok {
			t.Fatalf("cursor %d: current() failed with %d visible rows", wantCursor, len(m.view))
		}
		if row.Name != rows[m.view[wantCursor]].Name {
			t.Fatalf("cursor %d: got row %q, want %q", wantCursor, row.Name, rows[m.view[wantCursor]].Name)
		}
	}
}

func TestAgainstColumnOnlyWhenPresent(t *testing.T) {
	without := &model{
		loaded: true, width: 120, height: 30, project: "demo",
		rows: []Row{{Group: "g", Name: "api", Branch: "main", Upstream: "origin/main"}},
	}
	without.summary = summarizeRows(without.rows)
	without.applyFilter()
	if got := without.View(); contains(got, "unmerged") || contains(got, "merged") {
		t.Fatalf("against column leaked into view without data:\n%s", got)
	}

	with := &model{
		loaded: true, width: 120, height: 30, project: "demo",
		rows: []Row{{
			Group: "g", Name: "api", Branch: "main", Upstream: "origin/main",
			Against: &AgainstInfo{Ref: "main", Ahead: 2, Behind: 1, Merged: false},
		}},
	}
	with.summary = summarizeRows(with.rows)
	with.applyFilter()
	got := with.View()
	if !contains(got, "MAIN") {
		t.Fatalf("expected against ref header, got:\n%s", got)
	}
	if !contains(got, "unmerged") {
		t.Fatalf("expected unmerged label, got:\n%s", got)
	}
	if !contains(got, "↑2") || !contains(got, "↓1") {
		t.Fatalf("expected against arrows, got:\n%s", got)
	}
}

func TestCursorValidWhenFilterCrossesGroupBoundaries(t *testing.T) {
	rows := []Row{
		{Group: "alpha", Name: "a1", Branch: "main", Upstream: "origin/main"},
		{Group: "alpha", Name: "a2", Branch: "main", Upstream: "origin/main", Dirty: true},
		{Group: "beta", Name: "b1", Branch: "main", Upstream: "origin/main", Dirty: true},
		{Group: "beta", Name: "b2", Branch: "main", Upstream: "origin/main"},
	}
	m := &model{loaded: true, width: 120, height: 30, rows: rows}
	m.applyFilter()
	m.cursor = 3
	m.clampCursor()

	m.filter = "dirty"
	m.applyFilter()

	if m.cursor < 0 || m.cursor >= len(m.view) {
		t.Fatalf("cursor %d out of range for %d visible rows", m.cursor, len(m.view))
	}
	row, ok := m.current()
	if !ok {
		t.Fatal("current() failed after filter across groups")
	}
	if !row.Dirty {
		t.Fatalf("cursor landed on non-dirty row %q", row.Name)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(h, n string) int {
	for i := 0; i+len(n) <= len(h); i++ {
		if h[i:i+len(n)] == n {
			return i
		}
	}
	return -1
}

// The board must render from the RESOLVED theme, not from a package default.
//
// The old global theme initialiser was never synced from config; only the interactive
// `config` form wrote it, inside its own process. Reading it here made this the one view
// that ignored the user's configured theme, so it drew a different palette than `list` and
// `status` in the same terminal. Asserting on styles.* is the point: those are what every
// other view reads.
func TestViewRendersFromTheResolvedPalette(t *testing.T) {
	origGreen, origBlue := styles.Green, styles.Blue
	t.Cleanup(func() { styles.Green, styles.Blue = origGreen, origBlue })

	// Sentinels no theme in the catalog uses.
	styles.Green, styles.Blue = lipgloss.Color("#010203"), lipgloss.Color("#040506")
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := &model{
		loaded: true, width: 100, height: 24, project: "p",
		rows: []Row{{Group: "g", Repo: "api", Name: "api", Branch: "main", Upstream: "origin/main"}},
	}
	m.summary = summarizeRows(m.rows)
	m.applyFilter()
	m.cursor = -1 // keep the row unselected so its own colours render
	out := m.View()

	if !contains(out, "1;2;3") && !contains(out, "010203") {
		t.Errorf("the clean status did not use styles.Green; the view is reading a different source\n%q", out)
	}
	if !contains(out, "4;5;6") && !contains(out, "040506") {
		t.Errorf("the branch did not use styles.Blue; the view is reading a different source\n%q", out)
	}
}

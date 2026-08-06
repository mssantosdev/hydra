package browser

import (
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

// An empty register must not report a selection, and must not panic on navigation.
func TestEmptyRegisterIsNavigable(t *testing.T) {
	m := &model{loaded: true, width: 80, height: 24}
	m.applyFilter()
	m.move(1)
	m.move(-5)
	if _, ok := m.current(); ok {
		t.Error("current() returned a row from an empty register")
	}
	if v := m.View(); v == "" {
		t.Error("an empty register rendered nothing; it must say so")
	}
}

// Until the first load lands, the view must not claim the workspace is empty. Collapsing
// "not read yet" into "nothing here" printed "no worktrees registered" over a workspace
// holding nine of them.
func TestUnloadedIsNotReportedAsEmpty(t *testing.T) {
	m := &model{width: 80, height: 24}
	got := m.View()
	if got == "" {
		t.Fatal("rendered nothing before the first load")
	}
	if contains(got, "no worktrees registered") {
		t.Errorf("claimed the workspace is empty before reading it:\n%s", got)
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

// The register must render from the RESOLVED theme, not from a package default.
//
// themes.Current sat at its initialiser for the entire process because loadTheme never
// published to it; only the interactive `config` form wrote it, inside its own process.
// Reading it here made this the one view that ignored the user's configured theme, so it
// drew a different palette than `list` and `status` in the same terminal. Asserting on
// styles.* is the point: those are what every other view reads.
func TestViewRendersFromTheResolvedPalette(t *testing.T) {
	origGreen, origBlue := styles.Green, styles.Blue
	t.Cleanup(func() { styles.Green, styles.Blue = origGreen, origBlue })

	// Sentinels no theme in the catalog uses.
	styles.Green, styles.Blue = lipgloss.Color("#010203"), lipgloss.Color("#040506")
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := &model{
		loaded: true, width: 100, height: 24, project: "p",
		rows: []Row{{Repo: "api", Name: "api", Branch: "main", Upstream: "origin/main"}},
	}
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

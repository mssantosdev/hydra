package browser

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func sampleRows() []Row {
	return []Row{
		{Name: "alpha", Repo: "alpha", Branch: "main", Path: "/wt/alpha", Upstream: "origin/main"},
		{Name: "beta", Repo: "beta", Branch: "feat/x", Path: "/wt/beta", Upstream: "origin/main", Dirty: true},
		{Name: "gamma", Repo: "gamma", Branch: "stage", Path: "/wt/gamma", Upstream: "origin/main", Behind: 1},
	}
}

func stubLoader(rows []Row, asOf string, err error) Loader {
	return func() ([]Row, string, error) {
		return rows, asOf, err
	}
}

func loadModel(t *testing.T, m tea.Model, rows []Row, asOf string, err error) tea.Model {
	t.Helper()
	next, _ := m.Update(loadedMsg{rows: rows, asOf: asOf, err: err})
	return next
}

func keyModel(t *testing.T, m tea.Model, key tea.KeyMsg) tea.Model {
	t.Helper()
	next, _ := m.Update(key)
	return next
}

func asModel(t *testing.T, m tea.Model) *model {
	t.Helper()
	mm, ok := m.(*model)
	if !ok {
		t.Fatalf("model type = %T, want *model", m)
	}
	return mm
}

func TestNewOpensWithInitialFilter(t *testing.T) {
	m := New("demo", stubLoader(nil, "", nil), State{Filter: "dirty"})
	mm := asModel(t, m)
	if mm.filter != "dirty" {
		t.Fatalf("filter = %q, want dirty", mm.filter)
	}
}

func TestInitReloadsRows(t *testing.T) {
	rows := sampleRows()
	m := New("demo", stubLoader(rows, "2026-08-14", nil), State{})
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init returned nil command")
	}
	m = loadModel(t, m, rows, "2026-08-14", nil)
	mm := asModel(t, m)
	if !mm.loaded || len(mm.rows) != 3 {
		t.Fatalf("loaded = %v rows = %d, want loaded with 3 rows", mm.loaded, len(mm.rows))
	}
	if mm.asOf != "2026-08-14" {
		t.Errorf("asOf = %q, want dated footer input", mm.asOf)
	}
	if mm.summary.Total != 3 {
		t.Errorf("summary total = %d, want 3", mm.summary.Total)
	}
}

func TestLoadedErrorIsRetained(t *testing.T) {
	m := New("demo", stubLoader(nil, "", errors.New("git failed")), State{})
	m = loadModel(t, m, nil, "", errors.New("git failed"))
	mm := asModel(t, m)
	if mm.err == nil {
		t.Fatal("loader error was dropped")
	}
	if mm.loaded {
		t.Error("a failed load must not mark the board loaded")
	}
}

func TestBrowseNavigationMovesSelection(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	mm := asModel(t, m)
	if mm.cursor != 1 {
		t.Fatalf("cursor = %d after j, want 1", mm.cursor)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	mm = asModel(t, m)
	if mm.cursor != 0 {
		t.Fatalf("cursor = %d after k, want 0", mm.cursor)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	mm = asModel(t, m)
	if mm.cursor != len(mm.view)-1 {
		t.Fatalf("cursor = %d after end, want last row", mm.cursor)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyHome})
	mm = asModel(t, m)
	if mm.cursor != 0 || mm.top != 0 {
		t.Fatalf("cursor/top = (%d,%d) after home, want (0,0)", mm.cursor, mm.top)
	}
}

func TestBrowsePageKeysScrollByHalfViewport(t *testing.T) {
	rows := make([]Row, 20)
	for i := range rows {
		rows[i] = Row{Name: string(rune('a' + i)), Repo: "r", Branch: "main", Path: "/wt", Upstream: "origin/main"}
	}
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)
	mm := asModel(t, m)
	mm.height = 20
	half := mm.rowsVisible() / 2

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	mm = asModel(t, m)
	if mm.cursor != half {
		t.Fatalf("cursor = %d after ctrl+d, want %d", mm.cursor, half)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlU})
	mm = asModel(t, m)
	if mm.cursor != 0 {
		t.Fatalf("cursor = %d after ctrl+u, want 0", mm.cursor)
	}
}

func TestEnterChoosesCurrentRow(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	got, ok := Chosen(m)
	if !ok {
		t.Fatal("enter did not report a selection")
	}
	if got.Name != "beta" || got.Path != "/wt/beta" {
		t.Fatalf("chosen = %+v, want beta worktree", got)
	}
}

func TestQuitWithoutSelectionReportsNone(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	mm := asModel(t, m)
	if !mm.quit {
		t.Fatal("q must set quit")
	}
	if _, ok := Chosen(m); ok {
		t.Fatal("quit without enter must not report a selection")
	}
}

func TestDirtyShortcutAppliesFilter(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	mm := asModel(t, m)
	if mm.filter != "dirty" || len(mm.view) != 1 {
		t.Fatalf("filter = %q view = %v, want dirty with one row", mm.filter, mm.view)
	}
}

func TestRefreshReloadsFromLoader(t *testing.T) {
	call := 0
	loader := func() ([]Row, string, error) {
		call++
		if call == 1 {
			return sampleRows(), "first", nil
		}
		return []Row{{Name: "fresh", Repo: "fresh", Branch: "main", Path: "/wt/fresh", Upstream: "origin/main"}}, "second", nil
	}
	m := New("demo", loader, State{})
	m = loadModel(t, m, sampleRows(), "first", nil)
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	mm := asModel(t, m)
	if mm.status != "re-reading git…" {
		t.Fatalf("status = %q, want reload banner", mm.status)
	}
	if cmd := mm.reload(); cmd == nil {
		t.Fatal("reload returned nil")
	}
	m = loadModel(t, m, []Row{{Name: "fresh", Repo: "fresh", Branch: "main", Path: "/wt/fresh", Upstream: "origin/main"}}, "second", nil)
	mm = asModel(t, m)
	if len(mm.rows) != 1 || mm.asOf != "second" {
		t.Fatalf("after refresh rows = %+v asOf = %q", mm.rows, mm.asOf)
	}
}

func TestFilterModeEditsAndRestoresBrowse(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	mm := asModel(t, m)
	if mm.mode != modeFilter {
		t.Fatal("/ must enter filter mode")
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b', 'e', 't'}})
	mm = asModel(t, m)
	if mm.filter != "bet" || len(mm.view) != 1 {
		t.Fatalf("typed filter = %q view = %v, want beta only", mm.filter, mm.view)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	mm = asModel(t, m)
	if mm.filter != "be" {
		t.Fatalf("backspace left filter = %q", mm.filter)
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	mm = asModel(t, m)
	if mm.mode != modeBrowse {
		t.Fatal("enter must return to browse mode")
	}

	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = keyModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	mm = asModel(t, m)
	if mm.filter != "" || mm.mode != modeBrowse {
		t.Fatalf("esc in filter mode must clear filter, got filter=%q mode=%v", mm.filter, mm.mode)
	}
}

func TestWindowSizeClampsCursor(t *testing.T) {
	rows := sampleRows()
	m := loadModel(t, New("demo", stubLoader(rows, "", nil), State{}), rows, "", nil)
	mm := asModel(t, m)
	mm.cursor = 2
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})
	mm = asModel(t, m)
	if mm.width != 80 || mm.height != 10 {
		t.Fatalf("size = (%d,%d), want (80,10)", mm.width, mm.height)
	}
	if mm.cursor != 2 {
		t.Fatalf("cursor = %d, want to stay on last row", mm.cursor)
	}
}

func TestReloadInvokesLoader(t *testing.T) {
	called := false
	m := New("demo", func() ([]Row, string, error) {
		called = true
		return sampleRows(), "now", nil
	}, State{})
	cmd := asModel(t, m).reload()
	if cmd == nil {
		t.Fatal("reload returned nil")
	}
	msg := cmd()
	if !called {
		t.Fatal("loader was not invoked")
	}
	loaded, ok := msg.(loadedMsg)
	if !ok {
		t.Fatalf("msg = %T, want loadedMsg", msg)
	}
	if len(loaded.rows) != 3 || loaded.asOf != "now" {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestChosenRejectsForeignModels(t *testing.T) {
	if _, ok := Chosen(tea.Model(nil)); ok {
		t.Fatal("nil model must not report a selection")
	}
}

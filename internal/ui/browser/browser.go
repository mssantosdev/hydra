// Package browser is the interactive reporting surface: a full-screen board of every
// worktree in a workspace, with filtering and selection.
//
// It exists because the interactive surface had a hole. Eight mutating flows (add, sync,
// remove, switch, new, config, topic, repo add) ship huh forms, but every REPORTING command
// — list, status, where, doctor, topic show, run — was flags-only, so a human exploring a
// workspace had to already know the flag that would answer their question. A form cannot
// close that gap: forms collect values and exit, and reporting needs view, refine, and act
// in one place.
//
// Two rules carry over from the rest of hydra and are not negotiable here:
//
//   - Nothing is cached. Every refresh re-reads git, because a board that renders stale
//     state is worse than no board. The rows are recomputed, never patched.
//   - `behind` stays dated. The footer carries the fetch timestamp the rows were computed
//     against, since hydra never fetches to answer a query.
package browser

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// AgainstInfo answers "where is this worktree relative to REF", mirroring againstJSON
// from internal/cmd.
type AgainstInfo struct {
	Ref    string
	Ahead  int
	Behind int
	Merged bool
}

// Row is one worktree as the board displays it. The caller builds these from the same
// collectors the `status` command uses, so the two views cannot disagree.
type Row struct {
	Project  string // always set; used for headers in --all mode
	Group    string
	Repo     string
	Name     string
	Branch   string
	Path     string
	Upstream string // "local-only" when unpublished
	Topic    string // "" when unassigned, which is a permanent first-class state
	Ahead    int
	Behind   int
	Dirty    bool
	Changes  int
	Detached bool
	Against  *AgainstInfo // nil when --against was not passed
}

// Counts is the seven summary counters status emits; the board computes them from rows.
type Counts struct {
	Total     int
	Clean     int
	Dirty     int
	Ahead     int
	Behind    int
	LocalOnly int
	Detached  int
}

// Loader recomputes the board from disk. It is called on entry and on every refresh
// rather than once, so the view can never drift from git.

// State is the board's initial scope. Selectors from the invoking command are
// mapped here so the board opens pre-filtered rather than showing everything.
type State struct {
	Filter string
}

type Loader func() ([]Row, string, error)

// Result is what the browser hands back when it exits. Path is empty unless the user
// selected a worktree, which the caller turns into the same answer `hydra switch` gives.
type Result struct {
	Path string
	Name string
}

type mode int

const (
	modeBrowse mode = iota
	modeFilter
)

type model struct {
	load    Loader
	rows    []Row
	view    []int // indices into rows, after filtering
	cursor  int
	top     int // first visible row, for scrolling
	filter  string
	mode    mode
	asOf    string
	err     error
	status  string
	width   int
	height  int
	chosen  *Result
	quit    bool
	project string
	loaded  bool
	summary Counts
}

// New builds the browser over a loader. The first load happens in Init so a slow git read
// cannot block the terminal from switching to the alternate screen.
func New(project string, load Loader, initial State) tea.Model {
	return &model{load: load, project: project, filter: initial.Filter, width: 100, height: 30}
}

type loadedMsg struct {
	rows []Row
	asOf string
	err  error
}

func (m *model) Init() tea.Cmd { return m.reload() }

func (m *model) reload() tea.Cmd {
	return func() tea.Msg {
		rows, asOf, err := m.load()
		return loadedMsg{rows: rows, asOf: asOf, err: err}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clampCursor()
		return m, nil

	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.loaded = true
		m.rows = msg.rows
		m.asOf = msg.asOf
		sort.SliceStable(m.rows, func(i, j int) bool {
			if m.rows[i].Project != m.rows[j].Project {
				return m.rows[i].Project < m.rows[j].Project
			}
			if m.rows[i].Group != m.rows[j].Group {
				return m.rows[i].Group < m.rows[j].Group
			}
			return m.rows[i].Name < m.rows[j].Name
		})
		m.summary = summarizeRows(m.rows)
		m.applyFilter()
		m.status = fmt.Sprintf("%d worktree(s)", len(m.rows))
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeFilter {
			return m.updateFilter(msg)
		}
		return m.updateBrowse(msg)
	}
	return m, nil
}

func (m *model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.mode = modeBrowse
		m.filter = ""
		m.applyFilter()
	case "enter":
		m.mode = modeBrowse
	case "backspace":
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.applyFilter()
		}
	default:
		if r := msg.Runes; len(r) > 0 {
			m.filter += string(r)
			m.applyFilter()
		}
	}
	return m, nil
}

func (m *model) updateBrowse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		m.quit = true
		return m, tea.Quit
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		m.cursor, m.top = 0, 0
	case "G", "end":
		m.cursor = len(m.view) - 1
		m.clampCursor()
	case "ctrl+d", "pgdown":
		m.move(m.rowsVisible() / 2)
	case "ctrl+u", "pgup":
		m.move(-m.rowsVisible() / 2)
	case "/":
		m.mode = modeFilter
	case "r":
		m.status = "re-reading git…"
		return m, m.reload()
	case "d":
		// Dirty is the filter a human reaches for most; a shortcut beats retyping it.
		m.filter = "dirty"
		m.applyFilter()
	case "y":
		if row, ok := m.current(); ok {
			if err := clipboard.WriteAll(row.Path); err != nil {
				m.status = fmt.Sprintf("clipboard unavailable: %v", err)
			} else {
				m.status = "copied " + row.Name
			}
		}
	case "enter":
		if row, ok := m.current(); ok {
			m.chosen = &Result{Path: row.Path, Name: row.Name}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *model) move(delta int) {
	if len(m.view) == 0 {
		return
	}
	m.cursor += delta
	m.clampCursor()
}

func (m *model) clampCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(m.view)-1 {
		m.cursor = len(m.view) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	vis := m.rowsVisible()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+vis {
		m.top = m.cursor - vis + 1
	}
	if m.top < 0 {
		m.top = 0
	}
}

// rowsVisible is the table body height: total minus masthead, header and footer chrome.
func (m *model) rowsVisible() int {
	n := m.height - 8
	if n < 3 {
		return 3
	}
	return n
}

func (m *model) current() (Row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.view) {
		return Row{}, false
	}
	return m.rows[m.view[m.cursor]], true
}

// summarizeRows mirrors summarizeStatus in internal/cmd/status.go.
func summarizeRows(items []Row) Counts {
	var summary Counts
	summary.Total = len(items)
	for _, item := range items {
		if item.Detached {
			summary.Detached++
		}
		if item.Upstream == "local-only" && !item.Detached {
			summary.LocalOnly++
		}
		if item.Dirty {
			summary.Dirty++
		}
		if item.Ahead > 0 {
			summary.Ahead++
		}
		if item.Behind > 0 {
			summary.Behind++
		}
		if !item.Dirty && item.Ahead == 0 && item.Behind == 0 && !item.Detached && item.Upstream != "local-only" {
			summary.Clean++
		}
	}
	return summary
}

// applyFilter keeps the filter vocabulary identical to `--filter` on the non-interactive
// commands, because a human who learns a word here will type it into a script tomorrow.
// The CLI accepts exactly `dirty`, `behind` and `branch:<glob>`; anything else falls
// through to a substring search over the columns on screen. `topic:<id>` is included
// because `--topic` exists as its own flag, so the mapping is unambiguous.
//
// `ahead` is deliberately NOT a state word: the CLI rejects it, and accepting it here
// would teach a vocabulary that does not transfer.
func (m *model) applyFilter() {
	m.view = m.view[:0]
	q := strings.ToLower(strings.TrimSpace(m.filter))
	for i, r := range m.rows {
		if matches(r, q) {
			m.view = append(m.view, i)
		}
	}
	m.clampCursor()
}

func matches(r Row, q string) bool {
	switch q {
	case "":
		return true
	case "dirty":
		return r.Dirty
	case "behind":
		return r.Behind > 0
	}
	if glob, ok := cutPrefix(q, "branch:"); ok {
		return glob != "" && globMatch(glob, strings.ToLower(r.Branch))
	}
	if want, ok := cutPrefix(q, "topic:"); ok {
		return want != "" && strings.Contains(strings.ToLower(r.Topic), want)
	}
	hay := strings.ToLower(strings.Join([]string{r.Project, r.Repo, r.Branch, r.Name, r.Topic, r.Group}, " "))
	return strings.Contains(hay, q)
}

func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return strings.TrimPrefix(s, prefix), true
	}
	return "", false
}

// globMatch delegates to the same path.Match call `--filter branch:<glob>` makes in
// internal/cmd/resolve.go. That is the point: `*` does not cross a `/`, so `branch:feat/*`
// matches a namespaced branch and `branch:feat*` deliberately does not. A hand-rolled
// matcher here diverged on exactly that case.
func globMatch(pattern, s string) bool {
	ok, err := path.Match(pattern, s)
	return err == nil && ok
}

// Chosen reports the selection, if the user made one rather than quitting.
func Chosen(m tea.Model) (Result, bool) {
	mm, ok := m.(*model)
	if !ok || mm.chosen == nil {
		return Result{}, false
	}
	return *mm.chosen, true
}

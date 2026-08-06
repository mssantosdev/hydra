package browser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// The board on screen. Column widths are computed from the real content rather than
// fixed, because repo and branch names vary wildly between workspaces and a truncated
// branch name is the one thing a caller cannot afford to misread.
func (m *model) View() string {
	if m.quit || m.chosen != nil {
		return ""
	}

	// Read the RESOLVED colours from styles.*, not a package-default theme initialiser.
	// loadTheme() never synced the old global; only the interactive `config` form wrote it,
	// inside its own process. Reading it here made this the one view that ignored the
	// user's theme, so it rendered a different palette than `list` and `status`.
	var (
		dim    = lipgloss.NewStyle().Foreground(styles.FgComment)
		hi     = lipgloss.NewStyle().Foreground(styles.FgBright).Bold(true)
		lbl    = lipgloss.NewStyle().Foreground(styles.FgComment)
		accent = lipgloss.NewStyle().Foreground(styles.Blue)
		okS    = lipgloss.NewStyle().Foreground(styles.Green)
		warnS  = lipgloss.NewStyle().Foreground(styles.Yellow)
		errS   = lipgloss.NewStyle().Foreground(styles.Red)
		topicS = lipgloss.NewStyle().Foreground(styles.Purple)
		// Reverse video for the cursor, so the selection uses the reader's own
		// background instead of a hex this program does not own.
		selS = lipgloss.NewStyle().Reverse(true)
		rule = lipgloss.NewStyle().Foreground(styles.FgComment)
	)

	var b strings.Builder

	// masthead: what workspace, and how fresh the numbers are
	title := hi.Render("hydra") + dim.Render(" · worktrees")
	right := dim.Render(m.mastheadRight())
	b.WriteString(m.spread(title, right) + "\n")
	b.WriteString(rule.Render(strings.Repeat("─", m.clampWidth())) + "\n")

	if m.err != nil {
		b.WriteString(errS.Render("cannot read the workspace: "+m.err.Error()) + "\n")
		b.WriteString(dim.Render("r retry · q quit") + "\n")
		return b.String()
	}

	if !m.loaded {
		b.WriteString(dim.Render("reading git…") + "\n")
		return b.String()
	}

	if len(m.rows) == 0 {
		b.WriteString(dim.Render("no worktrees in this project") + "\n\n")
		b.WriteString(dim.Render("hydra repo add <url> --group <group>") + "\n")
		b.WriteString(dim.Render("q quit") + "\n")
		return b.String()
	}

	showAgainst, againstRef := m.againstColumn()
	wName, wBranch, wUp, wTopic, wAgainst := m.columnWidths(showAgainst)

	head := fmt.Sprintf("  %-*s  %-*s  %-*s  %-*s", wName, "WORKTREE", wBranch, "BRANCH", wUp, "UPSTREAM", wTopic, "TOPIC")
	if showAgainst {
		head += fmt.Sprintf("  %-*s", wAgainst, strings.ToUpper(againstRef))
	}
	head += "  STATUS"
	b.WriteString(lbl.Render(head) + "\n")

	if len(m.view) == 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  nothing matches %q", m.filter)) + "\n")
	}

	multi := m.multiProject()
	end := min(m.top+m.rowsVisible(), len(m.view))
	var prevProject, prevGroup string
	for vi := m.top; vi < end; vi++ {
		r := m.rows[m.view[vi]]

		if multi && r.Project != prevProject {
			b.WriteString(dim.Render("▸ "+strings.ToUpper(r.Project)) + "\n")
			prevProject = r.Project
			prevGroup = ""
		}
		if r.Group != prevGroup {
			b.WriteString(dim.Render("▸ "+strings.ToUpper(r.Group)) + "\n")
			prevGroup = r.Group
		}

		name := padRight(truncate(r.Name, wName), wName)
		branch := padRight(displayBranch(r), wBranch)
		up := padRight(r.Upstream, wUp)
		tp := r.Topic
		if tp == "" {
			tp = "—"
		}
		tp = padRight(tp, wTopic)

		state := "✓ clean"
		stStyle := okS
		switch {
		case r.Detached:
			state, stStyle = "detached", warnS
		case r.Dirty:
			state, stStyle = fmt.Sprintf("~ %d chg", r.Changes), warnS
		}
		st := stStyle.Render(state)
		if r.Behind > 0 {
			st += dim.Render(fmt.Sprintf("  ↓%d", r.Behind))
		}
		if r.Ahead > 0 {
			st += dim.Render(fmt.Sprintf("  ↑%d", r.Ahead))
		}

		topicCell := dim.Render(tp)
		if r.Topic != "" {
			topicCell = topicS.Render(tp)
		}

		gutter := "  "
		nameCell := name
		if vi == m.cursor {
			gutter = selS.Render("\u25b8") + " "
			nameCell = hi.Render(name)
		}

		cells := []string{nameCell, accent.Render(branch), dim.Render(up), topicCell}
		if showAgainst {
			cells = append(cells, displayAgainst(r, okS, warnS, dim, wAgainst))
		}
		cells = append(cells, st)
		b.WriteString(gutter + strings.Join(cells, "  ") + "\n")
	}

	// footer: position, freshness, keys
	b.WriteString(rule.Render(strings.Repeat("─", m.clampWidth())) + "\n")

	pos := fmt.Sprintf("%d/%d", minInt(m.cursor+1, len(m.view)), len(m.view))
	fresh := "never fetched"
	if m.asOf != "" {
		fresh = "as of " + m.asOf
	}
	b.WriteString(m.spread(dim.Render(pos+"  "+m.status), dim.Render(fresh)) + "\n")

	if m.mode == modeFilter {
		b.WriteString(hi.Render("/"+m.filter) + dim.Render("▏  enter accept · esc clear") + "\n")
	} else {
		keys := []string{"↑↓ move", "enter print path", "y copy", "/ filter", "d dirty", "r refresh", "q quit"}
		if m.filter != "" {
			keys = append([]string{"filter " + hi.Render(m.filter)}, keys...)
		}
		b.WriteString(dim.Render(strings.Join(keys, " · ")) + "\n")
	}
	return b.String()
}

func (m *model) mastheadRight() string {
	crumb := m.breadcrumb()
	counts := renderCounts(m.summary)
	if crumb == "" {
		return counts
	}
	return crumb + "  " + counts
}

func (m *model) breadcrumb() string {
	if m.project == "" {
		return ""
	}
	groups := map[string]struct{}{}
	for _, i := range m.view {
		groups[m.rows[i].Group] = struct{}{}
	}
	if len(groups) == 1 {
		for g := range groups {
			return m.project + " › " + g
		}
	}
	return m.project
}

func renderCounts(s Counts) string {
	lbl := lipgloss.NewStyle().Foreground(styles.FgComment)
	warn := lipgloss.NewStyle().Foreground(styles.Yellow)
	dim := lipgloss.NewStyle().Foreground(styles.FgComment)

	segment := func(label string, n int, warnCount bool) string {
		num := strconv.Itoa(n)
		if n == 0 {
			return dim.Render(label + " " + num)
		}
		if warnCount {
			return lbl.Render(label) + " " + warn.Render(num)
		}
		return lbl.Render(label) + " " + num
	}

	return strings.Join([]string{
		segment("TOTAL", s.Total, false),
		segment("CLEAN", s.Clean, false),
		segment("DIRTY", s.Dirty, true),
		segment("AHEAD", s.Ahead, false),
		segment("BEHIND", s.Behind, true),
		segment("LOCAL", s.LocalOnly, false),
		segment("DETACHED", s.Detached, true),
	}, "  ")
}

func (m *model) multiProject() bool {
	if len(m.rows) == 0 {
		return false
	}
	p := m.rows[0].Project
	for _, r := range m.rows[1:] {
		if r.Project != p {
			return true
		}
	}
	return false
}

func (m *model) againstColumn() (bool, string) {
	ref := ""
	for _, i := range m.view {
		if a := m.rows[i].Against; a != nil {
			if ref == "" {
				ref = a.Ref
			}
			return true, ref
		}
	}
	for _, r := range m.rows {
		if a := r.Against; a != nil {
			if ref == "" {
				ref = a.Ref
			}
			return true, ref
		}
	}
	return false, ref
}

func (m *model) columnWidths(showAgainst bool) (wName, wBranch, wUp, wTopic, wAgainst int) {
	wName, wBranch, wUp, wTopic, wAgainst = 8, 6, 8, 5, 6
	for _, i := range m.view {
		r := m.rows[i]
		wName = max(wName, len(r.Name))
		wBranch = max(wBranch, len(displayBranch(r)))
		wUp = max(wUp, len(r.Upstream))
		wTopic = max(wTopic, len(r.Topic))
		if showAgainst && r.Against != nil {
			wAgainst = max(wAgainst, len(displayAgainstPlain(r)))
		}
	}
	extra := 14 + 4*2
	if showAgainst {
		extra += wAgainst + 2
	}
	budget := m.clampWidth() - (wBranch + wUp + wTopic + extra)
	if budget < 12 {
		budget = 12
	}
	wName = min(wName, budget)
	return wName, wBranch, wUp, wTopic, wAgainst
}

func displayAgainst(r Row, okS, warnS, dim lipgloss.Style, width int) string {
	if r.Against == nil {
		return padRight("—", width)
	}
	return padRight(displayAgainstStyled(r, okS, warnS, dim), width)
}

func displayAgainstPlain(r Row) string {
	if r.Against == nil {
		return "—"
	}
	a := r.Against
	word := "unmerged"
	if a.Merged {
		word = "merged"
	}
	return fmt.Sprintf("↑%d ↓%d %s", a.Ahead, a.Behind, word)
}

func displayAgainstStyled(r Row, okS, warnS, dim lipgloss.Style) string {
	if r.Against == nil {
		return "—"
	}
	a := r.Against
	arrows := dim.Render(fmt.Sprintf("↑%d ↓%d", a.Ahead, a.Behind))
	if a.Merged {
		return arrows + " " + okS.Render("merged")
	}
	return arrows + " " + warnS.Render("unmerged")
}

// displayBranch keeps a detached HEAD legible rather than blank; the CLI shows the same.
func displayBranch(r Row) string {
	if r.Detached {
		return "(detached)"
	}
	if r.Branch == "" {
		return "—"
	}
	return r.Branch
}

func (m *model) clampWidth() int {
	if m.width < 40 {
		return 40
	}
	if m.width > 200 {
		return 200
	}
	return m.width
}

func (m *model) spread(left, right string) string {
	gap := m.clampWidth() - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

func truncate(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func padRight(s string, n int) string {
	if lipgloss.Width(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-lipgloss.Width(s))
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minInt(a, b int) int { return min(a, b) }

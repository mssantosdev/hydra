package browser

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mssantosdev/hydra/internal/ui/themes"
)

// The register on screen. Column widths are computed from the real content rather than
// fixed, because repo and branch names vary wildly between workspaces and a truncated
// branch name is the one thing a caller cannot afford to misread.
func (m *model) View() string {
	if m.quit || m.chosen != nil {
		return ""
	}

	t := themes.Current
	var (
		dim    = lipgloss.NewStyle().Foreground(t.Muted)
		hi     = lipgloss.NewStyle().Foreground(t.Highlight).Bold(true)
		lbl    = lipgloss.NewStyle().Foreground(t.Muted)
		accent = lipgloss.NewStyle().Foreground(t.Primary)
		okS    = lipgloss.NewStyle().Foreground(t.Success)
		warnS  = lipgloss.NewStyle().Foreground(t.Warning)
		errS   = lipgloss.NewStyle().Foreground(t.Error)
		topicS = lipgloss.NewStyle().Foreground(t.Secondary)
		selS   = lipgloss.NewStyle().Foreground(t.Highlight).Background(t.Border).Bold(true)
		rule   = lipgloss.NewStyle().Foreground(t.Border)
	)

	var b strings.Builder

	// masthead: what workspace, and how fresh the numbers are
	title := hi.Render("HYDRA") + dim.Render("  register")
	right := dim.Render(m.project)
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
		b.WriteString(dim.Render("no worktrees registered in this project") + "\n\n")
		b.WriteString(dim.Render("hydra repo add <url> --group <group>") + "\n")
		b.WriteString(dim.Render("q quit") + "\n")
		return b.String()
	}

	// column widths from the visible slice
	wName, wBranch, wUp, wTopic := 8, 6, 8, 5
	for _, i := range m.view {
		r := m.rows[i]
		wName = max(wName, len(r.Name))
		wBranch = max(wBranch, len(displayBranch(r)))
		wUp = max(wUp, len(r.Upstream))
		wTopic = max(wTopic, len(r.Topic))
	}
	budget := m.clampWidth() - (wBranch + wUp + wTopic + 14 + 4*2)
	if budget < 12 {
		budget = 12
	}
	wName = min(wName, budget)

	head := fmt.Sprintf("%-*s  %-*s  %-*s  %-*s  %s",
		wName, "WORKTREE", wBranch, "BRANCH", wUp, "UPSTREAM", wTopic, "TOPIC", "STATUS")
	b.WriteString(lbl.Render(head) + "\n")

	if len(m.view) == 0 {
		b.WriteString(dim.Render(fmt.Sprintf("  nothing matches %q", m.filter)) + "\n")
	}

	end := min(m.top+m.rowsVisible(), len(m.view))
	for vi := m.top; vi < end; vi++ {
		r := m.rows[m.view[vi]]

		// Pad the plain text to width FIRST, then style. Padding an already-styled
		// string means %-*s counts escape bytes as characters and every column after
		// the first colour drifts.
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

		if vi == m.cursor {
			plain := strings.Join([]string{name, branch, up, tp, state}, "  ")
			b.WriteString(selS.Render(padRight(plain, m.clampWidth())) + "\n")
			continue
		}

		topicCell := dim.Render(tp)
		if r.Topic != "" {
			topicCell = topicS.Render(tp)
		}
		cells := []string{name, accent.Render(branch), dim.Render(up), topicCell, st}
		b.WriteString(strings.Join(cells, "  ") + "\n")
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
		keys := []string{"↑↓ move", "enter switch", "/ filter", "d dirty", "r refresh", "q quit"}
		if m.filter != "" {
			keys = append([]string{"filter " + hi.Render(m.filter)}, keys...)
		}
		b.WriteString(dim.Render(strings.Join(keys, " · ")) + "\n")
	}
	return b.String()
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

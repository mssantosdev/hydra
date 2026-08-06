package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

// worktreeTableOpts selects the optional columns.
//
// Columns are opt-in per command rather than always present because an empty column
// costs width that the branch and path columns need more, and hydra is routinely run
// in narrow panes.
type worktreeTableOpts struct {
	IncludeUpstream bool
	IncludeTopic    bool
	// IncludeAgainst is set when --against was requested. The column would be blank
	// otherwise, so it is not shown.
	IncludeAgainst bool
}

// worktreeTable renders worktrees as one aligned table.
//
// This replaces two divergent hand-rolled layouts: list computed fixed column widths
// and re-derived them for EVERY row, then drew a separator whose width was the
// literal fudge `nameWidth + branchWidth + 10 + 4`; status did not align at all and
// simply printed space-joined fields. lipgloss/table computes column widths from the
// content once, which is both correct and less code — and it ships in the lipgloss
// version already pinned, so this costs no new dependency.
//
// Badges are pre-styled cells: StyleFunc handles padding and alignment, and the cell
// keeps whatever colour the badge already carried.
func worktreeTable(width int, items []worktreeJSON, opts worktreeTableOpts) string {
	headers := []string{"WORKTREE", "BRANCH"}
	if opts.IncludeUpstream {
		headers = append(headers, "UPSTREAM")
	}
	if opts.IncludeTopic {
		headers = append(headers, "TOPIC")
	}
	if opts.IncludeAgainst {
		headers = append(headers, "VS REF")
	}
	headers = append(headers, "STATUS")

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := []string{item.Name, branchLabelJSON(item)}
		if opts.IncludeUpstream {
			row = append(row, upstreamLabelJSON(item))
		}
		if opts.IncludeTopic {
			row = append(row, topicLabelJSON(item))
		}
		if opts.IncludeAgainst {
			row = append(row, againstLabelJSON(item))
		}
		status := lipgloss.NewStyle().Foreground(styles.Green).Render("  ✓ clean  ")
		if item.Dirty {
			status = lipgloss.NewStyle().Foreground(styles.Yellow).Render(fmt.Sprintf(" ~ %d chg  ", item.Changes))
		}
		row = append(row, status)
		rows = append(rows, row)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Blue).Padding(0, 1)
	cellStyle := lipgloss.NewStyle().Padding(0, 1)

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(styles.FgComment)).
		// Only the header rule is drawn. Full borders on a listing of a dozen
		// worktrees are noise, and the alignment is what was actually missing.
		BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).
		BorderColumn(false).BorderRow(false).BorderHeader(true).
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			return cellStyle
		}).
		// Wrap is OFF so an over-long cell truncates instead of spilling onto a second
		// line. One row per worktree is what makes the table scannable, and a wrapped
		// row silently breaks that.
		Wrap(false).
		Headers(headers...).
		Rows(rows...)

	if width > 0 {
		t = t.Width(width)
	}
	return t.String()
}

// topicLabelJSON renders membership for a table cell. Unassigned is shown as a dash
// rather than left blank, so an empty cell is never ambiguous with a missing value.
func topicLabelJSON(item worktreeJSON) string {
	if item.Topic == nil {
		return "—"
	}
	return *item.Topic
}

// againstLabelJSON renders the --against comparison.
func againstLabelJSON(item worktreeJSON) string {
	if item.Against == nil {
		// The ref did not resolve here; the warning already said so.
		return "—"
	}
	if item.Against.Merged {
		return lipgloss.NewStyle().Foreground(styles.Green).Render("merged")
	}
	return lipgloss.NewStyle().Foreground(styles.Yellow).Render(fmt.Sprintf("+%d", item.Against.Ahead))
}

// tableWidth is the usable width for a full-bleed element.
func tableWidth() int {
	width := styles.GetTerminalWidth() - 4
	if width < 40 {
		// Below this the table degrades into unreadable wrapping; letting it size to
		// content is better than forcing a width nothing fits in.
		return 0
	}
	return width
}

// groupLabel renders a group heading above its table.
func groupLabel(group string) string {
	return lipgloss.NewStyle().Foreground(styles.FgComment).Render("▸ " + strings.ToUpper(group))
}

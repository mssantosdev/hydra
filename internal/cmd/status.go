package cmd

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var statusAll bool

var statusCmd = &cobra.Command{
	Annotations: map[string]string{annotationRegistryFanout: "all"},
	Use:         "status",
	Short:       "Show worktree status overview",
	Long: `Display a compact overview of all worktrees and their status.

JSON DATA
  Single project: {project, root, summary: {total, clean, dirty, ahead, behind, local_only, detached}, worktrees: [...]}
  --all: {projects: [{project, root, summary, worktrees}], total}

EXIT CODES
  0  Success
  2  not_in_project, config_version_unsupported, project_unknown
  4  partial_failure`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVar(&statusAll, "all", false, "Show status across every registered project")
	registerSelectorFlags(statusCmd.Flags())
	rootCmd.AddCommand(statusCmd)
}

type statusSummaryJSON struct {
	Total     int `json:"total"`
	Clean     int `json:"clean"`
	Dirty     int `json:"dirty"`
	Ahead     int `json:"ahead"`
	Behind    int `json:"behind"`
	LocalOnly int `json:"local_only"`
	Detached  int `json:"detached"`
}

type statusProjectPayload struct {
	Project   string            `json:"project"`
	Root      string            `json:"root"`
	Summary   statusSummaryJSON `json:"summary"`
	Worktrees []worktreeJSON    `json:"worktrees"`
}

type statusAllPayload struct {
	Projects []statusProjectPayload `json:"projects"`
	Total    int                    `json:"total"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	targets, targetWarnings, err := projectTargets(statusAll)
	if err != nil {
		return err
	}

	// Validate before filtering: an unknown id is topic_unknown, never an empty
	// status board that looks like "everything is clean".
	if err := requireTopicInTargets(targets, topicFilter); err != nil {
		return err
	}

	projects, warnings, attempted, succeeded, err := collectProjectWorktrees(targets, currentSelector())
	if err != nil {
		return err
	}
	warnings = append(warnings, targetWarnings...)

	if err := checkWorktreePartialFailure(targets, targetWarnings, attempted, succeeded); err != nil {
		return err
	}

	payloads := make([]statusProjectPayload, 0, len(projects))
	grandTotal := 0
	for _, project := range projects {
		summary := summarizeStatus(project.Worktrees)
		grandTotal += summary.Total
		payloads = append(payloads, statusProjectPayload{
			Project:   project.Project,
			Root:      project.Root,
			Summary:   summary,
			Worktrees: project.Worktrees,
		})
	}

	var data any
	if statusAll {
		data = statusAllPayload{Projects: payloads, Total: grandTotal}
	} else if len(payloads) > 0 {
		data = payloads[0]
	} else {
		data = statusProjectPayload{Summary: statusSummaryJSON{}, Worktrees: []worktreeJSON{}}
	}

	return emit(cmd, statusSummaryLine(payloads, grandTotal), data, warnings, func() {
		renderStatusText(cmd, statusAll, payloads)
	})
}

// statusSummaryLine renders the one-line answer for the envelope.
//
// It reports only the states that are actionable: a caller wants to know whether
// anything needs attention, and "12 clean" says that better than six zero counts.
func statusSummaryLine(payloads []statusProjectPayload, total int) string {
	var dirty, behind, ahead, detached int
	for _, project := range payloads {
		dirty += project.Summary.Dirty
		behind += project.Summary.Behind
		ahead += project.Summary.Ahead
		detached += project.Summary.Detached
	}

	var parts []string
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirty))
	}
	if behind > 0 {
		parts = append(parts, fmt.Sprintf("%d behind", behind))
	}
	if ahead > 0 {
		parts = append(parts, fmt.Sprintf("%d ahead", ahead))
	}
	if detached > 0 {
		parts = append(parts, fmt.Sprintf("%d detached", detached))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("%d worktree(s), all clean", total)
	}
	return fmt.Sprintf("%d worktree(s): %s", total, strings.Join(parts, ", "))
}

func summarizeStatus(items []worktreeJSON) statusSummaryJSON {
	var summary statusSummaryJSON
	summary.Total = len(items)
	for _, item := range items {
		if item.Detached {
			summary.Detached++
		}
		if item.Upstream == nil && !item.Detached {
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
		if !item.Dirty && item.Ahead == 0 && item.Behind == 0 && !item.Detached && item.Upstream != nil {
			summary.Clean++
		}
	}
	return summary
}

func renderStatusText(cmd *cobra.Command, all bool, projects []statusProjectPayload) {
	out := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(out)
	headerBox := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(styles.Blue).
		Background(styles.BgDarker).
		Padding(0, 4).
		Align(lipgloss.Center).
		Width(styles.GetTerminalWidth() - 4)
	_, _ = fmt.Fprintln(out, headerBox.Render(
		lipgloss.NewStyle().Bold(true).Foreground(styles.Blue).Render("HYDRA")+"\n"+
			lipgloss.NewStyle().Foreground(styles.FgComment).Render("Status Overview")))
	_, _ = fmt.Fprintln(out)

	for _, project := range projects {
		if all {
			_, _ = fmt.Fprintf(out, "%s\n\n", styles.Label.Render(project.Project))
		}

		s := project.Summary
		stats := strings.Join([]string{
			styles.TotalBadge.Render(fmt.Sprintf("TOTAL %d", s.Total)),
			styles.CleanBadge.Render(fmt.Sprintf("CLEAN %d", s.Clean)),
			styles.ModifiedBadge.Render(fmt.Sprintf("DIRTY %d", s.Dirty)),
			styles.Label.Render(fmt.Sprintf("AHEAD %d", s.Ahead)),
			styles.Label.Render(fmt.Sprintf("BEHIND %d", s.Behind)),
			styles.Label.Render(fmt.Sprintf("LOCAL %d", s.LocalOnly)),
			styles.Label.Render(fmt.Sprintf("DETACHED %d", s.Detached)),
		}, "  ")
		_, _ = fmt.Fprintln(out, styles.StatBox.Render(stats))
		_, _ = fmt.Fprintln(out)

		if len(project.Worktrees) == 0 {
			_, _ = fmt.Fprintln(out, styles.Dimmed.Render("  No worktrees found."))
			_, _ = fmt.Fprintln(out)
			continue
		}

		_, _ = fmt.Fprintln(out, styles.Label.Render("Worktrees:"))
		for _, item := range project.Worktrees {
			line := fmt.Sprintf("  %s  %s  %s  %s",
				item.Name,
				styles.Branch.Render(branchLabelJSON(item)),
				upstreamLabelJSON(item),
				styles.StatusBadge(!item.Dirty, item.Changes),
			)
			_, _ = fmt.Fprintln(out, line)
		}
		_, _ = fmt.Fprintln(out)
	}
}

func upstreamLabelJSON(item worktreeJSON) string {
	if item.Detached {
		return "(detached)"
	}
	if item.Upstream == nil {
		return "local-only"
	}
	if item.Ahead > 0 || item.Behind > 0 {
		return fmt.Sprintf("%s +%d/-%d", *item.Upstream, item.Ahead, item.Behind)
	}
	return *item.Upstream
}

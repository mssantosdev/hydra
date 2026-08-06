package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/output"
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
	registerAgainstFlag(statusCmd.Flags())
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

	// Do not claim "all clean" while a warning says the workspace is broken. The counts
	// describe the worktrees status could INSPECT, and a registered worktree missing from
	// disk is not among them — so the summary has to say that rather than describing a
	// subset as if it were the whole.
	summaryLine := statusSummaryLine(payloads, grandTotal)
	if output.HasFault(warnings) {
		summaryLine += "; workspace has unreported problems, run hydra doctor"
	}

	// A partial outcome must come with a matching exit status, or the contradiction is
	// simply inverted: outcome partial under exit 0 is as misleading to a caller gating on
	// the exit code as outcome success under exit 4 was to one reading the envelope.
	var statusErr *output.Error
	if output.HasFault(warnings) {
		statusErr = output.Errorf(output.CodePartialFailure,
			"%d worktree(s) could not be inspected", countFaults(warnings)).
			WithDetail("warnings", warnings)
	}

	if emitErr := emitResult(cmd, output.Result{
		Summary:  summaryLine,
		Data:     data,
		Warnings: warnings,
		Next:     statusFaultNext(warnings),
		Err:      statusErr,
	}, func() {
		renderStatusText(cmd, statusAll, payloads)
	}); emitErr != nil {
		return emitErr
	}
	if statusErr != nil {
		return statusErr
	}
	return nil
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

	// status is the command that exists to show tracking, so the upstream column is
	// always on here even though list omits it.
	opts := worktreeTableOpts{
		IncludeUpstream: true,
		IncludeTopic:    topicFilter == "",
		IncludeAgainst:  againstRef != "",
	}

	for _, project := range projects {
		if all {
			_, _ = fmt.Fprintf(out, "%s\n\n", styles.Label.Render(project.Project))
		}

		_, _ = fmt.Fprintln(out, renderStatusCounts(project.Summary))
		_, _ = fmt.Fprintln(out)

		if len(project.Worktrees) == 0 {
			_, _ = fmt.Fprintln(out, styles.Dimmed.Render("  No worktrees found."))
			_, _ = fmt.Fprintln(out)
			continue
		}

		_, _ = fmt.Fprintln(out, worktreeTable(tableWidth(), project.Worktrees, opts))
		_, _ = fmt.Fprintln(out)
	}
}

// renderStatusCounts prints the seven summary counters as one plain line: labels in
// FgComment, numbers in the default foreground, and yellow only where the count
// signals something worth acting on.
func renderStatusCounts(s statusSummaryJSON) string {
	lbl := lipgloss.NewStyle().Foreground(styles.FgComment)
	warn := lipgloss.NewStyle().Foreground(styles.Yellow)

	segment := func(label string, n int, warnCount bool) string {
		num := strconv.Itoa(n)
		if n == 0 {
			return styles.Dimmed.Render(label + " " + num)
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

// statusFaultNext points at doctor when status could not inspect everything it was asked
// about, so the follow-up arrives without the caller knowing to look for it.
func statusFaultNext(warnings []string) []output.Next {
	if !output.HasFault(warnings) {
		return nil
	}
	return []output.Next{{
		Argv: []string{"hydra", "doctor", "--output", "json"},
		Why:  "diagnose the worktrees status could not inspect",
	}}
}

// countFaults counts the warnings that describe a workspace integrity problem.
func countFaults(warnings []string) int {
	n := 0
	for _, w := range warnings {
		if output.HasFault([]string{w}) {
			n++
		}
	}
	return n
}

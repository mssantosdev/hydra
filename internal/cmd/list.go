package cmd

import (
	"fmt"
	"sort"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var listAll bool

var listCmd = &cobra.Command{
	Annotations: map[string]string{annotationRegistryFanout: "all"},
	Use:         "list",
	Aliases:     []string{"ls"},
	Short:       "List all worktrees",
	Long: `Display all worktrees organized by group with their current status.

JSON DATA
  Single project: {project, root, worktrees: [worktreeJSON...], total}
  --all: {projects: [{project, root, worktrees, total}], total}

EXIT CODES
  0  Success
  2  not_in_project, config_version_unsupported, project_unknown
  4  partial_failure`,
	RunE: runList,
}

func init() {
	listCmd.Flags().BoolVar(&listAll, "all", false, "List worktrees across every registered project")
	registerSelectorFlags(listCmd.Flags())
	registerAgainstFlag(listCmd.Flags())
	rootCmd.AddCommand(listCmd)
}

type listProjectPayload struct {
	Project   string         `json:"project"`
	Root      string         `json:"root"`
	Worktrees []worktreeJSON `json:"worktrees"`
	Total     int            `json:"total"`
}

type listAllPayload struct {
	Projects []listProjectPayload `json:"projects"`
	Total    int                  `json:"total"`
}

type projectWorktrees struct {
	Project   string
	Root      string
	Worktrees []worktreeJSON
}

func runList(cmd *cobra.Command, args []string) error {
	targets, targetWarnings, err := projectTargets(listAll)
	if err != nil {
		return err
	}

	// Validate the topic BEFORE resolving. An unknown id must be topic_unknown, not
	// an empty list — "no such topic" and "that topic has no worktrees" are
	// different answers, and silently conflating them is how an agent loses work.
	//
	// Validation spans the SAME targets being listed, because with --all a topic may
	// live in any registered project; checking only the current one would reject an
	// id that genuinely exists.
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

	var data any
	total := 0
	for _, project := range projects {
		total += len(project.Worktrees)
	}

	if listAll {
		payload := listAllPayload{Total: total}
		for _, project := range projects {
			payload.Projects = append(payload.Projects, listProjectPayload{
				Project:   project.Project,
				Root:      project.Root,
				Worktrees: project.Worktrees,
				Total:     len(project.Worktrees),
			})
		}
		data = payload
	} else if len(projects) > 0 {
		project := projects[0]
		data = listProjectPayload{
			Project:   project.Project,
			Root:      project.Root,
			Worktrees: project.Worktrees,
			Total:     len(project.Worktrees),
		}
	} else {
		data = listProjectPayload{Worktrees: []worktreeJSON{}, Total: 0}
	}

	return emit(cmd, fmt.Sprintf("%d worktree(s)", total), data, warnings, func() {
		renderListText(cmd, listAll, projects)
	})
}

// collectProjectWorktrees resolves each target's worktrees through the shared
// resolver, so selector semantics cannot differ between list and status.
func collectProjectWorktrees(targets []projectTarget, sel Selector) ([]projectWorktrees, []string, int, int, error) {
	var projects []projectWorktrees
	var warnings []string
	attempted, succeeded := 0, 0

	for _, target := range targets {
		repos := allRepoContexts(target.Cfg, target.Root)
		attempted += len(repos)

		resolved, wtWarnings, repoFailures, err := resolveTargets(sessionFor(target), sel, true)
		if err != nil {
			return nil, warnings, attempted, succeeded, err
		}
		warnings = append(warnings, wtWarnings...)
		succeeded += len(repos) - repoFailures

		items := make([]worktreeJSON, 0, len(resolved))
		for _, entry := range resolved {
			items = append(items, entry.Item)
		}

		// A selector that matched nothing drops the project from the TEXT listing, so a
		// narrowed view never prints a heading with nothing under it. The JSON payload
		// still carries the project and root: blanking them made `.data.root` an empty
		// string on a zero-match query, which a caller cannot distinguish from "not in a
		// project" and cannot use to locate the workspace it just queried.
		if len(items) == 0 && !sel.empty() && !jsonMode() {
			continue
		}
		projects = append(projects, projectWorktrees{
			Project:   target.Name,
			Root:      target.Root,
			Worktrees: items,
		})
	}

	return projects, warnings, attempted, succeeded, nil
}

func checkWorktreePartialFailure(targets []projectTarget, targetWarnings []string, attempted, succeeded int) error {
	if len(targets) == 0 && len(targetWarnings) > 0 {
		return output.Errorf(output.CodePartialFailure, "every registered project failed to load")
	}
	if attempted > 0 && succeeded == 0 {
		return output.Errorf(output.CodePartialFailure, "every registered repository failed to list worktrees")
	}
	return nil
}

func branchLabelJSON(item worktreeJSON) string {
	if item.Detached {
		return "(detached)"
	}
	return item.Branch
}

func renderListText(cmd *cobra.Command, all bool, projects []projectWorktrees) {
	out := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(out)
	_, _ = fmt.Fprintln(out, hydraHeaderBox("Worktree Status"))
	_, _ = fmt.Fprintln(out)

	// Only show the columns this invocation can populate: a --topic-filtered listing
	// already knows the topic, and a blank column costs width the branch needs.
	opts := worktreeTableOpts{
		IncludeTopic:   topicFilter == "",
		IncludeAgainst: againstRef != "",
	}

	hasWorktrees := false
	for _, project := range projects {
		if all {
			_, _ = fmt.Fprintf(out, "%s\n\n", styles.Label.Render(project.Project))
		}

		groups := groupWorktrees(project.Worktrees)
		for _, group := range sortedGroupNames(groups) {
			items := groups[group]
			if len(items) == 0 {
				continue
			}
			hasWorktrees = true

			// The group header stays outside the table: lipgloss/table has no spanning
			// row, and a header per section is what makes several tables readable.
			_, _ = fmt.Fprintln(out, groupLabel(group))
			_, _ = fmt.Fprintln(out, worktreeTable(tableWidth(), items, opts))
			_, _ = fmt.Fprintln(out)
		}
	}

	if !hasWorktrees {
		_, _ = fmt.Fprintln(out, styles.Dimmed.Render("  No worktrees found."))
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, "  Run 'hydra repo add <url>' to add a repository.")
	}
}

func groupWorktrees(items []worktreeJSON) map[string][]worktreeJSON {
	groups := make(map[string][]worktreeJSON)
	for _, item := range items {
		groups[item.Group] = append(groups[item.Group], item)
	}
	return groups
}

func sortedGroupNames(groups map[string][]worktreeJSON) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

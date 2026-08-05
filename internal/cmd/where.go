package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var whereCmd = &cobra.Command{
	Use:   "where",
	Short: "Report where hydra thinks it is",
	Long: `Answer "where am I" from hydra's own point of view.

Every other command resolves the workspace root by walking up from the working directory,
and until now nothing reported the result. An agent dropped into a directory could not
tell whether it was inside a workspace, which worktree it was in, or which topic that
worktree belonged to, without inferring from paths.

Outside a workspace this is not an error: it answers "no", which is the useful answer.`,
	Args: cobra.NoArgs,
	RunE: runWhere,
}

func init() {
	rootCmd.AddCommand(whereCmd)
}

type whereJSON struct {
	InProject bool   `json:"in_project"`
	Project   string `json:"project,omitempty"`
	Root      string `json:"root,omitempty"`
	Manifest  string `json:"manifest,omitempty"`
	Cwd       string `json:"cwd"`
	// The fields below are populated only when the working directory is inside a
	// worktree, so a caller can tell "in the workspace" from "in a worktree".
	IsWorktree bool   `json:"is_worktree"`
	Group      string `json:"group,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Worktree   string `json:"worktree,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Topic      string `json:"topic,omitempty"`
}

func runWhere(cmd *cobra.Command, _ []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to resolve the working directory")
	}
	payload := whereJSON{Cwd: wd}

	// Not being in a project is an answer, not a failure: it is exactly what a caller
	// that just got dropped somewhere needs to learn, and erroring would make it
	// indistinguishable from a broken workspace.
	if cfg == nil || projectRoot == "" {
		return emitResult(cmd, output.Result{
			Summary: "not inside a hydra workspace",
			Data:    payload,
			Next: []output.Next{{
				Argv: []string{"hydra", "project", "ls", "--output", "json"},
				Why:  "list the registered workspaces and their roots",
			}},
		}, func() { printWhereText(payload) })
	}

	payload.InProject = true
	payload.Project = cfg.Project
	payload.Root = projectRoot
	payload.Manifest = projectConfigPath
	locateWorktree(&payload, wd)

	return emitResult(cmd, output.Result{
		Summary: whereSummary(payload),
		Data:    payload,
	}, func() { printWhereText(payload) })
}

// locateWorktree fills in the worktree fields when the working directory is inside one.
//
// Matching is by path prefix against the worktrees hydra already knows about, rather than
// by parsing the directory name: the name is derived from the alias and can be overridden
// with --as, so it is not reliably parseable back into a repo and branch.
func locateWorktree(payload *whereJSON, wd string) {
	items, _ := collectWorktrees(cfg, projectRoot)
	index, _ := newTopicIndex(projectRoot)

	best := ""
	for _, ctx := range items {
		item := ctx.json()
		index.decorate(&item)
		if item.Path == "" || !underPath(wd, item.Path) {
			continue
		}
		// The deepest match wins, so a nested checkout is not mistaken for its parent.
		if len(item.Path) <= len(best) {
			continue
		}
		best = item.Path
		payload.IsWorktree = true
		payload.Group = item.Group
		payload.Repo = item.Repo
		payload.Worktree = item.Name
		payload.Branch = item.Branch
		payload.Topic = ""
		if item.Topic != nil {
			payload.Topic = *item.Topic
		}
	}
}

// underPath reports whether dir is at or below root.
func underPath(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

func whereSummary(p whereJSON) string {
	if !p.IsWorktree {
		return fmt.Sprintf("in workspace %q, not inside a worktree", p.Project)
	}
	if p.Topic != "" {
		return fmt.Sprintf("%s/%s on %s, topic %s", p.Group, p.Worktree, p.Branch, p.Topic)
	}
	return fmt.Sprintf("%s/%s on %s", p.Group, p.Worktree, p.Branch)
}

func printWhereText(p whereJSON) {
	if !p.InProject {
		fmt.Println(styles.Dimmed.Render("not inside a hydra workspace"))
		fmt.Println(styles.Dimmed.Render("  cwd: " + p.Cwd))
		return
	}
	fmt.Println()
	fmt.Println(styles.Label.Render("project ") + p.Project)
	fmt.Println(styles.Label.Render("root    ") + p.Root)
	if p.IsWorktree {
		fmt.Println(styles.Label.Render("worktree") + " " + p.Group + "/" + p.Worktree)
		fmt.Println(styles.Label.Render("branch  ") + " " + p.Branch)
		if p.Topic != "" {
			fmt.Println(styles.Label.Render("topic   ") + " " + p.Topic)
		}
	}
	fmt.Println()
}

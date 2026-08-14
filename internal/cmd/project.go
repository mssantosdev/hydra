package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var projectPrune bool

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage registered hydra projects",
	Long: `Manage the global project registry that maps names to workspace roots.

DESCRIPTION
  Projects are registered in the global registry (~/.config/hydra/projects.yaml).
  Each entry points at a workspace root that must contain a schema v2 .hydra/config.yaml.

  These commands work outside any hydra workspace.

SUBCOMMANDS
  ls   List registered projects
  add  Register a workspace root
  rm   Remove a registry entry (does not delete files)

EXAMPLES
  $ hydra project ls
  $ hydra project ls --prune
  $ hydra project add my-app /path/to/workspace
  $ hydra project rm my-app

EXIT CODES
  0  Success
  1  General error
  2  not_in_project, config_version_unsupported, project_unknown`,
}

var projectLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List registered projects",
	RunE:  runProjectLs,
}

var projectAddCmd = &cobra.Command{
	Use:   "add <name> [path]",
	Short: "Register a workspace root",
	Args:  cobra.RangeArgs(1, 2),
	RunE:  runProjectAdd,
}

var projectRmCmd = &cobra.Command{
	Use:   "rm <name>",
	Short: "Remove a project from the registry",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectRm,
}

func init() {
	rootCmd.AddCommand(projectCmd)
	projectCmd.AddCommand(projectLsCmd, projectAddCmd, projectRmCmd)
	projectLsCmd.Flags().BoolVar(&projectPrune, "prune", false, "Drop registry entries whose workspace no longer has .hydra/config.yaml")
}

type projectEntry struct {
	Name   string `json:"name"`
	Root   string `json:"root"`
	Exists bool   `json:"exists"`
}

type projectLsPayload struct {
	Registry string         `json:"registry"`
	Projects []projectEntry `json:"projects"`
	Pruned   []string       `json:"pruned,omitempty"`
}

func runProjectLs(cmd *cobra.Command, args []string) error {
	reg, err := registry.Load()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to load project registry")
	}

	var pruned []string
	if projectPrune {
		pruned = reg.Prune()
		if len(pruned) > 0 {
			if err := reg.Save(); err != nil {
				return output.Wrap(output.CodeIOFailed, err, "failed to save project registry")
			}
		}
	}

	names := reg.Names()
	entries := make([]projectEntry, 0, len(names))
	for _, name := range names {
		root, _ := reg.Resolve(name)
		entries = append(entries, projectEntry{
			Name:   name,
			Root:   root,
			Exists: workspaceConfigExists(root),
		})
	}

	payload := projectLsPayload{
		Registry: registry.Path(),
		Projects: entries,
		Pruned:   pruned,
	}

	return emit(cmd, fmt.Sprintf("%d registered project(s)", len(entries)), payload, nil, func() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Registry: %s\n\n", registry.Path())
		if len(entries) == 0 {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "No registered projects.")
			return
		}

		lbl := lipgloss.NewStyle().Foreground(styles.FgComment)
		okMarker := lipgloss.NewStyle().Foreground(styles.Green)
		warnMarker := lipgloss.NewStyle().Foreground(styles.Yellow)
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-16s  %-8s  %s\n",
			lbl.Render("NAME"),
			lbl.Render("CONFIG"),
			lbl.Render("ROOT"))
		for _, entry := range entries {
			marker := okMarker.Render("ok")
			if !entry.Exists {
				marker = warnMarker.Render("!")
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "%-16s  %-8s  %s\n", entry.Name, marker, entry.Root)
		}
		if len(pruned) > 0 {
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "\nPruned: %s\n", stringsJoinSorted(pruned))
		}
	})
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	root := ""
	if len(args) > 1 {
		root = args[1]
	} else {
		wd, err := os.Getwd()
		if err != nil {
			return output.Wrap(output.CodeIOFailed, err, "failed to get current directory")
		}
		root = wd
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to resolve workspace path")
	}

	if _, err := verifyWorkspaceRoot(root); err != nil {
		return err
	}

	reg, err := registry.Load()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to load project registry")
	}

	if err := reg.Add(name, root); err != nil {
		// reg.Add fails for exactly one reason: the name is taken by a DIFFERENT root. That is
		// project_exists, not a write problem — reporting it as one sent the caller to look at
		// their disk when the answer is to choose another name. `73635b1` fixed this same
		// confusion once before, in the opposite direction.
		return output.Wrap(output.CodeProjectExists, err,
			"project %q is already registered", name).
			WithDetail("project", name).
			WithNext(output.Next{
				Argv: []string{"hydra", "project", "ls", "--output", "json"},
				Why:  "see which names are taken and where they point",
			})
	}
	if err := reg.Save(); err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to save project registry")
	}

	payload := map[string]any{
		"name": name,
		"root": root,
	}
	return emit(cmd, fmt.Sprintf("registered project %q", name), payload, nil, func() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Registered project %q at %s\n", name, root)
	})
}

func runProjectRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	reg, err := registry.Load()
	if err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to load project registry")
	}

	if _, ok := reg.Resolve(name); !ok {
		return output.Errorf(output.CodeProjectUnknown, "unknown project: %s", name)
	}

	if err := reg.Remove(name); err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to remove project")
	}
	if err := reg.Save(); err != nil {
		return output.Wrap(output.CodeIOFailed, err, "failed to save project registry")
	}

	payload := map[string]any{"name": name, "removed": true}
	return emit(cmd, fmt.Sprintf("removed project %q from the registry", name), payload, nil, func() {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed project %q from registry\n", name)
	})
}

func verifyWorkspaceRoot(root string) (*config.Config, error) {
	configPath := config.ManifestPath(root)
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			return nil, output.Errorf(output.CodeNotInProject,
				"no .hydra/config.yaml found at %s", root)
		}
		return nil, output.Wrap(output.CodeIOFailed, err, "failed to stat workspace config")
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		var unsupported *config.ErrUnsupportedVersion
		if errors.As(err, &unsupported) {
			return nil, output.Wrap(output.CodeConfigVersionUnsupported, unsupported, "%s", unsupported.Error())
		}
		return nil, output.Wrap(output.CodeIOFailed, err, "failed to load workspace config")
	}
	return loaded, nil
}

func workspaceConfigExists(root string) bool {
	_, err := os.Stat(config.ManifestPath(root))
	return err == nil
}

func stringsJoinSorted(items []string) string {
	sort.Strings(items)
	return strings.Join(items, ", ")
}

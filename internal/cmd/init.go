package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
)

var (
	initProjectName string
	initPath        string
)

type initResult struct {
	Project    string `json:"project"`
	Root       string `json:"root"`
	ConfigPath string `json:"config_path"`
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a hydra workspace in the current directory",
	Long: `Create a .hydra/config.yaml workspace configuration.

DESCRIPTION
  Writes a schema v2 .hydra/config.yaml into the current directory (or --path) and
  registers the workspace in hydra's global project registry, so it can later be
  addressed by name with --project.

  init creates an EMPTY workspace: no repos, no worktrees. Add repositories with
  "hydra clone <url>" for a remote, or "hydra adopt <path>" to import a checkout
  you already have on disk.

WHEN TO USE
  • Starting a workspace you will populate with "hydra clone"
  • Turning an existing directory into a hydra project

EXAMPLES
  # Initialize here, project name taken from the directory
  $ hydra init

  # Name the project explicitly
  $ hydra init --project-name arvia

  # Initialize somewhere else
  $ hydra init --path ~/projects/arvia

FLAGS
  --project-name <name>  registry name for this workspace (default: directory name)
  --path <dir>           directory to initialize (default: the current directory)

EXIT CODES
  0  Success
  1  General error (directory not writable, workspace already exists)

SEE ALSO
  • hydra clone   - add a remote repository to the workspace
  • hydra adopt   - import an existing local checkout
  • hydra new     - bootstrap a project and its first repo in one step
  • hydra project - manage the global project registry`,
	Args: cobra.NoArgs,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initProjectName, "project-name", "", "registry name for this workspace")
	initCmd.Flags().StringVar(&initPath, "path", "", "directory to initialize")
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	target := strings.TrimSpace(initPath)
	if target == "" {
		wd, err := os.Getwd()
		if err != nil {
			return output.Wrap(output.CodeInternal, err, "failed to resolve the working directory")
		}
		target = wd
	}

	root, configPath, created, err := createProjectRootAt(target, initProjectName)
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to initialize the workspace")
	}

	if err := registry.Register(created.Project, root); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to register project %q", created.Project)
	}

	cfg = created
	projectRoot = root
	projectConfigPath = configPath

	result := initResult{Project: created.Project, Root: root, ConfigPath: configPath}

	return emit(cmd, result, nil, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Hydra workspace initialized"))
		fmt.Printf("  Project: %s\n", result.Project)
		fmt.Printf("  Config:  %s\n", relativeTo(result.Root, result.ConfigPath))
		fmt.Printf("  Layout:  %s/<alias>.git holds git data; worktrees are siblings under <group>/\n", created.Paths.BareDir)
		fmt.Println()
		fmt.Println(styles.Label.Render("Next:"))
		fmt.Println("  hydra clone <url> --alias <alias> --group <group>")
		fmt.Println("  hydra adopt <path> --group <group>")
	})
}

// relativeTo renders a path relative to root when that is shorter to read.
func relativeTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

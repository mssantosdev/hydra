package cmd

import (
	"errors"
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
  "hydra repo add <url>" for a remote, or "hydra repo add <path> --adopt" for a checkout
  you already have on disk.

WHEN TO USE
  • Starting a workspace you will populate with "hydra repo add"
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
  • hydra repo add        - add a repository to the workspace
  • hydra repo add --adopt - track an existing local checkout
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
		// Preserve a cause that already carries a stable code. Wrapping everything as
		// `internal` told the caller hydra had broken when they had simply run init twice,
		// and `internal` is the one code an agent is expected to treat as a tool defect.
		var coded *output.Error
		if errors.As(err, &coded) {
			return coded
		}
		return output.Wrap(output.CodeInternal, err, "failed to initialize the workspace")
	}

	if err := registry.Register(created.Project, root); err != nil {
		// Roll the manifest back. Registration is the last step and the one that can fail
		// on a name collision, so leaving config.yaml behind turned a rejected init into a
		// half-made workspace: the retry with a different name then found a manifest it had
		// not created. Only the file this call wrote is removed, never the directory, which
		// may have held something of the caller's.
		if removeErr := os.Remove(configPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return output.Wrap(output.CodeInternal, err,
				"failed to register project %q, and %s was left behind: %v",
				created.Project, configPath, removeErr)
		}
		return output.Wrap(output.CodeProjectExists, err,
			"project name %q is already registered", created.Project).
			WithDetail("project", created.Project).
			WithNext(output.Next{
				Argv: []string{"hydra", "project", "ls", "--output", "json"},
				Why:  "see which names are already registered",
			})
	}

	cfg = created
	projectRoot = root
	projectConfigPath = configPath

	result := initResult{Project: created.Project, Root: root, ConfigPath: configPath}

	// Say where the registration landed. init writes to a GLOBAL registry, and nothing
	// reported which one — so a throwaway workspace silently accumulated an entry in the
	// real config, and the caller had no hint that HYDRA_CONFIG_DIR is how to avoid it.
	warnings := []string{"registered in " + registry.Path()}

	return emit(cmd, fmt.Sprintf("workspace %q initialised", result.Project), result, warnings, func() {
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Hydra workspace initialized"))
		fmt.Printf("  Project: %s\n", result.Project)
		fmt.Printf("  Config:  %s\n", relativeTo(result.Root, result.ConfigPath))
		fmt.Printf("  Layout:  %s/<alias>.git holds git data; worktrees are siblings under <group>/\n", created.Paths.BareDir)
		fmt.Println()
		fmt.Println(styles.Label.Render("Next:"))
		fmt.Println("  hydra repo add <url> --as <alias> --group <group>")
		fmt.Println("  hydra repo add <path> --adopt --group <group>")
	})
}

// relativeTo renders a path relative to root when that is shorter to read.
func relativeTo(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

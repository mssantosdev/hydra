package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var switchCmd = &cobra.Command{
	Use:   "switch [<worktree-name>]",
	Short: "Switch to a worktree",
	Long: `Switch to a different worktree by changing directory.

DESCRIPTION
  Changes to the specified worktree directory. Requires shell integration
  to be initialized (see PREREQUISITES below).

  Works by outputting a special directive (__HYDRA_CD__) that the shell
  helper intercepts and executes as a cd command.

PREREQUISITES
  Shell helper must be initialized:
    $ hydra init-shell
    $ source your shell rc/config file

WHEN TO USE
  • Moving between different feature branches
  • Switching from development to hotfix work
  • Navigating to staging/production worktrees
  • Quick context switching during code reviews

EXAMPLES
  # Interactive mode - select from list
  $ hydra switch

  # Switch by full worktree name
  $ hydra switch api-feature-login

  # Partial match (matches any *stage*)
  $ hydra switch stage

  # Quick switch using hsw alias
  $ hsw api-feature-login

EXIT CODES
  0  Success (directory changed)
  1  General error (worktree not found)
  2  Config file (.hydra.yaml) not found
  3  Shell helper not initialized

ALIASES
  hsw  Quick alias for 'hydra switch' (after init-shell)

SEE ALSO
  • hydra init-shell - Setup shell integration
  • hydra add - Create a new worktree to switch to
  • hydra list - View available worktrees
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/commands/worktree-management.md`,
	RunE: runSwitch,
}

func init() {
	rootCmd.AddCommand(switchCmd)
	switchCmd.ValidArgsFunction = completeWorktreeNames
}

func runSwitch(cmd *cobra.Command, args []string) error {
	// Check if shell helper is initialized
	if !isShellHelperInitialized() {
		fmt.Println(styles.Error.Render("Error: Shell helper not initialized"))
		fmt.Println()
		fmt.Println("To enable automatic directory switching, run:")
		fmt.Println()
		fmt.Println("  hydra init-shell")
		fmt.Println("  source your shell rc/config file")
		fmt.Println()
		fmt.Println("Then you can use: hydra switch <worktree>")
		fmt.Println()

		// Try to find the worktree to show cd command
		if len(args) > 0 {
			wd, _ := os.Getwd()
			configPath, cfg, err := config.FindConfig(wd)
			if err == nil {
				projectRoot := filepath.Dir(configPath)
				wt, ok := findWorktreeByName(cfg, projectRoot, args[0])
				if ok {
					relPath, _ := filepath.Rel(wd, wt.SymlinkPath)
					fmt.Println("For now, manually run:")
					fmt.Printf("  cd %s\n", relPath)
				}
			}
		}

		return fmt.Errorf("shell helper not initialized")
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	configPath, cfg, err := config.FindConfig(wd)
	if err != nil {
		return err
	}

	projectRoot := filepath.Dir(configPath)

	var targetPath string

	// Interactive mode if no args
	if len(args) == 0 {
		var err error
		targetPath, err = interactiveSwitch(cfg, projectRoot)
		if err != nil {
			return err
		}
	} else {
		// Find worktree by name
		wt, ok := findWorktreeByName(cfg, projectRoot, args[0])
		if !ok {
			// Try to find similar worktrees
			similar := findSimilarWorktreesByName(cfg, projectRoot, args[0])
			if len(similar) > 0 {
				fmt.Println(styles.Error.Render(fmt.Sprintf("Worktree not found: %s", args[0])))
				fmt.Println()
				fmt.Println("Did you mean:")
				for i, s := range similar {
					fmt.Printf("  %d. %s\n", i+1, s)
				}
				fmt.Println()
				fmt.Println("Create it with:")
				fmt.Printf("  hydra add <repo> %s\n", args[0])
			} else {
				return fmt.Errorf("worktree not found: %s", args[0])
			}
			return fmt.Errorf("shell helper not initialized")
		}
		targetPath = wt.WorktreePath
	}

	// Output the path for shell helper to cd to
	if err := writeSwitchTarget(targetPath); err != nil {
		return err
	}

	return nil
}

func isShellHelperInitialized() bool {
	// Check for environment variable set by init-shell
	return os.Getenv("HYDRA_SHELL_HELPER") == "1" && os.Getenv("GO_TEST") != "1"
}

func writeSwitchTarget(targetPath string) error {
	if outputFile := os.Getenv("HYDRA_SWITCH_OUTPUT_FILE"); outputFile != "" {
		return os.WriteFile(outputFile, []byte(targetPath), 0600)
	}
	fmt.Printf("__HYDRA_CD__ %s\n", targetPath)
	return nil
}

func interactiveSwitch(cfg *config.Config, projectRoot string) (string, error) {
	// Build list of available worktrees
	type worktreeItem struct {
		path  string
		label string
	}

	choices, err := collectWorktreeChoices(cfg, projectRoot)
	if err != nil {
		return "", err
	}

	items := make([]worktreeItem, 0, len(choices))
	for _, choice := range choices {
		items = append(items, worktreeItem{
			path:  choice.WorktreePath,
			label: fmt.Sprintf("%s/%s", choice.RepoContext.Ecosystem, choice.SymlinkName),
		})
	}

	if len(items) == 0 {
		return "", fmt.Errorf("no worktrees found")
	}

	// Convert to huh options
	options := make([]huh.Option[worktreeItem], len(items))
	for i, item := range items {
		options[i] = huh.NewOption(item.label, item)
	}

	var selected worktreeItem

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[worktreeItem]().
				Title("Select Worktree").
				Description("Choose which worktree to switch to").
				Options(options...).
				Value(&selected),
		),
	)

	if err := form.Run(); err != nil {
		return "", err
	}

	return selected.path, nil
}

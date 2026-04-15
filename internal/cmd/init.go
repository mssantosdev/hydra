package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize Hydra configuration",
	Long: `Create a .hydra.yaml configuration file in the current directory.

DESCRIPTION
  Initializes a new Hydra project by creating the configuration file.
  Scans for existing Git repositories and helps organize them.

  Creates:
    • .hydra.yaml - Project configuration
    • Directory structure for worktrees

  Detects Git repositories in subdirectories and offers to organize
  them into ecosystems (backend, frontend, etc.).

WHEN TO USE
  • Setting up Hydra in an existing project
  • Starting a new Hydra-managed project
  • After cloning multiple repos that need organization

EXAMPLES
  # Initialize in current directory
  $ hydra init

  # Interactive flow:
  # 1. Scans for Git repositories
  # 2. Asks to organize into ecosystems
  # 3. Creates .hydra.yaml

NEXT STEPS AFTER INIT
  $ hydra clone <url>     # Add a new repository
  $ hydra add <repo> <branch>  # Create worktrees
  $ hydra list            # View all worktrees

CONFIG FILE
  The .hydra.yaml file contains:
    • Ecosystem definitions (group aliases)
    • Repository mappings
    • Path configurations

  Edit manually or use hydra commands to update.

EXIT CODES
  0  Success (config created or already exists)
  1  General error (write failed)

SEE ALSO
  • hydra clone - Add repositories after init
  • hydra config - Manage global (not project) settings
  • Docs: https://github.com/mssantosdev/hydra/blob/main/docs/configuration.md`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	configPath := filepath.Join(wd, ".hydra.yaml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println(styles.Title.Render("Configuration already exists"))
		fmt.Printf("Found: %s\n", configPath)
		return nil
	}

	fmt.Println(styles.AppHeader.Render("HYDRA"))
	fmt.Println(styles.Title.Render("Initialize Configuration"))
	fmt.Println()

	cfg := config.DefaultConfig()
	reader := bufio.NewReader(os.Stdin)

	// Auto-detect repositories
	fmt.Println("Scanning for Git repositories...")
	repos := findGitRepos(wd)

	if len(repos) > 0 {
		fmt.Printf("Found %d Git repositories:\n", len(repos))
		for _, repo := range repos {
			fmt.Printf("  • %s\n", repo)
		}
		fmt.Println()

		fmt.Print(styles.Prompt.Render("Organize into ecosystems? [Y/n]: "))
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))

		if response == "" || response == "y" || response == "yes" {
			if err := setupEcosystems(reader, cfg, repos); err != nil {
				return err
			}
		}
	} else {
		fmt.Println("No Git repositories found in current directory.")
		fmt.Println("You can configure them manually in .hydra.yaml")
	}

	// Save config
	if err := cfg.Save(configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println()
	fmt.Println(styles.Success.Render("✓ Created .hydra.yaml"))
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  • Run 'hydra list' to see all worktrees")
	fmt.Println("  • Run 'hydra help' for all commands")
	fmt.Println("  • Edit .hydra.yaml to customize configuration")

	return nil
}

func findGitRepos(root string) []string {
	var repos []string

	entries, err := os.ReadDir(root)
	if err != nil {
		return repos
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		gitDir := filepath.Join(root, entry.Name(), ".git")
		if _, err := os.Stat(gitDir); err == nil {
			repos = append(repos, entry.Name())
		}
	}

	return repos
}

func setupEcosystems(reader *bufio.Reader, cfg *config.Config, repos []string) error {
	for len(repos) > 0 {
		fmt.Println()
		fmt.Print(styles.Prompt.Render("Ecosystem name (e.g., 'backend', 'frontend', 'services'): "))
		ecoName, _ := reader.ReadString('\n')
		ecoName = strings.TrimSpace(ecoName)

		if ecoName == "" {
			break
		}

		eco := make(config.Ecosystem)

		fmt.Printf("Assign repositories to '%s' ecosystem:\n", ecoName)
		remaining := []string{}

		for _, repo := range repos {
			alias := suggestAlias(repo)
			fmt.Printf("  %s → alias: %s [Y/n/custom]: ", repo, alias)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(response)

			if response == "" || strings.ToLower(response) == "y" {
				eco[alias] = repo
			} else if response != "n" && response != "no" {
				// Custom alias
				eco[response] = repo
			} else {
				remaining = append(remaining, repo)
			}
		}

		if len(eco) > 0 {
			cfg.Ecosystems[ecoName] = eco
		}

		repos = remaining

		if len(repos) > 0 {
			fmt.Printf("\n%d repositories remaining. Create another ecosystem? [Y/n]: ", len(repos))
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response == "n" || response == "no" {
				break
			}
		}
	}

	return nil
}

func suggestAlias(repoName string) string {
	// Generic alias suggestion - just use the repo name as-is
	// Users can customize this during init or edit the config manually
	return repoName
}

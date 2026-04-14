package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/log"
	"github.com/mssantosdev/hydra/internal/ui/components"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:   "clone <repo-url>",
	Short: "Clone a new repository and set up worktrees",
	Long: `Clone a new repository as a bare repo and create worktrees.

This command will:
1. Clone the repository as a bare repo to .bare/
2. Create worktrees for specified branches
3. Add the repository to your .hydra.yaml configuration
4. Create symlinks in the appropriate group folder

Examples:
  # Interactive mode
  hydra clone github.com/user/repo

  # Non-interactive with options
  hydra clone github.com/user/repo --alias api --group backend --branches main,dev

  # Dry run to see what would happen
  hydra clone github.com/user/repo --dry-run`,
	Args: cobra.ExactArgs(1),
	RunE: runClone,
}

// CloneOptions holds the clone configuration
type CloneOptions struct {
	URL         string
	Alias       string
	Group       string
	Branches    []string
	DryRun      bool
	Interactive bool
	NewProject  bool
	ProjectName string
}

// Rollback holds cleanup functions
type Rollback struct {
	actions []func()
}

func (r *Rollback) Add(action func()) {
	r.actions = append(r.actions, action)
}

func (r *Rollback) Execute() {
	for i := len(r.actions) - 1; i >= 0; i-- {
		r.actions[i]()
	}
}

func (r *Rollback) Clear() {
	r.actions = nil
}

func init() {
	rootCmd.AddCommand(cloneCmd)

	cloneCmd.Flags().StringP("alias", "a", "", "Repository alias (short name)")
	cloneCmd.Flags().StringP("group", "g", "", "Group name (previously called ecosystem)")
	cloneCmd.Flags().StringSliceP("branches", "b", []string{}, "Branches to create worktrees for (comma-separated)")
	cloneCmd.Flags().BoolP("dry-run", "n", false, "Show what would be done without executing")
	cloneCmd.Flags().BoolP("interactive", "i", true, "Interactive mode (prompt for missing options)")
	cloneCmd.Flags().BoolP("new-project", "p", false, "Create a new project directory")
}

func runClone(cmd *cobra.Command, args []string) error {
	url := args[0]

	// Parse flags
	alias, _ := cmd.Flags().GetString("alias")
	group, _ := cmd.Flags().GetString("group")
	branches, _ := cmd.Flags().GetStringSlice("branches")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	interactive, _ := cmd.Flags().GetBool("interactive")
	newProject, _ := cmd.Flags().GetBool("new-project")

	// Set verbose logging if needed
	verbose, _ := cmd.Flags().GetBool("verbose")
	log.SetVerbose(verbose)

	// Extract repo name from URL
	repoName := extractRepoName(url)

	// Build options
	opts := &CloneOptions{
		URL:         url,
		Alias:       alias,
		Group:       group,
		Branches:    branches,
		DryRun:      dryRun,
		Interactive: interactive,
		NewProject:  newProject,
	}

	// Get working directory
	wd, err := os.Getwd()
	if err != nil {
		log.Error("Failed to get working directory", "error", err)
		return err
	}

	// Try to find config
	configPath, cfg, configErr := config.FindConfig(wd)

	// Handle no config found
	if configErr != nil {
		if interactive {
			// Check if user wants to create new project
			var createNew bool
			var projectName string

			form := huh.NewForm(
				huh.NewGroup(
					huh.NewNote().
						Title("No Hydra Project Found").
						Description("A Hydra project organizes your repositories using worktrees.\n\n"+
							"A project contains:\n"+
							"  • A .hydra.yaml configuration file\n"+
							"  • A .bare/ directory (bare git repositories)\n"+
							"  • Group folders (backend/, frontend/, etc.)"),

					huh.NewConfirm().
						Title("Create a new Hydra project?").
						Description("This will create a new directory for your project.").
						Value(&createNew).
						Affirmative("Yes, create project").
						Negative("Cancel"),
				),
			)

			if err := form.Run(); err != nil {
				return fmt.Errorf("cancelled")
			}

			if !createNew {
				return fmt.Errorf("aborted: no .hydra.yaml found")
			}

			// Ask for project name
			nameForm := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Project Name").
						Description("The directory name for your new project.\n" +
							"Example: my-project, gileade, company-tools").
						Value(&projectName).
						Placeholder(repoName).
						Validate(func(s string) error {
							if s == "" {
								return fmt.Errorf("project name cannot be empty")
							}
							return nil
						}),
				),
			)

			if err := nameForm.Run(); err != nil {
				return fmt.Errorf("cancelled")
			}

			if projectName == "" {
				projectName = repoName
			}

			opts.ProjectName = projectName
			opts.NewProject = true

			// Create project directory
			projectDir := filepath.Join(wd, projectName)
			if err := os.MkdirAll(projectDir, 0755); err != nil {
				return fmt.Errorf("failed to create project directory: %w", err)
			}

			// Initialize config
			cfg = config.DefaultConfig()
			configPath = filepath.Join(projectDir, ".hydra.yaml")
			if err := cfg.Save(configPath); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			log.Success("Created new Hydra project", "name", projectName)
			wd = projectDir
		} else {
			return fmt.Errorf("no .hydra.yaml found. Run 'hydra init' first or use --interactive")
		}
	}

	projectRoot := filepath.Dir(configPath)

	// Interactive prompts
	if interactive {
		if err := promptForOptions(opts, repoName, cfg); err != nil {
			return err
		}
	}

	// Validate options
	if opts.Alias == "" {
		opts.Alias = repoName
	}
	if opts.Group == "" {
		opts.Group = "default"
	}
	if len(opts.Branches) == 0 {
		opts.Branches = []string{"main"}
	}

	// Dry run
	if opts.DryRun {
		return showDryRun(opts, configPath, cfg, projectRoot)
	}

	// Execute clone
	return executeClone(opts, cfg, configPath, projectRoot)
}

func promptForOptions(opts *CloneOptions, repoName string, cfg *config.Config) error {
	// Get existing groups for selection
	existingGroups := getExistingGroups(cfg)

	var groupOptions []huh.Option[string]
	for _, g := range existingGroups {
		groupOptions = append(groupOptions, huh.NewOption(g, g))
	}
	groupOptions = append(groupOptions, huh.NewOption("+ Create new group", "__new__"))

	// Group selection with hint
	var selectedGroup string
	groupForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select Group").
				Description("Groups organize related repositories.\n" +
					"Examples: backend (APIs, services), frontend (web apps), infra (configs).").
				Options(groupOptions...).
				Value(&selectedGroup),
		),
	)

	if err := groupForm.Run(); err != nil {
		return fmt.Errorf("cancelled")
	}

	if selectedGroup == "__new__" {
		// Ask for new group name
		newGroupForm := huh.NewForm(
			huh.NewGroup(
				huh.NewInput().
					Title("New Group Name").
					Description("Create a new group for organizing repositories.\n" +
						"This will be the folder name where your repository's worktrees are linked.").
					Value(&opts.Group).
					Placeholder("backend").
					Validate(func(s string) error {
						if s == "" {
							return fmt.Errorf("group name cannot be empty")
						}
						return nil
					}),
			),
		)
		if err := newGroupForm.Run(); err != nil {
			return fmt.Errorf("cancelled")
		}
	} else {
		opts.Group = selectedGroup
	}

	// Alias input with hint
	aliasForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Alias").
				Description("A short name for this repository.\n" +
					"Used to navigate: cd " + opts.Group + "/<alias>\n" +
					"Default is the repository name.").
				Value(&opts.Alias).
				Placeholder(repoName),
		),
	)

	if err := aliasForm.Run(); err != nil {
		return fmt.Errorf("cancelled")
	}

	if opts.Alias == "" {
		opts.Alias = repoName
	}

	// Fetch branches in background
	log.Info("Fetching available branches from remote...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	branchesChan := make(chan []git.RemoteBranch, 1)
	errChan := make(chan error, 1)

	go func() {
		branches, err := git.FetchRemoteBranches(opts.URL)
		if err != nil {
			errChan <- err
			return
		}
		branchesChan <- branches
	}()

	// Show spinner while fetching
	spinner := components.NewSpinner("Fetching branches...", components.SpinnerDots)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

fetchLoop:
	for {
		select {
		case <-ctx.Done():
			log.Warn("Branch fetch timed out, using defaults")
			break fetchLoop
		case err := <-errChan:
			log.Debug("Failed to fetch branches", "error", err)
			break fetchLoop
		case branches := <-branchesChan:
			if len(branches) > 0 {
				opts.Branches = selectBranches(branches)
			}
			break fetchLoop
		case <-ticker.C:
			// Continue showing spinner
		}
	}

	spinner.Finish()

	// If no branches selected yet, use default
	if len(opts.Branches) == 0 {
		opts.Branches = []string{"main"}
	}

	// Confirmation
	var confirmed bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Clone Configuration").
				Description(fmt.Sprintf(
					"Repository: %s\n"+
						"Alias: %s\n"+
						"Group: %s\n"+
						"Branches: %s",
					opts.URL,
					opts.Alias,
					opts.Group,
					strings.Join(opts.Branches, ", "),
				)),

			huh.NewConfirm().
				Title("Proceed with clone?").
				Value(&confirmed).
				Affirmative("Yes, clone").
				Negative("Cancel"),
		),
	)

	if err := confirmForm.Run(); err != nil {
		return fmt.Errorf("cancelled")
	}

	if !confirmed {
		return fmt.Errorf("user cancelled")
	}

	return nil
}

func selectBranches(branches []git.RemoteBranch) []string {
	var options []huh.Option[string]
	var defaults []string

	for _, b := range branches {
		label := b.Name
		if b.IsDefault {
			label = b.Name + " (default)"
			defaults = append(defaults, b.Name)
		}
		options = append(options, huh.NewOption(label, b.Name))
	}

	// If no defaults found, use main or first branch
	if len(defaults) == 0 {
		for _, b := range branches {
			if b.Name == "main" || b.Name == "master" {
				defaults = append(defaults, b.Name)
				break
			}
		}
		if len(defaults) == 0 && len(branches) > 0 {
			defaults = append(defaults, branches[0].Name)
		}
	}

	var selected []string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select Branches").
				Description("Choose which branches to create as worktrees.\n" +
					"Each branch becomes a separate working directory you can use simultaneously.").
				Options(options...).
				Value(&selected).
				Validate(func(s []string) error {
					if len(s) == 0 {
						return fmt.Errorf("select at least one branch")
					}
					return nil
				}),
		),
	)

	// Set defaults after creating the form
	selected = defaults

	if err := form.Run(); err != nil {
		return defaults
	}

	return selected
}

func executeClone(opts *CloneOptions, cfg *config.Config, configPath, projectRoot string) error {
	rollback := &Rollback{}
	defer func() {
		if r := recover(); r != nil {
			log.Error("Panic occurred, rolling back", "error", r)
			rollback.Execute()
			panic(r)
		}
	}()

	// Clone bare repo with progress
	barePath := filepath.Join(projectRoot, cfg.Paths.BareDir, opts.Alias+".git")
	log.Info("Cloning bare repository...")

	// Create bare directory
	if err := os.MkdirAll(filepath.Dir(barePath), 0755); err != nil {
		return fmt.Errorf("failed to create bare directory: %w", err)
	}

	// Clone with progress indication
	progress := components.NewSimpleProgress("Cloning bare repository")
	progressCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-progressCtx.Done():
				return
			case <-ticker.C:
				// Update spinner
			}
		}
	}()

	if err := git.CloneBare(opts.URL, barePath); err != nil {
		return fmt.Errorf("failed to clone bare repository: %w", err)
	}

	progress.Finish()
	log.Success("Bare repository cloned")

	rollback.Add(func() {
		os.RemoveAll(barePath)
	})

	// Create worktrees
	createdWorktrees := []string{}
	failedWorktrees := []string{}

	for _, branch := range opts.Branches {
		log.Info("Creating worktree", "branch", branch)

		worktreePath := filepath.Join(barePath, branch)
		safeBranch := strings.ReplaceAll(branch, "/", "-")

		if err := git.CreateWorktree(barePath, worktreePath, branch); err != nil {
			log.Error("Failed to create worktree", "branch", branch, "error", err)
			failedWorktrees = append(failedWorktrees, branch)
			continue
		}

		createdWorktrees = append(createdWorktrees, branch)
		log.Success("Worktree created", "branch", branch)

		// Create symlink
		symlinkDir := filepath.Join(projectRoot, opts.Group)
		os.MkdirAll(symlinkDir, 0755)

		symlinkName := opts.Alias + "-" + safeBranch
		if safeBranch == "main" || safeBranch == "master" {
			symlinkName = opts.Alias
		}

		symlinkPath := filepath.Join(symlinkDir, symlinkName)
		relPath, _ := filepath.Rel(symlinkDir, worktreePath)
		os.Symlink(relPath, symlinkPath)
	}

	// Update config
	log.Info("Updating configuration...")
	if cfg.Ecosystems == nil {
		cfg.Ecosystems = make(map[string]config.Ecosystem)
	}
	if cfg.Ecosystems[opts.Group] == nil {
		cfg.Ecosystems[opts.Group] = make(config.Ecosystem)
	}
	cfg.Ecosystems[opts.Group][opts.Alias] = opts.Alias

	if err := cfg.Save(configPath); err != nil {
		log.Error("Failed to save config", "error", err)
		return err
	}
	log.Success("Configuration updated")

	// Clear rollback on success
	rollback.Clear()

	// Print summary
	printSummary(opts, barePath, createdWorktrees, failedWorktrees)

	return nil
}

func printSummary(opts *CloneOptions, barePath string, created, failed []string) {
	fmt.Println()
	fmt.Println(styles.Success.Render("✓ Successfully cloned repository"))
	fmt.Println()

	// Summary box
	summaryStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7aa2f7")).
		Padding(1)

	var summaryContent strings.Builder
	summaryContent.WriteString(styles.Label.Render("Repository: ") + opts.Alias + "\n")
	summaryContent.WriteString(styles.Label.Render("Group: ") + opts.Group + "\n")
	summaryContent.WriteString(styles.Label.Render("Bare repo: ") + barePath + "\n")
	summaryContent.WriteString("\n")
	summaryContent.WriteString(styles.Label.Render(fmt.Sprintf("Worktrees created (%d):\n", len(created))))
	for _, branch := range created {
		summaryContent.WriteString("  " + styles.Success.Render("✓") + " " + branch + "\n")
	}

	if len(failed) > 0 {
		summaryContent.WriteString("\n")
		summaryContent.WriteString(styles.Error.Render(fmt.Sprintf("Failed (%d):\n", len(failed))))
		for _, branch := range failed {
			summaryContent.WriteString("  " + styles.Error.Render("✗") + " " + branch + "\n")
		}
	}

	fmt.Println(summaryStyle.Render(summaryContent.String()))

	fmt.Println()
	nextSteps := fmt.Sprintf("cd %s/%s", opts.Group, opts.Alias)
	fmt.Println(styles.Label.Render("Next steps: ") + styles.Branch.Render(nextSteps))
}

func showDryRun(opts *CloneOptions, configPath string, cfg *config.Config, projectRoot string) error {
	fmt.Println()
	fmt.Println(styles.Title.Render("DRY RUN - Would execute:"))
	fmt.Println()

	barePath := filepath.Join(projectRoot, cfg.Paths.BareDir, opts.Alias+".git")

	fmt.Println(styles.Label.Render("Configuration: ") + configPath)
	fmt.Println(styles.Label.Render("Clone bare:    ") + barePath)
	fmt.Println(styles.Label.Render("Group:         ") + opts.Group)
	fmt.Println(styles.Label.Render("Alias:         ") + opts.Alias)
	fmt.Println()
	fmt.Println(styles.Label.Render("Worktrees:"))
	for _, branch := range opts.Branches {
		fmt.Printf("  • %s\n", branch)
	}
	fmt.Println()

	return nil
}

func getExistingGroups(cfg *config.Config) []string {
	var groups []string
	for name := range cfg.Ecosystems {
		groups = append(groups, name)
	}
	return groups
}

func extractRepoName(url string) string {
	name := strings.TrimSuffix(url, ".git")

	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")
		name = parts[len(parts)-1]
	}

	parts := strings.Split(name, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}

	return name
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new Hydra project",
	Long: `Create a new Hydra project and bootstrap the first repository.

DESCRIPTION
  Starts an interactive flow to create a new Hydra project directory and set up
  the first repository using either a local repo bootstrap or the existing
  remote clone flow.

WHEN TO USE
  • Starting a brand-new project locally
  • Creating a Hydra workspace for a new codebase
  • Bootstrapping the first repository before adding more repos and worktrees
`,
	RunE: runNew,
}

type newProjectOptions struct {
	ProjectPath   string
	Mode          string
	Group         string
	Alias         string
	LocalRepoName string
	InitialBranch string
	RemoteURL     string
}

func init() {
	rootCmd.AddCommand(newCmd)
}

func runNew(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	opts, err := promptForNewProjectOptions()
	if err != nil {
		return err
	}

	projectRoot, configPath, cfg, err := createProjectRoot(wd, opts.ProjectPath)
	if err != nil {
		return err
	}

	if opts.Mode == "remote" {
		cloneOpts := &CloneOptions{
			URL:         opts.RemoteURL,
			Alias:       opts.Alias,
			Group:       opts.Group,
			Branches:    []string{opts.InitialBranch},
			Interactive: false,
		}
		if err := executeClone(cloneOpts, cfg, configPath, projectRoot); err != nil {
			return err
		}
		printProjectCreatedSummary(wd, projectRoot)
		return nil
	}

	if err := bootstrapLocalProject(projectRoot, configPath, cfg, opts); err != nil {
		return err
	}

	printProjectCreatedSummary(wd, projectRoot)
	return nil
}

func promptForNewProjectOptions() (*newProjectOptions, error) {
	opts := &newProjectOptions{InitialBranch: "main"}

	projectForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project Path").
				Description("Relative path to create the Hydra project in. Nested paths are allowed.").
				Placeholder("client-x/api-platform").
				Value(&opts.ProjectPath).
				Validate(func(s string) error {
					_, err := validateRelativeProjectPath(s)
					return err
				}),
		),
	)
	if err := projectForm.Run(); err != nil {
		return nil, err
	}

	modeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("First Repository").
				Description("Choose whether to create the first repo locally or clone it from a remote.").
				Options(
					huh.NewOption("Create local repo", "local"),
					huh.NewOption("Clone remote repo", "remote"),
				).
				Value(&opts.Mode),
		),
	)
	if err := modeForm.Run(); err != nil {
		return nil, err
	}

	if err := promptForNewRepoMetadata(opts); err != nil {
		return nil, err
	}

	if opts.Mode == "local" {
		if err := promptForLocalRepoOptions(opts); err != nil {
			return nil, err
		}
	} else {
		if err := promptForRemoteRepoOptions(opts); err != nil {
			return nil, err
		}
	}

	return opts, nil
}

func promptForNewRepoMetadata(opts *newProjectOptions) error {
	metadataForm := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Group").
				Description("Folder name used for symlinks inside the Hydra project.").
				Placeholder("backend").
				Value(&opts.Group).
				Validate(func(s string) error {
					return validatePathSegment("group", s)
				}),
			huh.NewInput().
				Title("Alias").
				Description("Short name used in symlinks and Hydra commands.").
				Placeholder("api").
				Value(&opts.Alias).
				Validate(func(s string) error {
					return validatePathSegment("alias", s)
				}),
			huh.NewInput().
				Title("Initial Branch").
				Description("First branch to create as a worktree.").
				Placeholder("main").
				Value(&opts.InitialBranch).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("initial branch cannot be empty")
					}
					return nil
				}),
		),
	)
	return metadataForm.Run()
}

func promptForLocalRepoOptions(opts *newProjectOptions) error {
	if opts.LocalRepoName == "" {
		opts.LocalRepoName = opts.Alias
	}
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Local Repository Directory").
				Description("Single directory name for the first local repository inside the new project.").
				Placeholder(opts.Alias).
				Value(&opts.LocalRepoName).
				Validate(func(s string) error {
					return validatePathSegment("local repository directory", s)
				}),
		),
	)
	return form.Run()
}

func promptForRemoteRepoOptions(opts *newProjectOptions) error {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Remote URL").
				Description("Repository URL to clone into the new Hydra project.").
				Placeholder("github.com/org/repo").
				Value(&opts.RemoteURL).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("remote URL cannot be empty")
					}
					return nil
				}),
		),
	)
	return form.Run()
}

func bootstrapLocalProject(projectRoot, configPath string, cfg *config.Config, opts *newProjectOptions) error {
	repoPath := filepath.Join(projectRoot, opts.LocalRepoName)
	if err := git.InitRepository(repoPath, opts.InitialBranch); err != nil {
		return err
	}

	barePath := filepath.Join(projectRoot, cfg.Paths.BareDir, opts.Alias+".git")
	if err := os.MkdirAll(filepath.Dir(barePath), 0755); err != nil {
		return fmt.Errorf("failed to create bare directory: %w", err)
	}
	if err := git.CloneBareFromLocal(repoPath, barePath); err != nil {
		return err
	}

	repo := repoContext{
		Ecosystem: opts.Group,
		Alias:     opts.Alias,
		RepoName:  opts.LocalRepoName,
		BareRepo:  barePath,
	}
	wt := buildWorktreeContext(repo, projectRoot, opts.InitialBranch)
	if err := git.CreateWorktreeForBranch(barePath, wt.WorktreePath, opts.InitialBranch); err != nil {
		return err
	}
	if err := ensureSymlink(wt); err != nil {
		return err
	}

	if err := registerRepo(cfg, configPath, opts.Group, opts.Alias, opts.LocalRepoName); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}

	return nil
}

func printProjectCreatedSummary(wd, projectRoot string) {
	relPath, err := filepath.Rel(wd, projectRoot)
	if err != nil || relPath == "" {
		relPath = projectRoot
	}
	fmt.Println()
	fmt.Println(styles.Success.Render("✓ Hydra project created"))
	fmt.Printf("  Path: %s\n", projectRoot)
	fmt.Println()
	fmt.Printf("cd %s\n", relPath)
	fmt.Println("hydra list")
}

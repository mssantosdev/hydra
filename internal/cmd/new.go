package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/config/registry"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/mssantosdev/hydra/internal/ui/styles"
	"github.com/spf13/cobra"
)

var (
	newProjectPath string
	newGroup       string
	newAlias       string
	newBranch      string
	newRemoteURL   string
	newLocal       bool
)

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new Hydra project",
	Long: `Create a new Hydra project and bootstrap the first repository.

DESCRIPTION
  Creates a new Hydra workspace with schema v2 configuration, registers it in
  the project registry, and bootstraps the first repository either locally or
  from a remote URL.

  The repository alias is the single source of truth for both the bare
  repository path (.bare/<alias>.git) and the default-branch worktree directory
  (<group>/<alias>).

WHEN TO USE
  - Starting a brand-new project locally
  - Creating a Hydra workspace for a new codebase
  - Bootstrapping the first repository before adding more repos and worktrees

EXAMPLES
  hydra new

  hydra new --project-path client/api --group backend --alias api --branch main --local

  hydra new --project-path client/api --group backend --alias api --branch main --remote-url git@github.com:org/repo.git

EXIT CODES
  0  Success (project created and first repository bootstrapped)
  1  General error (invalid options, git failure, registry conflict)
  4  Partial failure when cloning the first remote repository

SEE ALSO
  hydra init - Initialize configuration in an existing directory
  hydra clone - Add another repository to a project`,
	RunE: runNew,
}

type newProjectOptions struct {
	ProjectPath   string
	Mode          string
	Group         string
	Alias         string
	InitialBranch string
	RemoteURL     string
}

type newResult struct {
	Project string `json:"project"`
	Root    string `json:"root"`
	Group   string `json:"group"`
	Repo    string `json:"repo"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
}

func init() {
	rootCmd.AddCommand(newCmd)

	newCmd.Flags().StringVar(&newProjectPath, "project-path", "", "relative path for the new project directory")
	newCmd.Flags().StringVar(&newGroup, "group", "", "group folder for the first repository")
	newCmd.Flags().StringVar(&newAlias, "alias", "", "repository alias")
	newCmd.Flags().StringVar(&newBranch, "branch", "main", "initial branch to check out")
	newCmd.Flags().StringVar(&newRemoteURL, "remote-url", "", "remote URL to clone for the first repository")
	newCmd.Flags().BoolVar(&newLocal, "local", false, "bootstrap a new local repository instead of cloning a remote")
}

func runNew(cmd *cobra.Command, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to get working directory")
	}

	opts, err := resolveNewProjectOptions()
	if err != nil {
		return err
	}

	projectRoot, configPath, cfg, err := createProjectRoot(wd, opts.ProjectPath)
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to create project")
	}

	if err := registry.Register(cfg.Project, projectRoot); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to register project %q", cfg.Project)
	}

	if opts.Mode == "remote" {
		cloneOpts := &CloneOptions{
			URL:         opts.RemoteURL,
			Alias:       opts.Alias,
			Group:       opts.Group,
			Branches:    []string{opts.InitialBranch},
			Interactive: false,
		}
		if _, _, err := performClone(cloneOpts, cfg, configPath, projectRoot); err != nil {
			return err
		}
	} else if err := bootstrapLocalProject(projectRoot, configPath, cfg, opts); err != nil {
		return err
	}

	repo, err := resolveRepoByAlias(cfg, projectRoot, opts.Alias)
	if err != nil {
		return err
	}

	worktreeFullPath := worktreePath(projectRoot, repo.Group, worktreeDirName(repo, opts.InitialBranch))

	result := newResult{
		Project: cfg.Project,
		Root:    projectRoot,
		Group:   opts.Group,
		Repo:    opts.Alias,
		Branch:  opts.InitialBranch,
		Path:    worktreeFullPath,
	}

	return emit(cmd, fmt.Sprintf("workspace %q created", result.Project), result, nil, func() {
		relPath, relErr := filepath.Rel(wd, projectRoot)
		if relErr != nil || relPath == "" {
			relPath = projectRoot
		}
		fmt.Println()
		fmt.Println(styles.Success.Render("✓ Hydra project created"))
		fmt.Printf("  Project: %s\n", cfg.Project)
		fmt.Printf("  Path:    %s\n", projectRoot)
		fmt.Printf("  Repo:    %s/%s\n", opts.Group, opts.Alias)
		fmt.Println()
		fmt.Printf("cd %s\n", relPath)
		fmt.Println("hydra list")
	})
}

func resolveNewProjectOptions() (*newProjectOptions, error) {
	if interactive() {
		return promptForNewProjectOptions()
	}

	opts := &newProjectOptions{
		ProjectPath:   newProjectPath,
		Group:         newGroup,
		Alias:         newAlias,
		InitialBranch: newBranch,
	}

	if strings.TrimSpace(opts.ProjectPath) == "" {
		return nil, output.Errorf(output.CodeInternal, "--project-path is required in non-interactive mode")
	}
	if err := validatePathSegment("group", opts.Group); err != nil {
		return nil, output.Errorf(output.CodeInternal, "%v", err)
	}
	if err := validatePathSegment("alias", opts.Alias); err != nil {
		return nil, output.Errorf(output.CodeInternal, "%v", err)
	}
	if strings.TrimSpace(opts.InitialBranch) == "" {
		return nil, output.Errorf(output.CodeInternal, "branch cannot be empty")
	}

	switch {
	case newLocal:
		opts.Mode = "local"
	case strings.TrimSpace(newRemoteURL) != "":
		opts.Mode = "remote"
		opts.RemoteURL = newRemoteURL
	default:
		return nil, output.Errorf(output.CodeInternal, "pass --local or --remote-url in non-interactive mode")
	}

	return opts, nil
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

	if opts.Mode == "remote" {
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
				Description("Folder that will contain this repository's worktrees.").
				Placeholder("backend").
				Value(&opts.Group).
				Validate(func(s string) error {
					return validatePathSegment("group", s)
				}),
			huh.NewInput().
				Title("Alias").
				Description("Short name used in commands and as the bare repository name.").
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
	barePath := cfg.BarePath(projectRoot, opts.Alias)
	if err := os.MkdirAll(filepath.Dir(barePath), 0750); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to create bare directory")
	}

	// A project with no remote gets a bare repo with NO origin at all. Seeding it
	// from a throwaway checkout and leaving that path as `origin` would configure a
	// remote that does not exist, breaking every later fetch.
	if err := git.InitBareLocal(barePath, opts.InitialBranch); err != nil {
		return output.Wrap(output.CodeGitFailed, err, "failed to create bare repository")
	}

	repo := repoContext{
		Group:         opts.Group,
		Alias:         opts.Alias,
		DefaultBranch: opts.InitialBranch,
		BareRepo:      barePath,
	}

	targetPath := worktreePath(projectRoot, repo.Group, worktreeDirName(repo, opts.InitialBranch))
	if err := createWorktreeForBranch(cfg, repo, targetPath, opts.InitialBranch, ""); err != nil {
		return err
	}

	cfg.SetRepo(opts.Group, opts.Alias, config.Repo{DefaultBranch: opts.InitialBranch})
	if err := cfg.Save(configPath); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to save config")
	}

	hctx := hooksContextFor(repo, opts.InitialBranch, targetPath)
	if _, err := runHookEventForProject(cfg, projectRoot, "post_clone", hctx, targetPath); err != nil {
		return err
	}

	return nil
}

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mssantosdev/hydra/internal/config"
	"github.com/mssantosdev/hydra/internal/git"
	"github.com/mssantosdev/hydra/internal/output"
	"github.com/spf13/cobra"
)

var (
	adoptGroup  string
	adoptAlias  string
	adoptBranch string
)

var adoptCmd = &cobra.Command{
	Use:   "adopt <path>",
	Short: "Import an existing checkout into the current Hydra workspace",
	Long: `Register an existing Git checkout without moving or rewriting it.

DESCRIPTION
  adopt reads the checkout's origin URL, creates a hydra bare repository under
  .bare/<alias>.git with InitBareWithRemote, registers the alias under --group,
  and creates hydra worktrees for the requested branch. It also scans up to three
  directory levels below the given path for sibling checkouts that share the same
  origin and registers worktrees for their current branches.

  adopt never moves, deletes, or rewrites the checkout you point at. The original
  directory keeps its own .git directory untouched.

WHEN TO USE
  • Onboarding pre-existing repositories into a hydra workspace
  • Importing a legacy checkout tree that already lives near the project root
  • Adding repos when config migration is out of scope

EXAMPLES
  hydra repo add ../legacy-api --adopt --group backend
  hydra repo add ../legacy-api --adopt --group backend --alias api --branch main
  hydra repo add ../legacy-api --adopt --group backend --output json

FLAGS
  --group <name>    Group directory (required)
  --alias <name>    Repository alias (default: checkout directory name)
  --branch <name>   Branch to adopt (default: checkout's current branch)

EXIT CODES
  0  Success
  1  git_failed / internal error
  2  not_in_project

SEE ALSO
  hydra doctor - Validate the workspace after adopting
  hydra repo add <url> - clone a fresh repository into the workspace`,
	Args: cobra.ExactArgs(1),
	RunE: runAdopt,
}

type adoptJSON struct {
	Project   string         `json:"project"`
	Root      string         `json:"root"`
	Group     string         `json:"group"`
	Repo      string         `json:"repo"`
	Remote    string         `json:"remote"`
	BarePath  string         `json:"bare_path"`
	Worktrees []worktreeJSON `json:"worktrees"`
}

func init() {
	adoptCmd.Flags().StringVar(&adoptGroup, "group", "", "Group directory for the adopted repository (required)")
	adoptCmd.Flags().StringVar(&adoptAlias, "alias", "", "Repository alias (default: checkout directory name)")
	adoptCmd.Flags().StringVar(&adoptBranch, "branch", "", "Branch to adopt (default: checkout current branch)")
	_ = adoptCmd.MarkFlagRequired("group")
}

func runAdopt(cmd *cobra.Command, args []string) error {
	if cfg == nil {
		return output.Errorf(output.CodeNotInProject,
			`no hydra workspace found; run "hydra init" or pass --project <name>`)
	}
	if err := validatePathSegment("group", adoptGroup); err != nil {
		return output.Errorf(output.CodeInternal, "%v", err)
	}

	checkoutPath, err := filepath.Abs(args[0])
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to resolve checkout path")
	}
	if _, err := os.Stat(filepath.Join(checkoutPath, ".git")); err != nil {
		return output.Errorf(output.CodeInternal, "%s is not a git checkout", checkoutPath)
	}

	remoteURL, err := git.GetRemoteURL(checkoutPath)
	if err != nil || strings.TrimSpace(remoteURL) == "" {
		return output.Errorf(output.CodeGitFailed, "checkout has no origin remote to adopt")
	}

	alias := strings.TrimSpace(adoptAlias)
	if alias == "" {
		alias = filepath.Base(checkoutPath)
	}
	if err := validatePathSegment("alias", alias); err != nil {
		return output.Errorf(output.CodeInternal, "%v", err)
	}

	branch := strings.TrimSpace(adoptBranch)
	if branch == "" {
		branch, err = git.GetCurrentBranch(checkoutPath)
		if err != nil || branch == "" {
			return output.Errorf(output.CodeGitFailed, "could not determine branch to adopt")
		}
	}

	barePath := cfg.BarePath(projectRoot, alias)
	if err := os.MkdirAll(filepath.Dir(barePath), 0o750); err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to create bare directory")
	}
	if err := git.InitBareWithRemote(barePath, remoteURL); err != nil {
		return output.Wrap(output.CodeGitFailed, err, "failed to initialize bare repository")
	}

	// Same locked read-modify-write as a clone: adopting is reached through the same
	// `repo add` front door, so it is exposed to the same concurrent-registration race.
	if err := config.Update(projectRoot, func(live *config.Config) error {
		live.RegisterRepo(adoptGroup, alias, remoteURL, branch)
		return nil
	}); err != nil {
		return classifyManifestErr(err)
	}
	cfg.RegisterRepo(adoptGroup, alias, remoteURL, branch)

	repo := repoContextFor(cfg, projectRoot, config.RepoRef{Group: adoptGroup, Alias: alias, Repo: cfg.Groups[adoptGroup][alias]})
	candidates, err := collectAdoptCandidates(checkoutPath, remoteURL, branch)
	if err != nil {
		return output.Wrap(output.CodeInternal, err, "failed to scan sibling checkouts")
	}

	var worktrees []worktreeJSON
	// Carry warnings from adopted worktrees ride the same envelope as everything else.
	var adoptWarnings []string
	for _, candidate := range candidates {
		dirName := worktreeDirName(repo, candidate.Branch)
		targetPath := worktreePath(projectRoot, repo.Group, dirName)
		if err := checkWorktreeNameConflict(repo, projectRoot, dirName, candidate.Branch); err != nil {
			return output.Wrap(output.CodeWorktreeExists, err, "worktree conflict for branch %s", candidate.Branch)
		}
		if _, err := os.Stat(targetPath); err == nil {
			wt, ok := findRepoWorktreeByBranch(repo, candidate.Branch)
			if ok {
				item, err := wt.withTracking()
				if err != nil {
					return output.Wrap(output.CodeGitFailed, err, "failed to read worktree tracking")
				}
				worktrees = append(worktrees, item)
				continue
			}
		}
		carried, err := createWorktreeForBranch(cfg, repo, targetPath, candidate.Branch, "")
		adoptWarnings = append(adoptWarnings, carried...)
		if err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to create worktree for %s", candidate.Branch)
		}
		wt, ok := findRepoWorktreeByBranch(repo, candidate.Branch)
		if !ok {
			return output.Errorf(output.CodeInternal, "worktree for %s was not registered", candidate.Branch)
		}
		item, err := wt.withTracking()
		if err != nil {
			return output.Wrap(output.CodeGitFailed, err, "failed to read worktree tracking")
		}
		worktrees = append(worktrees, item)
	}

	payload := adoptJSON{
		Project:   cfg.Project,
		Root:      projectRoot,
		Group:     adoptGroup,
		Repo:      alias,
		Remote:    remoteURL,
		BarePath:  barePath,
		Worktrees: worktrees,
	}

	return emit(cmd, fmt.Sprintf("adopted %s with %d worktree(s)", alias, len(worktrees)), payload, adoptWarnings, func() {
		fmt.Printf("Adopted %s into %s/%s (%d worktree(s))\n", alias, adoptGroup, alias, len(worktrees))
	})
}

type adoptCandidate struct {
	Path   string
	Branch string
}

func collectAdoptCandidates(rootPath, remoteURL, primaryBranch string) ([]adoptCandidate, error) {
	seen := map[string]struct{}{}
	var out []adoptCandidate

	add := func(path string) error {
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			return nil
		}
		url, err := git.GetRemoteURL(path)
		if err != nil || url != remoteURL {
			return nil
		}
		branch, err := git.GetCurrentBranch(path)
		if err != nil || branch == "" {
			return nil
		}
		if _, ok := seen[branch]; ok {
			return nil
		}
		seen[branch] = struct{}{}
		out = append(out, adoptCandidate{Path: path, Branch: branch})
		return nil
	}

	if err := add(rootPath); err != nil {
		return nil, err
	}
	_ = primaryBranch

	var walk func(dir string, depth int) error
	walk = func(dir string, depth int) error {
		if depth > 3 {
			return nil
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if err := add(path); err != nil {
				return err
			}
			if err := walk(path, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(rootPath, 1); err != nil {
		return nil, err
	}
	return out, nil
}
